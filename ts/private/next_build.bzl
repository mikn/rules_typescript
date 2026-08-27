"""Next.js build rule for rules_typescript.

next_build wraps `next build` as a single opaque Bazel action. It is designed
for the hybrid monorepo pattern where:

  - Shared TypeScript libraries use ts_compile for fast, incremental builds
    with .d.ts compilation boundaries (sub-second caching).
  - The Next.js application shell uses next_build as a single Bazel action.

The hybrid pattern:

    packages/
      shared/     → ts_compile (fast, incremental, .d.ts boundary)
      ui/         → ts_compile (fast, incremental)
    apps/
      web/        → next_build (opaque, wraps `next build`)

Shared libraries provide JsInfo (compiled .js files). next_build creates a
staging directory that contains:

  1. The app source files (.tsx, .ts, .css, public/ assets from srcs attr).
  2. A symlink to the Bazel-built node_modules tree.
  3. The user's next.config.mjs (from config attr) and anything it imports
     (from config_srcs attr).
  4. An optional tsconfig.json (from tsconfig attr).
  5. Source files from staging_srcs, placed at their package-relative paths
     so that Next.js resolves them via relative imports or path mappings.

Nothing else is in the staging directory, which is what makes the file list a
real declaration: an import of a file the target does not list fails to resolve
rather than picking the file up off the developer's disk.

The action runs with `block-network`, so `next build` cannot download anything.
`next/font/google` fetches its woff2 payloads at build time and therefore fails
here; `allow_network = True` opts a target out and says so in the BUILD file.

Output: one declare_directory artifact holding the `.next/` build output. A
whole-directory output is right for this rule -- `next start` reads the tree by
name, and the file set depends on the routes Next.js decided to prerender -- so
the pruning is subtractive: `cache/` (a local incremental cache), `trace` and
`diagnostics/` (build telemetry carrying absolute paths and wall-clock times)
are removed. What remains is still not byte-reproducible: Next.js bakes the
absolute project path into its server bundles, and BUILD_ID is a random nanoid
unless next.config sets `generateBuildId`.

Usage:

    load("@rules_typescript//ts:defs.bzl", "next_build")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    node_modules(
        name = "node_modules",
        deps = [
            "@npm//:next",
            "@npm//:react",
            "@npm//:react-dom",
        ],
    )

    next_build(
        name = "app",
        srcs = glob(["app/**/*.tsx", "app/**/*.ts", "lib/**/*.ts"]),
        config = "next.config.mjs",
        tsconfig = "tsconfig.json",
        node_modules = ":node_modules",
    )

With shared packages (staging_srcs pattern):

    next_build(
        name = "app",
        srcs = glob(["app/**/*.tsx", "app/**/*.ts"]),
        staging_srcs = [
            "//packages/shared:sources",  # filegroup of .ts source files
            "//packages/ui:sources",
        ],
        config = "next.config.mjs",
        tsconfig = "tsconfig.json",
        node_modules = ":node_modules",
    )
"""

load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# Build telemetry Next.js writes beside the real output: `trace` records the
# absolute staging path of every span, `diagnostics/` the build wall clock.
# Both differ between two otherwise identical builds, and nothing serves them.
_TELEMETRY_OUTPUTS = ["trace", "diagnostics"]

_NETWORK_FAILURE_PATTERN = "ENETUNREACH|EAI_AGAIN|ECONNREFUSED|getaddrinfo|from Google Fonts"

_NETWORK_DIAGNOSTIC = """next_build: `next build` failed while reaching for the network.

This action runs with the network blocked, so nothing it builds may depend on a
download. `next/font/google` is the usual cause: it fetches the font CSS and the
woff2 payloads from fonts.googleapis.com while compiling.

Either declare the font locally -- `next/font/local`, with the font files listed
in `srcs` -- or set `allow_network = True` on this next_build target to accept a
build whose output depends on the network."""

def _staging_dest(src, pkg, attr):
    """Path, relative to the staging root, that `src` is copied to."""
    short = src.short_path
    if not pkg:
        return short
    if short.startswith(pkg + "/"):
        return short[len(pkg) + 1:]

    # Falling back to the basename put two files from different packages at the
    # same staging path, and neither where an import pointed.
    fail(("next_build: %s in `%s` is outside this target's package (%s), so it has no " +
          "path inside the Next.js project root. Files from another package go in " +
          "`staging_srcs`, which stages them at their workspace-relative paths.") %
         (short, attr, pkg))

# ─── Rule implementation ────────────────────────────────────────────────────────

