"""Vite bundler implementation for rules_typescript.

Provides the vite_bundler rule which returns a BundlerInfo that wires Vite
into the ts_bundle / ts_binary bundler interface.

Usage:

    load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    # Build a node_modules tree with vite and its transitive deps.
    # The target can have any name; vite_bundler creates a "node_modules"
    # symlink at runtime so Node.js ESM resolution always finds it.
    node_modules(
        name = "vite_deps",
        deps = ["@npm//:vite"],
    )

    vite_bundler(
        name = "vite",
        vite = "@npm//:vite",
        node_modules = ":vite_deps",
    )

    ts_bundle(
        name = "app",
        entry_point = "//src/app:app",
        bundler = ":vite",
        format = "esm",
    )

The vite_bundler rule:
  1. Accepts the @npm//:vite target (NpmPackageInfo) for the Vite package files.
  2. Accepts a node_modules tree (any name) that contains vite and its transitive
     deps.
  3. Generates a thin wrapper script (as a Bazel action output) that:
     - Resolves the actual tree artifact path.
     - Creates a "node_modules" symlink in the parent directory pointing at the
       actual tree artifact (if not already named "node_modules").
     - cd's to the parent directory.
     - Invokes `node node_modules/vite/bin/vite.js build --config <config>`.
  4. Returns BundlerInfo with:
     - bundler_binary: the wrapper script
     - runtime_deps: the vite package files + node_modules tree

Why a "node_modules" symlink is created:
  Node.js ESM module resolution (unlike CJS NODE_PATH) traverses parent
  directories looking for a "node_modules" folder. The wrapper script creates
  a "node_modules" symlink in the parent of the tree artifact before invoking
  Vite, so any target name works transparently.
"""

