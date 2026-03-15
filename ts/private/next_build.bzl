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

  1. The app source files (.tsx, .ts from srcs attr).
  2. A symlink to the Bazel-built node_modules tree.
  3. The user's next.config.mjs (from config attr).
  4. An optional tsconfig.json (from tsconfig attr).
  5. Source files from staging_srcs, placed at their package-relative paths
     so that Next.js resolves them via relative imports or path mappings.

Output: a declare_directory artifact containing the `.next/` build output.
The `.next/cache/` subdirectory is excluded to keep the output hermetic and
cacheable by Bazel's remote cache.

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

load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# ─── Rule implementation ────────────────────────────────────────────────────────

def _next_build_impl(ctx):
    # Resolve the JS runtime from the toolchain.
    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

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

    for src in srcs:
        short = src.short_path
        # Strip the package prefix from the short path to get a path relative
        # to the package directory (= the Next.js project root in staging).
        # Examples:
        #   pkg = "apps/web", short = "apps/web/app/page.tsx" → dest = "app/page.tsx"
        #   pkg = "",         short = "src/app/page.tsx"       → dest = "src/app/page.tsx"
        if pkg and short.startswith(pkg + "/"):
            dest_rel = short[len(pkg) + 1:]
        elif not pkg:
            # Root package: short_path is already workspace-relative and correct.
            dest_rel = short
        else:
            # Fallback: use basename (should not happen for properly structured sources).
            dest_rel = src.basename
        manifest_lines.append("{}\t{}".format(dest_rel, src.path))

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
    # The wrapper script:
    #  1. Creates a writable staging directory *inside* the declared output dir
    #     at OUT_DIR/_staging to stay within the writable sandbox tree (RBE-safe).
    #  2. Copies source files (from manifest) into the staging dir.
    #  3. Symlinks the node_modules tree into the staging dir.
    #  4. Copies the next.config file into the staging dir.
    #  5. Copies the tsconfig.json into the staging dir (if provided).
    #  6. Generates a minimal package.json in the staging dir (required by Next.js).
    #  7. Runs `next build` inside the staging dir.
    #  8. Moves the .next/ output to OUT_DIR, removes .next/cache/ and _staging.

    # Escape paths for embedding in the shell script.
    manifest_path = _shell_escape(manifest.path)
    node_modules_path_esc = _shell_escape(node_modules_path)
    out_dir_path_esc = _shell_escape(out_dir.path)
    config_path_esc = _shell_escape(config_file.path) if config_file else ""
    config_basename_esc = _shell_escape(config_file.basename) if config_file else ""
    tsconfig_path_esc = _shell_escape(tsconfig_file.path) if tsconfig_file else ""

    # Escape the label name once and reuse everywhere it appears in shell context.
    label_name_esc = _shell_escape(ctx.label.name)

    # Runtime invocation.
    if runtime_binary:
        runtime_rel_esc = _shell_escape(runtime_binary.path)
        runtime_cmd = '"${EXEC_ROOT}/' + runtime_rel_esc + '"'
    else:
        runtime_cmd = '"node"'

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
        'while IFS=$\'\\t\' read -r DEST SRC; do\n' +
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
            "\n"
            if config_file else ""
        ) +
        (
            "# Copy tsconfig.json into staging dir.\n" +
            'cp -f "${EXEC_ROOT}/' + tsconfig_path_esc + '" "${STAGING_DIR}/tsconfig.json"\n' +
            "\n"
            if tsconfig_file else ""
        ) +
        "# Generate a minimal package.json so Next.js can determine the project name.\n" +
        "# Next.js requires package.json to exist in the project directory.\n" +
        "# Include devDependencies for typescript/@types so Next.js does not try to\n" +
        "# auto-install them via npm (which would fail in the Bazel sandbox).\n" +
        'if [[ ! -f "${STAGING_DIR}/package.json" ]]; then\n' +
        "  printf '{\"name\":\"%s\",\"version\":\"0.0.0\",\"private\":true," +
        "\"devDependencies\":{\"typescript\":\"*\",\"@types/react\":\"*\",\"@types/node\":\"*\"}}\\n' " +
        '"' + label_name_esc + '" > "${STAGING_DIR}/package.json"\n' +
        "fi\n" +
        "\n" +
        "# Export user environment variables.\n" +
        env_exports +
        "\n" +
        "# Next.js build configuration for hermetic Bazel actions.\n" +
        "# Disable telemetry to avoid network calls.\n" +
        'export NEXT_TELEMETRY_DISABLED=1\n' +
        "# Skip Next.js's Node.js require() patching which can fail in sandbox envs.\n" +
        'export NEXT_PRIVATE_SKIP_PATCHING=1\n' +
        "\n" +
        "# Run next build inside the staging directory.\n" +
        'RUNTIME_ARGS=(' + runtime_args_str + ')\n' +
        'NEXT_BIN="${NM_ACTUAL}/next/dist/bin/next"\n' +
        "\n" +
        'cd "${STAGING_DIR}"\n' +
        "\n" +
        'if [[ -n "${RUNTIME_ARGS[*]+set}" ]]; then\n' +
        '  ' + runtime_cmd + ' "${RUNTIME_ARGS[@]}" "${NEXT_BIN}" build\n' +
        'else\n' +
        '  ' + runtime_cmd + ' "${NEXT_BIN}" build\n' +
        'fi\n' +
        "\n" +
        "# Move the .next/ output to OUT_DIR.\n" +
        "# Remove .next/cache/ to keep the Bazel output hermetic and cacheable.\n" +
        'mv "${STAGING_DIR}/.next/"* "${OUT_DIR}/" 2>/dev/null || true\n' +
        'rm -rf "${OUT_DIR}/cache" 2>/dev/null || true\n' +
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
    direct_inputs = [manifest, wrapper] + srcs + staging_srcs_files + nm_files
    if config_file:
        direct_inputs.append(config_file)
    if tsconfig_file:
        direct_inputs.append(tsconfig_file)
    if runtime_binary:
        direct_inputs.append(runtime_binary)

    # ── Run the build action ──────────────────────────────────────────────────
    ctx.actions.run(
        inputs = depset(direct_inputs),
        outputs = [out_dir],
        executable = wrapper,
        mnemonic = "NextBuild",
        progress_message = "NextBuild %{label}",
        # Next.js needs network-free operation. Tell it not to use telemetry etc.
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
                  "(app/ directory, pages/ directory, lib/ files, etc.).",
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
        "tsconfig": attr.label(
            doc = "An optional tsconfig.json file to stage into the Next.js project " +
                  "directory. When provided, Next.js and its SWC compiler will use " +
                  "this config for path aliases and compiler options. Without this, " +
                  "Next.js uses its built-in default TypeScript configuration.",
            allow_single_file = True,
        ),
        "env": attr.string_dict(
            doc = "Additional environment variables to set for the next build action. " +
                  "NEXT_TELEMETRY_DISABLED and NEXT_PRIVATE_STANDALONE are always set.",
            default = {},
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Builds a Next.js application with `next build`.

Produces a `.next/` directory artifact containing the compiled Next.js output
(server bundles, static assets, route manifests, etc.). The `.next/cache/`
directory is excluded from the output to keep the artifact hermetic and
cacheable by Bazel's remote cache.

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
