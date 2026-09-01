"""Executable binary rule that compiles, optionally bundles, and runs TypeScript.

ts_binary:
  - Takes an entry_point label (a ts_compile target, or a .js/.mjs/.cjs source)
  - Collects all transitive .js outputs from the target graph
  - Optionally invokes a pluggable bundler (via BundlerInfo) to produce a bundle
  - Writes a launcher config so `bazel run //target` works
  - Is executable = True

When a `bundler` attribute is provided (a target returning BundlerInfo), the
bundler CLI is invoked with a standard set of arguments and the launcher
executes the bundled output. Without a bundler the launcher executes the entry
point .js file directly (use ts_bundle for a non-executable bundle artifact).

Launcher behaviour (//tools/launcher, driven by the generated JSON config):
  - Resolves every path through the runfiles library, so a manifest-only
    runfiles layout works exactly like a symlink tree.
  - Looks up the Node runtime from the JS runtime toolchain if registered,
    otherwise falls back to system `node`.
  - Prepends toolchain args_prefix (e.g. --experimental-vm-modules) before the
    entry point path.
  - Forwards all positional arguments passed after `--` on the command line.
  - `TS_LAUNCHER_DUMP_CONFIG=1 bazel run //target` prints what it would exec.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:providers.bzl", "BundlerInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_bundle.bzl", "BUNDLE_ACTION_ATTRS", "create_bundle_action")

# ─── Executable implementation ─────────────────────────────────────────────────

_JS_ENTRY_EXTENSIONS = [".js", ".mjs", ".cjs"]

_TS_ENTRY_EXTENSIONS = [".ts", ".tsx", ".mts", ".cts"]

def _has_extension(file, extensions):
    for ext in extensions:
        if file.basename.endswith(ext):
            return True
    return False

def _js_file_entry_js_info(ctx, data_files):
    """Builds the JsInfo a plain JavaScript file at entry_point stands in for."""
    files = ctx.files.entry_point
    label = ctx.attr.entry_point.label
    entry = files[0] if len(files) == 1 else None
    if entry and _has_extension(entry, _JS_ENTRY_EXTENSIONS):
        return JsInfo(
            js_files = depset([entry]),
            js_map_files = depset([]),
            transitive_js_files = depset([entry] + data_files),
            transitive_js_map_files = depset([]),
        )
    if entry and _has_extension(entry, _TS_ENTRY_EXTENSIONS):
        fail(
            "ts_binary: entry_point '{ep}' is a TypeScript source, which this rule does not compile.\n".format(ep = label) +
            "Compile it with ts_compile and point entry_point at that target, or " +
            "hand entry_point an already-plain .js/.mjs/.cjs file.",
        )
    fail(
        "ts_binary: entry_point '{ep}' does not provide JsInfo and is not a JavaScript file.\n".format(ep = label) +
        "The entry_point attr must be a ts_compile target (or any target that provides JsInfo), " +
        "or a single {exts} file.\n".format(exts = "/".join(_JS_ENTRY_EXTENSIONS)) +
        "Did you mean: entry_point = \"//path/to:your_ts_compile_target\"?",
    )

def _ts_binary_impl(ctx):
    entry_point = ctx.attr.entry_point
    data_files = ctx.files.data
    if JsInfo in entry_point:
        entry_js_info = entry_point[JsInfo]
    else:
        entry_js_info = _js_file_entry_js_info(ctx, data_files)

    # Resolve the JS runtime (toolchain or fall back to system node).
    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

    bundler_target = ctx.attr.bundler
    bundle_out = None
    extra_outputs = []

    if bundler_target and BundlerInfo in bundler_target:
        # ── Bundler path: produce bundle and run it ────────────────────────────
        bundle_filename = ctx.attr.bundle_name if ctx.attr.bundle_name else ctx.label.name
        bundle_result = create_bundle_action(ctx, entry_js_info, bundle_filename)
        bundle_out = bundle_result.bundle_out
        extra_outputs = bundle_result.outputs

        entry_file = bundle_out

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
                            ef = ctx.attr.entry_file,
                            ep = ctx.attr.entry_point.label,
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
        entry_file = entry_js_file

        runtime_depset = depset(
            ([runtime_binary] if runtime_binary else []),
            transitive = [entry_js_info.transitive_js_files, entry_js_info.transitive_js_map_files],
        )

    # ── Launcher config ────────────────────────────────────────────────────────
    node_modules_files = ctx.files.node_modules
    config = {
        "label": str(ctx.label),
        "mode": "node",
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "node": {
            "entry": rlocation_path(ctx, entry_file),
        },
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)
    if node_modules_files:
        config["node"]["node_modules"] = rlocation_path(ctx, node_modules_files[0])

    launcher = declare_launcher(ctx, config)

    # ── Runfiles ───────────────────────────────────────────────────────────────
    explicit_runfiles = list(node_modules_files) + list(data_files) + launcher.files
    if runtime_binary:
        explicit_runfiles.append(runtime_binary)
    if bundle_out:
        explicit_runfiles.append(bundle_out)
        explicit_runfiles.extend(extra_outputs)

    runfiles = ctx.runfiles(
        files = explicit_runfiles,
        transitive_files = runtime_depset,
        root_symlinks = launcher.root_symlinks,
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
            executable = launcher.executable,
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
    attrs = LAUNCHER_ATTRS | BUNDLE_ACTION_ATTRS | {
        "entry_point": attr.label(
            doc = "The ts_compile target whose output is the binary entry point, or a single .js/.mjs/.cjs source file to run as-is.",
            allow_files = True,
            mandatory = True,
        ),
        "data": attr.label_list(
            doc = "Extra runfiles: sibling modules a source entry_point imports, fixtures, anything read at runtime.",
            allow_files = True,
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

Example (a plain JavaScript file as the entry point):
    ts_binary(
        name = "generate",
        entry_point = "generate.mjs",
        data = ["helpers.mjs"],
        node_modules = "//:node_modules",
    )
""",
)
