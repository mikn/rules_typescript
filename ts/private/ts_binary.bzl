"""ts_binary: runs a compiled entry point, or a BundlerInfo's bundle of it.

entry_point is a ts_compile target (or any target providing TsInfo) or a plain
.js/.mjs/.cjs file. With a `bundler` the bundle is what runs; without one the
entry's .js runs directly. The launcher (//tools/launcher, driven by the
generated JSON config) resolves every path through the runfiles library, runs
the JS runtime toolchain's node when one is registered and the system `node`
otherwise, prepends the toolchain's args_prefix, and forwards every argument
after `--`. `TS_LAUNCHER_DUMP_CONFIG=1 bazel run //target` prints what it
would exec.
"""

load(
    "//tools/launcher:launcher.bzl",
    "LAUNCHER_TOOLCHAINS",
    "declare_launcher",
    "rlocation_path",
)
load("//ts/private:bundle_action.bzl", "create_bundle_action")
load("//ts/private:node_modules.bzl", "runfiles_dir")
load(
    "//ts/private:providers.bzl",
    "BundlerInfo",
    "NodeModulesInfo",
    "TsInfo",
    "ts_info",
)
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

_JS_ENTRY_EXTENSIONS = [".js", ".mjs", ".cjs"]

_TS_ENTRY_EXTENSIONS = [".ts", ".tsx", ".mts", ".cts"]

def _has_extension(file, extensions):
    for ext in extensions:
        if file.basename.endswith(ext):
            return True
    return False

def _js_file_entry(ctx, data_files):
    """The TsInfo a plain JavaScript file at entry_point stands in for."""
    files = ctx.files.entry_point
    label = ctx.attr.entry_point.label
    entry = files[0] if len(files) == 1 else None
    if entry and _has_extension(entry, _JS_ENTRY_EXTENSIONS):
        modules = [
            f
            for f in data_files
            if _has_extension(f, _JS_ENTRY_EXTENSIONS)
        ]
        return ts_info(
            js = depset([entry]),
            data = depset([f for f in data_files if f not in modules]),
            transitive_js = depset([entry] + modules),
        )
    if entry and _has_extension(entry, _TS_ENTRY_EXTENSIONS):
        fail(
            ("ts_binary: entry_point '{}' is a TypeScript source, which this " +
             "rule does not compile.\nCompile it with ts_compile and point " +
             "entry_point at that target, or hand entry_point an " +
             "already-plain .js/.mjs/.cjs file.").format(label),
        )
    fail(
        ("ts_binary: entry_point '{}' does not provide TsInfo and is not a " +
         "JavaScript file.\nThe entry_point attr must be a ts_compile target " +
         "(or any target that provides TsInfo), or a single {} file.\nDid " +
         "you mean: entry_point = \"//path/to:your_ts_compile_target\"?")
            .format(label, "/".join(_JS_ENTRY_EXTENSIONS)),
    )

def _entry_js_file(ctx, entry):
    """The one .js the launcher runs, out of the entry's direct outputs."""
    label = ctx.attr.entry_point.label
    entry_js_files = entry.js.to_list()
    names = ", ".join([f.basename for f in entry_js_files])
    if not entry_js_files:
        fail(
            ("ts_binary: entry_point '{}' provides TsInfo but has no direct " +
             ".js outputs.\nEnsure the ts_compile target at entry_point has " +
             "at least one .ts source file in srcs.").format(label),
        )
    if len(entry_js_files) == 1:
        return entry_js_files[0]
    if ctx.attr.entry_file:
        wanted = ctx.attr.entry_file
        for ext in [".ts", ".tsx"]:
            if wanted.endswith(ext):
                wanted = wanted[:-len(ext)] + ".js"
        match = [f for f in entry_js_files if f.basename == wanted]
        if not match:
            fail(
                ("ts_binary: entry_file '{}' not found in entry_point '{}'." +
                 "\nAvailable .js files: {}").format(
                    ctx.attr.entry_file,
                    label,
                    names,
                ),
            )
        return match[0]
    index_match = [f for f in entry_js_files if f.basename == "index.js"]
    if index_match:
        return index_match[0]
    fail(
        ("ts_binary: entry_point '{}' produces {} .js files: {}.\nSet " +
         "entry_file = \"index.ts\" (or the filename you want), or add an " +
         "index.ts to the ts_compile target to use the default " +
         "convention.").format(label, len(entry_js_files), names),
    )

def _ts_binary_impl(ctx):
    entry_point = ctx.attr.entry_point
    data_files = ctx.files.data
    if TsInfo in entry_point:
        entry = entry_point[TsInfo]
    else:
        entry = _js_file_entry(ctx, data_files)

    if entry.transitive_runtime_sources:
        fail("{}: ts_binary requires emitted JavaScript for its complete dependency closure; enable emit on source-only dependencies before running this target.".format(ctx.label))

    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

    bundle_out = None
    extra_outputs = []
    if ctx.attr.bundler and BundlerInfo in ctx.attr.bundler:
        bundle_filename = ctx.attr.bundle_name if ctx.attr.bundle_name else ctx.label.name
        bundle_result = create_bundle_action(ctx, entry, bundle_filename)
        bundle_out = bundle_result.bundle_out
        extra_outputs = bundle_result.outputs
        entry_file = bundle_out
    else:
        entry_file = _entry_js_file(ctx, entry)

    runtime_depset = depset(
        ([runtime_binary] if runtime_binary else []),
        transitive = [
            entry.transitive_js,
            entry.transitive_js_maps,
            entry.transitive_data,
            entry.npm_files,
        ],
    )

    node_modules = ctx.attr.node_modules
    node_modules_files = depset()
    if node_modules:
        node_modules_files = node_modules[DefaultInfo].files
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
    if node_modules:
        config["node"]["node_modules"] = runfiles_dir(ctx, node_modules.label)

    launcher = declare_launcher(ctx, config)

    explicit_runfiles = list(data_files) + launcher.files
    if runtime_binary:
        explicit_runfiles.append(runtime_binary)
    if bundle_out:
        explicit_runfiles.append(bundle_out)
        explicit_runfiles.extend(extra_outputs)

    runfiles = ctx.runfiles(
        files = explicit_runfiles,
        transitive_files = depset(
            transitive = [runtime_depset, node_modules_files],
        ),
        root_symlinks = launcher.root_symlinks,
    )

    # As a dep, the target is its bundle, or the entry it runs.
    if bundle_out:
        info = ts_info(
            js = depset([bundle_out]),
            transitive_data = entry.transitive_data,
        )
        output_group = OutputGroupInfo(
            bundle = depset([bundle_out]),
            js_tree = entry.transitive_js,
        )
        default_files = depset(extra_outputs)
    else:
        info = entry
        output_group = OutputGroupInfo(js_tree = entry.transitive_js)
        default_files = depset([])

    return [
        DefaultInfo(
            executable = launcher.executable,
            files = default_files,
            runfiles = runfiles,
        ),
        info,
        output_group,
    ]

ts_binary = rule(
    implementation = _ts_binary_impl,
    executable = True,
    fragments = ["platform"],
    toolchains = LAUNCHER_TOOLCHAINS + [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    attrs = {
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
            doc = "The importer's `node_modules` target: its links and store " +
                  "trees are runfiles, and the directory is on NODE_PATH.",
            providers = [NodeModulesInfo],
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

Example (with bundler — run bundled output; `bundler` is any target returning BundlerInfo):
    ts_binary(
        name = "app",
        entry_point = "//src/app:app",
        bundler = ":bundler",
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