def _next_build_impl(ctx):
    # `next build` runs inside the action, so node comes from the js_tool
    # (exec platform) toolchain.
    js_tool = get_js_tool(ctx)
    runtime_binary = js_tool.runtime_binary
    runtime_args = js_tool.args_prefix

    # ── Collect node_modules ──────────────────────────────────────────────────
    # Use the DefaultInfo file list to get the directory artifact directly.
    # ctx.files.node_modules expands TreeArtifact contents (individual files),
    # but we need the directory handle itself for the symlink target.
    nm_files = ctx.attr.node_modules[DefaultInfo].files.to_list()
    if not nm_files:
        fail(
            "next_build: 'node_modules' attr must be set to a node_modules() target " +
            "that contains next, react, react-dom and their transitive dependencies.\n" +
            "Example:\n" +
            "  node_modules(\n" +
            "      name = \"node_modules\",\n" +
            "      deps = [\"@npm//:next\", \"@npm//:react\", \"@npm//:react-dom\"],\n" +
            "  )\n" +
            "  next_build(\n" +
            "      name = \"app\",\n" +
            "      node_modules = \":node_modules\",\n" +
            "  )",
        )
    node_modules_dir = nm_files[0]
    node_modules_path = node_modules_dir.path

    # ── Source files ──────────────────────────────────────────────────────────
    srcs = ctx.files.srcs

    # ── next.config ───────────────────────────────────────────────────────────
    config_files = ctx.files.config
    config_file = config_files[0] if config_files else None

    # ── tsconfig ──────────────────────────────────────────────────────────────
    tsconfig_files = ctx.files.tsconfig
    tsconfig_file = tsconfig_files[0] if tsconfig_files else None

    # ── staging_srcs ──────────────────────────────────────────────────────────
    # Collect source files from staging_srcs (shared package source files).
    staging_srcs_files = ctx.files.staging_srcs

    # ── Output directory ──────────────────────────────────────────────────────
    # Declare the .next/ output as a directory artifact. Next.js writes its
    # build output here: .next/server/, .next/static/, .next/BUILD_ID, etc.
    out_dir = ctx.actions.declare_directory("{}_next_out".format(ctx.label.name))

    # ── Build the staging manifest ────────────────────────────────────────────
    # Each line in the manifest is: <dest_rel_path> TAB <src_exec_path>
    # dest_rel_path is relative to the staging dir root.
    # The staging dir layout mirrors the workspace layout from the package root.
    #
    # For srcs: strip the package path prefix to get the package-relative path.
    # For staging_srcs: use the short_path (workspace-relative) as-is so that
    #   packages/shared/src/index.ts lands at staging/packages/shared/src/index.ts.
    manifest_lines = []

    pkg = ctx.label.package

    # srcs and config_srcs are project files: they land at the path they have
    # inside the package, which is the Next.js project root in staging.
    #   pkg = "apps/web", short = "apps/web/app/page.tsx" → dest = "app/page.tsx"
    #   pkg = "",         short = "src/app/page.tsx"      → dest = "src/app/page.tsx"
    for src in srcs:
        manifest_lines.append("{}\t{}".format(_staging_dest(src, pkg, "srcs"), src.path))

    for src in ctx.files.config_srcs:
        manifest_lines.append("{}\t{}".format(_staging_dest(src, pkg, "config_srcs"), src.path))

    for src in staging_srcs_files:
        # staging_srcs files land at their workspace-relative paths.
        short = src.short_path

        # short_path can start with "../" for external repo files — skip those.
        if short.startswith("../"):
            continue
        manifest_lines.append("{}\t{}".format(short, src.path))

    manifest = ctx.actions.declare_file("{}_next_manifest.txt".format(ctx.label.name))
    ctx.actions.write(
        output = manifest,
        content = "\n".join(manifest_lines) + "\n",
    )

    # ── Generate the wrapper script ───────────────────────────────────────────
    # The staging root goes *inside* the declared output dir so it stays within
    # the sandbox-writable tree, which is what makes this work on RBE.

    manifest_path = _shell_escape(manifest.path)
    node_modules_path_esc = _shell_escape(node_modules_path)
    out_dir_path_esc = _shell_escape(out_dir.path)
    config_path_esc = _shell_escape(config_file.path) if config_file else ""
    config_basename_esc = _shell_escape(config_file.basename) if config_file else ""
    tsconfig_path_esc = _shell_escape(tsconfig_file.path) if tsconfig_file else ""

    # Escape the label name once and reuse everywhere it appears in shell context.
    label_name_esc = _shell_escape(ctx.label.name)

    runtime_cmd = '"${EXEC_ROOT}/' + _shell_escape(runtime_binary.path) + '"'

    runtime_args_str = " ".join(['"{}"'.format(_shell_escape(a)) for a in runtime_args])

    # Always symlink node_modules into the staging dir under the canonical
    # name "node_modules", regardless of the artifact's actual basename.
    # Node.js ESM module resolution requires the directory to be named
    # "node_modules" for parent-directory traversal to work.
    nm_symlink_line = 'ln -sf "${NM_ACTUAL}" "${STAGING_DIR}/node_modules"\n'

    # User-specified environment variables.
    env_exports = ""
    for k, v in ctx.attr.env.items():
        env_exports += 'export {}="{}"\n'.format(k, _shell_escape(v))

    # A build that runs with the network blocked fails deep inside a webpack
    # loader, so the wrapper names the cause rather than leaving the user to
    # recognise ENETUNREACH.
    network_diagnostic_block = ""
    if not ctx.attr.allow_network:
        network_diagnostic_block = (
            '  if grep -qE "' + _NETWORK_FAILURE_PATTERN + '" "${BUILD_LOG}"; then\n' +
            "    cat >&2 <<'NEXT_BUILD_NETWORK_EOF'\n" +
            _NETWORK_DIAGNOSTIC + "\n" +
            "NEXT_BUILD_NETWORK_EOF\n" +
            "  fi\n"
        )

    wrapper_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated Next.js build wrapper for " + str(ctx.label) + "\n" +
        "# This script is invoked by the next_build Bazel action.\n" +
        "# CWD is the Bazel exec root when this runs.\n" +
        "set -euo pipefail\n" +
        "\n" +
        "EXEC_ROOT=\"$(pwd)\"\n" +
        "\n" +
        "# Paths (exec-root-relative).\n" +
        'MANIFEST="${EXEC_ROOT}/' + manifest_path + '"\n' +
        'NM_ACTUAL="${EXEC_ROOT}/' + node_modules_path_esc + '"\n' +
        'OUT_DIR="${EXEC_ROOT}/' + out_dir_path_esc + '"\n' +
        "\n" +
        "# Create the staging directory *inside* OUT_DIR so it remains within the\n" +
        "# writable sandbox subtree on RBE (no '../' traversal needed).\n" +
        'STAGING_DIR="${OUT_DIR}/_staging"\n' +
        'mkdir -p "${STAGING_DIR}"\n' +
        "\n" +
        "# Copy source files from the manifest into the staging directory.\n" +
        "while IFS=$'\\t' read -r DEST SRC; do\n" +
        '  [[ -z "${DEST}" ]] && continue\n' +
        '  DEST_ABS="${STAGING_DIR}/${DEST}"\n' +
        '  mkdir -p "$(dirname "${DEST_ABS}")"\n' +
        '  cp -f "${EXEC_ROOT}/${SRC}" "${DEST_ABS}"\n' +
        'done < "${MANIFEST}"\n' +
        "\n" +
        "# Symlink node_modules into the staging dir.\n" +
        nm_symlink_line +
        "\n" +
        (
            "# Copy next.config into staging dir.\n" +
            'cp -f "${EXEC_ROOT}/' + config_path_esc + '" "${STAGING_DIR}/' + config_basename_esc + '"\n' +
            "\n" if config_file else ""
        ) +
        (
            "# Copy tsconfig.json into staging dir.\n" +
            'cp -f "${EXEC_ROOT}/' + tsconfig_path_esc + '" "${STAGING_DIR}/tsconfig.json"\n' +
            "\n" if tsconfig_file else ""
        ) +
        "# Next.js requires a package.json in the project directory. Naming\n" +
        "# typescript here would not help: Next.js resolves those modules rather\n" +
        "# than reading the manifest, so the node_modules tree is what carries them.\n" +
        'if [[ ! -f "${STAGING_DIR}/package.json" ]]; then\n' +
        "  printf '{\"name\":\"%s\",\"version\":\"0.0.0\",\"private\":true}\\n' " +
        '"' + label_name_esc + '" > "${STAGING_DIR}/package.json"\n' +
        "fi\n" +
        "\n" +
        "# Export user environment variables.\n" +
        env_exports +
        "\n" +
        "# Next.js build configuration for hermetic Bazel actions.\n" +
        "# Disable telemetry to avoid network calls.\n" +
        "export NEXT_TELEMETRY_DISABLED=1\n" +
        "# Skip Next.js's Node.js require() patching which can fail in sandbox envs.\n" +
        "export NEXT_PRIVATE_SKIP_PATCHING=1\n" +
        "\n" +
        "# Run next build inside the staging directory.\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        'NEXT_BIN="${NM_ACTUAL}/next/dist/bin/next"\n' +
        "NEXT_CMD=(" + runtime_cmd + ")\n" +
        'if [[ -n "${RUNTIME_ARGS[*]+set}" ]]; then\n' +
        '  NEXT_CMD+=("${RUNTIME_ARGS[@]}")\n' +
        "fi\n" +
        'NEXT_CMD+=("${NEXT_BIN}" build)\n' +
        "\n" +
        'cd "${STAGING_DIR}"\n' +
        "\n" +
        "# The log is kept only to classify a failure; a successful build deletes\n" +
        "# it before the output directory is handed back to Bazel.\n" +
        'BUILD_LOG="${OUT_DIR}/_next_build.log"\n' +
        "set +e\n" +
        '"${NEXT_CMD[@]}" 2>&1 | tee "${BUILD_LOG}"\n' +
        'NEXT_STATUS="${PIPESTATUS[0]}"\n' +
        "set -e\n" +
        'if [[ "${NEXT_STATUS}" -ne 0 ]]; then\n' +
        network_diagnostic_block +
        '  exit "${NEXT_STATUS}"\n' +
        "fi\n" +
        'rm -f "${BUILD_LOG}"\n' +
        "\n" +
        "# Move the .next/ output to OUT_DIR, minus the local cache and the\n" +
        "# build telemetry: neither is served, and both differ run to run.\n" +
        'if [[ ! -d "${STAGING_DIR}/.next" ]]; then\n' +
        '  echo "next_build: next build wrote no .next directory" >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        'mv "${STAGING_DIR}/.next/"* "${OUT_DIR}/"\n' +
        'rm -rf "${OUT_DIR}/cache"\n' +
        "".join([
            'rm -rf "${OUT_DIR}/' + name + '"\n'
            for name in _TELEMETRY_OUTPUTS
        ]) +
        "# Clean up the staging directory from inside OUT_DIR.\n" +
        'rm -rf "${OUT_DIR}/_staging"\n'
    )

    wrapper = ctx.actions.declare_file("{}_next_build_wrapper.sh".format(ctx.label.name))
    ctx.actions.write(
        output = wrapper,
        content = wrapper_content,
        is_executable = True,
    )

    # ── Build the action input depset ─────────────────────────────────────────
    direct_inputs = (
        [manifest, wrapper, runtime_binary] +
        srcs + ctx.files.config_srcs + staging_srcs_files + nm_files
    )
    if config_file:
        direct_inputs.append(config_file)
    if tsconfig_file:
        direct_inputs.append(tsconfig_file)

    # `next build` reaches the network on its own initiative -- telemetry,
    # next/font/google -- so the sandbox takes the option away instead of the
    # rule trusting an env var to have covered every caller.
    execution_requirements = {"block-network": ""}
    if ctx.attr.allow_network:
        execution_requirements = {"requires-network": ""}

    # ── Run the build action ──────────────────────────────────────────────────
    ctx.actions.run(
        inputs = depset(direct_inputs),
        outputs = [out_dir],
        executable = wrapper,
        mnemonic = "NextBuild",
        progress_message = "NextBuild %{label}",
        execution_requirements = execution_requirements,
        env = {
            "NEXT_TELEMETRY_DISABLED": "1",
        },
    )

    return [
        DefaultInfo(
            files = depset([out_dir]),
        ),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

next_build = rule(
    implementation = _next_build_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "TypeScript/TSX source files for the Next.js application " +
                  "(app/ directory, pages/ directory, lib/ files, etc.). Every file " +
                  "must be inside this target's package; one from elsewhere has no " +
                  "path inside the project root and belongs in `staging_srcs`.",
            allow_files = True,
            mandatory = True,
        ),
        "staging_srcs": attr.label_list(
            doc = """Filegroup targets whose files should be staged alongside the app sources.

Use this attr to provide shared TypeScript package sources to Next.js. The files
are placed at their workspace-relative paths inside the staging directory so that
relative imports (e.g. import { greet } from "../lib/greeting") resolve correctly.

Example BUILD.bazel:

    next_build(
        name = "app",
        srcs = glob(["app/**/*.tsx", "app/**/*.ts"]),
        staging_srcs = [
            "//packages/shared:sources",
            "//packages/ui:sources",
        ],
        config = "next.config.mjs",
        tsconfig = "tsconfig.json",
        node_modules = ":node_modules",
    )

Filegroups should use visibility = ["//visibility:public"] so they can be
referenced from the Next.js app's BUILD file.
""",
            allow_files = True,
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target containing next, react, react-dom, " +
                  "and all application dependencies. Required.",
            allow_files = True,
            mandatory = True,
        ),
        "config": attr.label(
            doc = "The next.config.js or next.config.mjs file. " +
                  "When omitted, Next.js uses its default configuration.",
            allow_single_file = True,
        ),
        "config_srcs": attr.label_list(
            doc = """Extra files the `config` file imports, staged beside it.

`config` is a single file, so a next.config.mjs that imports a sibling module
fails with ERR_MODULE_NOT_FOUND unless that module is staged too. These files
land at the same project-relative paths as `srcs`:

    next_build(
        name = "app",
        config = "next.config.mjs",       # imports "./next.shared.mjs"
        config_srcs = ["next.shared.mjs"],
        ...
    )
""",
            allow_files = True,
        ),
        "allow_network": attr.bool(
            doc = "Let `next build` reach the network. The action is otherwise " +
                  "run with `block-network`, which fails any build that depends " +
                  "on a download -- `next/font/google` is the usual one. Setting " +
                  "this to True makes the output depend on a remote host, so the " +
                  "same inputs no longer imply the same output.",
            default = False,
        ),
        "tsconfig": attr.label(
            doc = "An optional tsconfig.json file to stage into the Next.js project " +
                  "directory. When provided, Next.js and its SWC compiler will use " +
                  "this config for path aliases and compiler options. Without this, " +
                  "Next.js uses its built-in default TypeScript configuration.",
            allow_single_file = True,
        ),
        "env": attr.string_dict(
            doc = "Additional environment variables to set for the next build action. " +
                  "NEXT_TELEMETRY_DISABLED and NEXT_PRIVATE_SKIP_PATCHING are always set.",
            default = {},
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    doc = """Builds a Next.js application with `next build`.

Requires the js_tool toolchain (Node.js on the exec platform).

Produces a `.next/` directory artifact containing the compiled Next.js output
(server bundles, static assets, route manifests, etc.). `cache/`, `trace` and
`diagnostics/` are removed from it: they are a local incremental cache and
build telemetry, they carry absolute paths and wall-clock times, and nothing
serves from them.

The action runs with the network blocked. `next/font/google` downloads its
fonts while compiling, so it fails here with a diagnostic naming the cause;
`allow_network = True` accepts that non-hermeticity for a target that needs it.

The rule creates a writable staging directory *inside* the declared output
directory (`OUT_DIR/_staging`) so it is always within the sandbox-writable tree.
This is required for correctness on RBE and local sandboxed builds alike.

Source files are copied from the manifest into the staging directory. The
Bazel-built node_modules directory is symlinked in as `node_modules/`.
After `next build` completes, the `.next/` output is moved to the declared
output directory and the staging directory is removed.

For the hybrid monorepo pattern (shared ts_compile + Next.js app):

  1. Shared packages use ts_compile for fast type-checking and .d.ts caching.
  2. The Next.js app uses next_build with staging_srcs for shared sources.
  3. Shared source files are accessed via relative imports (no transpilePackages
     path rewriting needed — the files are physically present in staging).

If you have path aliases (e.g. `@/lib/*`), provide a tsconfig.json via the
`tsconfig` attr with the appropriate `paths` entries pointing at staging-relative
locations.

Example (standalone Next.js app):

    load("@rules_typescript//ts:defs.bzl", "next_build")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    node_modules(
        name = "node_modules",
        deps = [
            "@npm//:next",
            "@npm//:react",
            "@npm//:react-dom",
        ],
    )

    next_build(
        name = "app",
        srcs = glob([
            "app/**/*.tsx",
            "app/**/*.ts",
            "lib/**/*.ts",
        ]),
        config = "next.config.mjs",
        tsconfig = "tsconfig.json",
        node_modules = ":node_modules",
    )

Example (hybrid monorepo with shared packages):

    next_build(
        name = "app",
        srcs = glob(["app/**/*.tsx", "app/**/*.ts"]),
        staging_srcs = [
            "//packages/shared:sources",
            "//packages/ui:sources",
        ],
        config = "next.config.mjs",
        tsconfig = "tsconfig.json",
        node_modules = ":node_modules",
    )
""",
)
