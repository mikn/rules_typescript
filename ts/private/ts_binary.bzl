"""Executable binary rule that compiles, optionally bundles, and runs TypeScript.

ts_binary:
  - Takes an entry_point label (a ts_compile target)
  - Collects all transitive .js outputs from the target graph
  - Optionally invokes a pluggable bundler (via BundlerInfo) to produce a bundle
  - Generates a runner script so `bazel run //target` works
  - Is executable = True

When a `bundler` attribute is provided (a target returning BundlerInfo), the
bundler CLI is invoked with a standard set of arguments and the runner script
executes the bundled output. Without a bundler the runner executes the entry
point .js file directly (use ts_bundle for a non-executable bundle artifact).

Runner script behaviour:
  - Resolves $RUNFILES_DIR (set by `bazel run`) as the runfiles root.
  - Looks up the Node runtime from the JS runtime toolchain if registered,
    otherwise falls back to system `node`.
  - Prepends toolchain args_prefix (e.g. --experimental-vm-modules) before the
    entry point path.
  - Forwards all positional arguments passed after `--` on the command line.
"""

load("//ts/private:providers.bzl", "BundlerInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_bundle.bzl", "create_bundle_action")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# ─── Executable implementation ─────────────────────────────────────────────────

def _ts_binary_impl(ctx):
    entry_point = ctx.attr.entry_point
    if JsInfo not in entry_point:
        fail(
            "ts_binary: entry_point '{ep}' does not provide JsInfo.\n".format(ep = ctx.attr.entry_point.label) +
            "The entry_point attr must be a ts_compile target (or any target that provides JsInfo).\n" +
            "Did you mean: entry_point = \"//path/to:your_ts_compile_target\"?",
        )

    entry_js_info = entry_point[JsInfo]

    # Resolve the JS runtime (toolchain or fall back to system node).
    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

    # Helper: convert a file's short_path to its runfiles-tree-relative path.
    #
    # Bazel runfiles layout with --nolegacy_external_runfiles (bzlmod default):
    #   $RUNFILES_DIR/_main/<short_path>          for main-workspace files
    #   $RUNFILES_DIR/<repo_name>/<path>          for external-repo files
    #
    # File.short_path encoding:
    #   main-workspace:   "path/to/file"          (no prefix)
    #   external-repo:    "../repo_name/path"      (leading "../")
    def _rl(short_path):
        if short_path.startswith("../"):
            return short_path[3:]  # strip leading "../"
        return "_main/" + short_path

    bundler_target = ctx.attr.bundler
    bundle_out = None
    extra_outputs = []

    if bundler_target and BundlerInfo in bundler_target:
        # ── Bundler path: produce bundle and run it ────────────────────────────
        bundle_filename = ctx.attr.bundle_name if ctx.attr.bundle_name else ctx.label.name
        bundle_result = create_bundle_action(ctx, entry_js_info, bundle_filename)
        bundle_out = bundle_result.bundle_out
        extra_outputs = bundle_result.outputs

        entry_path = _shell_escape(_rl(bundle_out.short_path))

        runtime_depset = depset(
            ([runtime_binary] if runtime_binary else []),
            transitive = [entry_js_info.transitive_js_files, entry_js_info.transitive_js_map_files],
        )
    else:
        # ── No bundler: run the entry point .js directly ───────────────────────
        # Materialise only the direct js_files (O(1) files per target).
        entry_js_files = entry_js_info.js_files.to_list()
        if not entry_js_files:
            fail(
            "ts_binary: entry_point '{ep}' provides JsInfo but has no direct .js outputs.\n".format(ep = ctx.attr.entry_point.label) +
            "Ensure the ts_compile target at entry_point has at least one .ts source file in srcs.",
        )
        if len(entry_js_files) != 1:
            # If entry_file is set, use it to select the right .js file.
            if ctx.attr.entry_file:
                wanted = ctx.attr.entry_file
                # Normalize: if user passed "index.ts", convert to "index.js"
                for ext in [".ts", ".tsx"]:
                    if wanted.endswith(ext):
                        wanted = wanted[:-len(ext)] + ".js"
                match = [f for f in entry_js_files if f.basename == wanted]
                if not match:
                    fail(
                    "ts_binary: entry_file '{ef}' not found in entry_point '{ep}'.\n".format(
                        ef = ctx.attr.entry_file, ep = ctx.attr.entry_point.label,
                    ) +
                    "Available .js files: {avail}".format(avail = ", ".join([f.basename for f in entry_js_files])),
                )
                entry_js_file = match[0]
            else:
                # No entry_file specified — try "index.js" convention.
                index_match = [f for f in entry_js_files if f.basename == "index.js"]
                if index_match:
                    entry_js_file = index_match[0]
                else:
                    fail(
                    "ts_binary: entry_point '{ep}' produces {n} .js files: {files}.\n".format(
                        ep = ctx.attr.entry_point.label,
                        n = len(entry_js_files),
                        files = ", ".join([f.basename for f in entry_js_files]),
                    ) +
                    "Set entry_file = \"index.ts\" (or the filename you want), or " +
                    "add an index.ts to the ts_compile target to use the default convention.",
                )
        else:
            entry_js_file = entry_js_files[0]
        entry_path = _shell_escape(_rl(entry_js_file.short_path))

        runtime_depset = depset(
            ([runtime_binary] if runtime_binary else []),
            transitive = [entry_js_info.transitive_js_files, entry_js_info.transitive_js_map_files],
        )

    # ── Optional node_modules for NODE_PATH ────────────────────────────────────
    node_modules_files = ctx.files.node_modules
    node_modules_path = _shell_escape(_rl(node_modules_files[0].short_path)) if node_modules_files else ""

    # ── Generate the runner script ─────────────────────────────────────────────
    runtime_path = _shell_escape(_rl(runtime_binary.short_path)) if runtime_binary else ""
    runtime_args_str = " ".join(["\"{}\"".format(_shell_escape(a)) for a in runtime_args])

    runner = ctx.actions.declare_file("{}_runner.sh".format(ctx.label.name))
    runner_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated runner for " + str(ctx.label) + "\n" +
        "set -euo pipefail\n" +
        "\n" +
        "# Resolve the runfiles root.\n" +
        "# `bazel run` sets RUNFILES_DIR; fall back to the .runfiles sibling of\n" +
        "# the script for direct invocation.\n" +
        "RUNFILES=\"${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}\"\n" +
        "\n" +
        "# Optional node_modules for NODE_PATH.\n" +
        "NODE_MODULES_REL=\"" + node_modules_path + "\"\n" +
        "if [[ -n \"$NODE_MODULES_REL\" && -d \"${RUNFILES}/${NODE_MODULES_REL}\" ]]; then\n" +
        "  export NODE_PATH=\"${RUNFILES}/${NODE_MODULES_REL}:${NODE_PATH:-}\"\n" +
        "fi\n" +
        "\n" +
        "# Resolve the JS runtime binary.\n" +
        "RUNTIME_REL=\"" + runtime_path + "\"\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        "if [[ -z \"$RUNTIME_REL\" ]]; then\n" +
        "  # No toolchain runtime: use system node.\n" +
        "  RUNTIME=\"node\"\n" +
        "else\n" +
        "  RUNTIME=\"${RUNFILES}/${RUNTIME_REL}\"\n" +
        "fi\n" +
        "\n" +
        "# Entry point (absolute path via runfiles).\n" +
        "ENTRY=\"${RUNFILES}/" + entry_path + "\"\n" +
        "\n" +
        "exec \"$RUNTIME\" \"${RUNTIME_ARGS[@]}\" \"$ENTRY\" \"$@\"\n"
    )

    ctx.actions.write(
        output = runner,
        content = runner_content,
        is_executable = True,
    )

    # ── Runfiles ───────────────────────────────────────────────────────────────
    explicit_runfiles = list(node_modules_files)
    if runtime_binary:
        explicit_runfiles.append(runtime_binary)
    if bundle_out:
        explicit_runfiles.append(bundle_out)
        explicit_runfiles.extend(extra_outputs)

    runfiles = ctx.runfiles(
        files = explicit_runfiles,
        transitive_files = runtime_depset,
    )

    # ── Providers ──────────────────────────────────────────────────────────────
    # Propagate JsInfo so ts_binary can be used as a dep of other rules.
    if bundle_out:
        js_info = JsInfo(
            js_files = depset([bundle_out]),
            js_map_files = depset([]),
            transitive_js_files = depset([bundle_out]),
            transitive_js_map_files = depset([]),
        )
        output_group = OutputGroupInfo(
            bundle = depset([bundle_out]),
            js_tree = entry_js_info.transitive_js_files,
        )
        default_files = depset(extra_outputs)
    else:
        js_info = JsInfo(
            js_files = entry_js_info.js_files,
            js_map_files = entry_js_info.js_map_files,
            transitive_js_files = entry_js_info.transitive_js_files,
            transitive_js_map_files = entry_js_info.transitive_js_map_files,
        )
        output_group = OutputGroupInfo(
            js_tree = entry_js_info.transitive_js_files,
        )
        default_files = depset([])

    return [
        DefaultInfo(
            executable = runner,
            files = default_files,
            runfiles = runfiles,
        ),
        js_info,
        output_group,
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_binary = rule(
    implementation = _ts_binary_impl,
    executable = True,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    attrs = {
        "entry_point": attr.label(
            doc = "The ts_compile target whose output is the binary entry point.",
            providers = [JsInfo],
            mandatory = True,
        ),
        "entry_file": attr.string(
            doc = "Source file name to use as the entry point when entry_point produces multiple .js files. E.g. 'index.ts'. If unset and the target has index.js, it is used by convention.",
            default = "",
        ),
        "bundler": attr.label(
            doc = "Optional target providing BundlerInfo. When set, the bundle output is executed. When absent, the entry point .js is run directly.",
            providers = [BundlerInfo],
            default = None,
        ),
        "bundle_name": attr.string(
            doc = "Name for the output bundle file (without extension). Defaults to the rule name. Only meaningful when bundler is set.",
            default = "",
        ),
        "format": attr.string(
            doc = "Output module format: 'esm', 'cjs', 'iife'. Passed to the bundler. Only meaningful when bundler is set.",
            default = "esm",
            values = ["esm", "cjs", "iife"],
        ),
        "sourcemap": attr.bool(
            doc = "Whether to emit a source map alongside the bundle. Only meaningful when bundler is set.",
            default = True,
        ),
        "external": attr.string_list(
            doc = "Module specifiers to mark as external (not bundled). Only meaningful when bundler is set.",
        ),
        "define": attr.string_dict(
            doc = "Global constant replacements. Only meaningful when bundler is set.",
        ),
        "node_modules": attr.label(
            doc = "Optional node_modules target. When set, its files are added to NODE_PATH at runtime.",
            allow_files = True,
        ),
    },
    doc = """Produces an executable binary from a TypeScript entry point.

`bazel run //target` executes the compiled JavaScript using the registered
JS runtime (Node by default). When a bundler target is provided, the bundled
output is executed; otherwise the entry point .js file is run directly.

Example (no bundler — run entry point .js directly):
    ts_binary(
        name = "app",
        entry_point = "//src/app:app",
    )

Example (with bundler — run bundled output):
    ts_binary(
        name = "app",
        entry_point = "//src/app:app",
        bundler = ":vite",
        format = "cjs",
    )
""",
)