load("//ts/private:providers.bzl", "BundlerInfo", "NpmPackageInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# ─── Rule implementation ───────────────────────────────────────────────────────

def _vite_bundler_impl(ctx):
    # Vite runs inside a build action, so node comes from the js_tool (exec
    # platform) toolchain unless the target names its own.
    runtime_args = []
    if ctx.file.runtime:
        runtime_binary = ctx.file.runtime
    else:
        js_tool = get_js_tool(ctx)
        if not js_tool:
            fail(
                "vite_bundler: no JS tool toolchain registered and no 'runtime' set for '{}'.\n".format(ctx.label) +
                "Either register one:\n" +
                "  register_toolchains(\"@rules_typescript//ts/toolchain:all\")\n" +
                "or point the runtime attr at a node binary built for the exec platform.",
            )
        runtime_binary = js_tool.runtime_binary
        runtime_args = js_tool.args_prefix

    # Collect the node_modules directory artifact (from node_modules() rule).
    # The node_modules() rule produces a single directory artifact.
    node_modules_files = ctx.files.node_modules
    if not node_modules_files:
        fail(
            "vite_bundler: 'node_modules' attr must be set to a node_modules() target " +
            "that contains vite and its transitive dependencies.\n" +
            "Example:\n" +
            "  node_modules(\n" +
            "      name = \"node_modules\",\n" +
            "      deps = [\"@npm//:vite\"],\n" +
            "  )\n" +
            "  vite_bundler(\n" +
            "      name = \"vite\",\n" +
            "      node_modules = \":node_modules\",\n" +
            "  )",
        )
    node_modules_dir = node_modules_files[0]

    # The actual tree artifact path (exec-root-relative).
    # This may or may not be named "node_modules". The wrapper script creates a
    # "node_modules" symlink pointing here if the name differs, so Node.js ESM
    # resolution always sees a correctly named directory.
    node_modules_path = node_modules_dir.path
    actual_nm_basename = node_modules_dir.basename

    # The parent directory is where we cd to run Vite.
    # node_modules_dir.path = "bazel-out/.../tests/pkg/<name>"
    # → parent_rel = "bazel-out/.../tests/pkg"
    parent_rel = node_modules_path[:-len("/" + actual_nm_basename)]

    # The vite binary is at node_modules/vite/bin/vite.js relative to the parent.
    # This path is used after cd'ing to the parent directory.
    vite_entry_rel = "node_modules/vite/bin/vite.js"

    # The runtime path is exec-root-relative. Since we cd to a subdirectory,
    # we must resolve it to an absolute path before cd'ing.
    # We capture EXEC_ROOT=$(pwd) before the cd, then use $EXEC_ROOT/<rel>.
    runtime_rel = runtime_binary.path
    runtime_args_str = " ".join(["\"{}\"".format(a) for a in runtime_args])

    # Generate a wrapper script that will be used as the bundler_binary in
    # build actions. This script:
    #   - Args: <config_path> <entry_path> <out_dir> <html_path|""> [<staging_manifest>]
    #     (all exec-root-relative)
    #   - Captures EXEC_ROOT=$(pwd) before cd'ing (CWD = exec root in Bazel actions)
    #   - Sets VITE_ENTRY_PATH, VITE_OUT_DIR, and (app mode) VITE_HTML_PATH env vars
    #   - When a staging manifest is provided ($5):
    #       - Creates a writable _staging/ directory inside the action sandbox
    #       - Copies each listed file preserving package-relative structure
    #       - Exports VITE_STAGING_ROOT pointing at the staging dir
    #   - cd's to the parent of the node_modules tree.
    #   - Invokes: node [runtime_args] node_modules/vite/bin/vite.js build --config <abs_config>
    #
    # CWD in a Bazel build action is the exec root. We cd to the subdirectory
    # containing node_modules/ so that Node.js ESM resolution traversal finds
    # vite's dependencies (picomatch, rollup, esbuild, etc.).
    wrapper = ctx.actions.declare_file("{}_vite_wrapper.sh".format(ctx.label.name))
    wrapper_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated Vite bundler wrapper for " + str(ctx.label) + "\n" +
        "# This script is invoked by ts_bundle as a build action.\n" +
        "# CWD is the Bazel exec root when this runs.\n" +
        "set -euo pipefail\n" +
        "\n" +
        "# Capture exec root (CWD at script start) before any cd.\n" +
        "EXEC_ROOT=\"$(pwd)\"\n" +
        "\n" +
        "# Arguments (all exec-root-relative):\n" +
        "#   $1 = vite.config.mjs path\n" +
        "#   $2 = entry .js path\n" +
        "#   $3 = output directory path\n" +
        "#   $4 = HTML file path (\"\" when not in app mode, or no html attr)\n" +
        "#   $5 = staging manifest path (\"\" when staging_srcs is empty)\n" +
        "#   $6 = the lib-mode stylesheet Bazel declared (\"\" in app mode)\n" +
        "CONFIG=\"${EXEC_ROOT}/$1\"\n" +
        "\n" +
        "# Vite's config loader mkdirs '.vite-temp' in the nearest node_modules above\n" +
        "# the config, and the exec root's is a read-only symlink into the source tree.\n" +
        "mkdir -p \"$(dirname \"${CONFIG}\")/node_modules\"\n" +
        "\n" +
        "# Export absolute paths for the vite.config.mjs to read.\n" +
        "export EXEC_ROOT\n" +
        "export VITE_ENTRY_PATH=\"${EXEC_ROOT}/$2\"\n" +
        "export VITE_OUT_DIR=\"${EXEC_ROOT}/$3\"\n" +
        "# Export VITE_HTML_PATH when the 4th argument is non-empty (app mode).\n" +
        "# Copy the HTML into a staging dir inside bazel-out so that Vite/Rollup\n" +
        "# resolves it without following symlinks back to the source tree, which\n" +
        "# would produce a path outside EXEC_ROOT and cause Rollup to reject it.\n" +
        "if [[ -n \"${4:-}\" ]]; then\n" +
        "  HTML_STAGING=\"${EXEC_ROOT}/$3/../_html_staging\"\n" +
        "  mkdir -p \"${HTML_STAGING}\"\n" +
        "  cp -f \"${EXEC_ROOT}/$4\" \"${HTML_STAGING}/\"\n" +
        "  export VITE_HTML_PATH=\"${HTML_STAGING}/$(basename \"$4\")\"\n" +
        "fi\n" +
        "\n" +
        "# When a staging manifest is provided ($5), copy source files into a\n" +
        "# writable _staging/ directory. This lets framework Vite plugins (Remix,\n" +
        "# TanStack Start) scan route files and write codegen without hitting\n" +
        "# sandbox write-protection on the original source tree.\n" +
        "# The manifest has one line per file: <dest_rel_path>TAB<src_exec_path>\n" +
        "if [[ -n \"${5:-}\" ]]; then\n" +
        "  STAGING_DIR=\"${EXEC_ROOT}/$3/../_staging\"\n" +
        "  mkdir -p \"${STAGING_DIR}\"\n" +
        "  while IFS=$'\\t' read -r DEST SRC; do\n" +
        "    [[ -z \"${DEST}\" ]] && continue\n" +
        "    DEST_ABS=\"${STAGING_DIR}/${DEST}\"\n" +
        "    mkdir -p \"$(dirname \"${DEST_ABS}\")\"\n" +
        "    cp -f \"${EXEC_ROOT}/${SRC}\" \"${DEST_ABS}\"\n" +
        "  done < \"${EXEC_ROOT}/$5\"\n" +
        "  export VITE_STAGING_ROOT=\"${STAGING_DIR}\"\n" +
        "  # When the HTML file is also staged (it was listed in staging_srcs),\n" +
        "  # update VITE_HTML_PATH to the staged copy so that Rollup resolves the\n" +
        "  # HTML relative to the staging root (= vite.root). Without this fix,\n" +
        "  # the HTML is at _html_staging/index.html which is '../' from staging,\n" +
        "  # and Rollup rejects filenames with '..' path traversal.\n" +
        "  if [[ -n \"${VITE_HTML_PATH:-}\" ]]; then\n" +
        "    HTML_BASENAME=\"$(basename \"${VITE_HTML_PATH}\")\"\n" +
        "    STAGED_HTML=\"${STAGING_DIR}/${HTML_BASENAME}\"\n" +
        "    if [[ -f \"${STAGED_HTML}\" ]]; then\n" +
        "      export VITE_HTML_PATH=\"${STAGED_HTML}\"\n" +
        "    fi\n" +
        "  fi\n" +
        "fi\n" +
        "\n" +
        "# The node_modules tree artifact may have any name. If it is not already\n" +
        "# named 'node_modules', create a symlink so that Node.js ESM resolution\n" +
        "# (which traverses parent directories looking for 'node_modules/') works.\n" +
        "NM_ACTUAL=\"${EXEC_ROOT}/" + _shell_escape(node_modules_path) + "\"\n" +
        "NM_DIR=\"${EXEC_ROOT}/" + _shell_escape(parent_rel) + "\"\n" +
        (
            "" if actual_nm_basename == "node_modules" else "ln -sf \"${NM_ACTUAL}\" \"${NM_DIR}/node_modules\" 2>/dev/null || true\n"  # already named node_modules — no symlink needed
        ) +
        "\n" +
        "# cd to the parent of the node_modules tree so that Node.js ESM resolution\n" +
        "# can traverse directories and find node_modules/picomatch, etc.\n" +
        "cd \"${NM_DIR}\"\n" +
        "\n" +
        "RUNTIME=\"${EXEC_ROOT}/" + _shell_escape(runtime_rel) + "\"\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        "VITE_JS=\"" + vite_entry_rel + "\"\n" +
        "\n" +
        "# Lib mode emits the stylesheet only when something was extracted into it,\n" +
        "# so an entry importing no CSS would leave a declared output uncreated. An\n" +
        "# empty file put there first is overwritten by the real one when there is\n" +
        "# one: the generated config sets emptyOutDir false, so Vite does not clear\n" +
        "# the directory before writing. Done before the build so the exec below\n" +
        "# stays an exec.\n" +
        "if [[ -n \"${6:-}\" ]]; then\n" +
        "  mkdir -p \"$(dirname \"${EXEC_ROOT}/$6\")\"\n" +
        "  : > \"${EXEC_ROOT}/$6\"\n" +
        "fi\n" +
        "\n" +
        "\"$RUNTIME\" ${RUNTIME_ARGS[@]+\"${RUNTIME_ARGS[@]}\"} \"$VITE_JS\" build --config \"$CONFIG\"\n" +
        "\n" +
        "# Vite keys the manifest by module id relative to its root, which for an\n" +
        "# input living in bazel-out is a path escaping the sandbox and naming the\n" +
        "# build configuration. Rewriting to the workspace-relative tail makes a key\n" +
        "# something a consumer can look up and stable across -c opt.\n" +
        "MANIFEST=\"${EXEC_ROOT}/$3/manifest.json\"\n" +
        "if [[ -f \"${MANIFEST}\" ]]; then\n" +
        "  \"$RUNTIME\" -e '\n" +
        "    const fs = require(\"node:fs\");\n" +
        "    const p = process.argv[1];\n" +
        "    const strip = (v) =>\n" +
        "      typeof v === \"string\"\n" +
        "        ? v.replace(/^.*?bazel-out\\/[^/]+\\/bin\\//, \"\")\n" +
        "        : v;\n" +
        "    const src = JSON.parse(fs.readFileSync(p, \"utf8\"));\n" +
        "    const out = {};\n" +
        "    for (const [key, entry] of Object.entries(src)) {\n" +
        "      if (entry && typeof entry === \"object\" && \"src\" in entry) {\n" +
        "        entry.src = strip(entry.src);\n" +
        "      }\n" +
        "      out[strip(key)] = entry;\n" +
        "    }\n" +
        "    fs.writeFileSync(p, JSON.stringify(out, null, 2) + \"\\n\");\n" +
        "  ' \"${MANIFEST}\"\n" +
        "fi\n"
    )

    ctx.actions.write(
        output = wrapper,
        content = wrapper_content,
        is_executable = True,
    )

    # Collect all runtime deps: vite npm package files + node_modules tree + runtime.
    vite_npm_info = ctx.attr.vite[NpmPackageInfo]

    runtime_deps = depset(
        [wrapper, runtime_binary],
        transitive = [
            vite_npm_info.all_files,
            depset(node_modules_files),
        ],
    )

    return [
        BundlerInfo(
            bundler_binary = wrapper,
            config_file = None,
            runtime_deps = runtime_deps,
            use_generated_config = True,
        ),
        DefaultInfo(files = depset([wrapper])),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

vite_bundler = rule(
    implementation = _vite_bundler_impl,
    attrs = {
        "vite": attr.label(
            doc = "The @npm//:vite target providing the Vite npm package (NpmPackageInfo).",
            mandatory = True,
            providers = [NpmPackageInfo],
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target that includes vite and its transitive deps " +
                  "(picomatch, rollup, esbuild, etc.). The target may have any name; " +
                  "the wrapper script creates a 'node_modules' symlink at runtime so " +
                  "Node.js ESM resolution always finds the packages.",
            mandatory = True,
            allow_files = True,
        ),
        "runtime": attr.label(
            doc = "Per-target override for the node binary that runs Vite. " +
                  "When set, takes priority over the js_tool toolchain.",
            allow_single_file = True,
            cfg = "exec",
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Declares a Vite bundler instance that satisfies the BundlerInfo interface.

Returns a BundlerInfo provider wiring Vite into the ts_bundle / ts_binary
bundler plugin interface.

The node_modules attr may point to a node_modules() target with any name.
The generated wrapper script creates a 'node_modules' symlink at runtime so
Node.js ESM resolution can find Vite's dependencies regardless of the target
name.

The generated wrapper script is invoked by ts_bundle as a build action. It
creates the node_modules symlink, cd's to the parent of the node_modules tree,
and runs:
  node node_modules/vite/bin/vite.js build --config <config>

Example:

    load("@rules_typescript//npm:defs.bzl", "node_modules")
    load("@rules_typescript//vite:bundler.bzl", "vite_bundler")

    node_modules(
        name = "vite_deps",   # Any name works
        deps = ["@npm//:vite"],
    )

    vite_bundler(
        name = "vite",
        vite = "@npm//:vite",
        node_modules = ":vite_deps",
    )
""",
)
