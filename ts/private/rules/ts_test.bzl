"""ts_test: ts_compile's actions over the test files, run under a runner target.

The rule takes TS_COMPILE_ATTRS and the test attributes; compile_program
registers the actions a ts_compile over the same srcs would, and the forest it
builds is the tree the tests run in. `runner` names the target providing
TsTestRunnerInfo whose `launch` writes the launcher config.
docs/rules/ts-test.md is the reference.
"""

load(
    "//tools/launcher:launcher.bzl",
    "LAUNCHER_ATTRS",
    "declare_launcher",
    "rlocation_path",
)
load("//ts/private:providers.bzl", "TsInfo", "TsTestRunnerInfo")
load(
    "//ts/private:runtime.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "get_js_runtime",
)
load("//ts/private/actions:workers_pool.bzl", "WORKERS_POOL_ATTRS")
load(
    "//ts/private/rules:ts_compile.bzl",
    "TS_COMPILE_ATTRS",
    "TS_COMPILE_TOOLCHAINS",
    "compile_program",
)

_ENTRY_EXTENSIONS = ["js", "jsx", "mjs", "cjs"]

# A test inside a member resolves the member's name through the nearest
# package.json, so the manifest as built stands at the member's own path.
def _member_manifests(ctx, members):
    out = {}
    for info in members:
        member_dir = info.package_root.removeprefix(ctx.bin_dir.path + "/")
        for f in info.all_files.to_list():
            if f.basename == "package.json" and not f.is_source:
                out[member_dir + "/package.json"] = f
    return out

def _without(files, paths):
    if not paths:
        return files
    return depset([f for f in files.to_list() if f.short_path not in paths])

def _same_package(a, b):
    return a.package == b.package and a.repo_name == b.repo_name

# Another package's files reach a test through `data`.
def _package_sources(ctx):
    return [
        dep[TsInfo].sources
        for dep in ctx.attr.deps
        if _same_package(dep.label, ctx.label)
    ]

def _ts_test_impl(ctx):
    runner = ctx.attr.runner[TsTestRunnerInfo]
    program = compile_program(ctx, es_modules = runner.es_modules)

    linked = {info.package_name: True for info in program.packages}
    missing = [name for name in runner.packages if name not in linked]
    if missing:
        fail(("ts_test {}: {} runs the tests and needs {} in the " +
              "node_modules tree, which no dep provides.\nAdd the hub label " +
              "of each to deps.").format(
            ctx.label,
            ctx.attr.runner.label,
            ", ".join(missing),
        ))

    entry_points = [f for f in program.js if f.extension in _ENTRY_EXTENSIONS]

    # The launcher shards over this list of runfiles paths.
    test_files_list = ctx.actions.declare_file(
        "{}_test_files.txt".format(ctx.label.name),
    )
    ctx.actions.write(
        output = test_files_list,
        content = "\n".join([
            rlocation_path(ctx, f)
            for f in entry_points
        ]) + "\n",
    )

    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

    # The workspace members in the tree: the packages with no extracted
    # manifest.
    members = [info for info in program.packages if info.package_dir == None]
    member_manifests = _member_manifests(ctx, members)
    node_modules_files = [program.forest] if program.forest else []

    launched = runner.launch(ctx, struct(
        entry_points = entry_points,
        test_files_list = test_files_list,
        node_modules_files = node_modules_files,
        transitive_js = program.transitive_js,
        es_twins = program.es_twins,
        runtime_data_sets = [
            _without(program.transitive_data, member_manifests),
        ],
        package_sources = _package_sources(ctx),
        inline_members = sorted({m.package_name: True for m in members}.keys()),
        runner = runner,
    ))

    config = {
        "label": str(ctx.label),
        "mode": launched.mode,
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "env": launched.env,
        launched.mode: launched.section,
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)
    launcher = declare_launcher(
        ctx,
        config,
        basename = "{}_test_launcher".format(ctx.label.name),
    )

    # The .js, the data srcs and the package's sources: each is in the sandbox
    # only because it is named here.
    files = (
        [test_files_list] + launcher.files + program.outputs +
        ctx.files.srcs + node_modules_files + ctx.files.data + launched.files
    )
    if runtime_binary:
        files.append(runtime_binary)
    runfiles = ctx.runfiles(
        files = files,
        transitive_files = launched.transitive_files,
        root_symlinks = launcher.root_symlinks,
        symlinks = dict(member_manifests) | launched.symlinks,
    )
    for target in ctx.attr.data:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    providers = [
        DefaultInfo(executable = launcher.executable, runfiles = runfiles),
        program.instrumented,
    ]
    output_groups = program.output_groups | launched.output_groups
    if output_groups:
        providers.append(OutputGroupInfo(**output_groups))
    return providers

_TEST_ATTRS = {
    "runner": attr.label(
        doc = "The target that runs the compiled tests: //ts/runners:vitest " +
              "(the default), //ts/runners:node_test for tests written " +
              "against node:test, or any target providing " +
              "TsTestRunnerInfo. A vitest attribute set under another " +
              "runner is an analysis error rather than a silently dropped " +
              "setting.",
        default = Label("//ts/runners:vitest"),
        providers = [TsTestRunnerInfo],
    ),
    "env": attr.string_dict(
        doc = "Additional environment variables for the test.",
    ),
    "config": attr.label(
        doc = "The vitest config file (.ts/.mts/.cts/.js/.mjs/.cjs), merged " +
              "over the Bazel layer (root, cacheDir, preserveSymlinks, " +
              "coverage.allowExternal, the Workers-pool half), so every " +
              "vitest setting is the file's, as under plain vitest. A " +
              "config that default-exports an array is read as a list of " +
              "vitest projects (test.projects). The modules it imports " +
              "relatively are `config_srcs`.",
        allow_single_file = [".ts", ".mts", ".cts", ".js", ".mjs", ".cjs"],
    ),
    "config_srcs": attr.label_list(
        doc = "The modules `config` imports relatively, and theirs, each " +
              "written at its own path in the runfiles tree, where the " +
              "config's imports resolve as in the checkout.",
        allow_files = True,
    ),
    "data": attr.label_list(
        doc = "Extra runfiles for the test: fixtures, anything read at run " +
              "time.",
        allow_files = True,
    ),
    "coverage_provider": attr.string(
        doc = "Vitest coverage provider (test.coverage.provider): \"v8\" " +
              "(vitest's own default) or \"istanbul\".  The matching " +
              "@vitest/coverage-* package must be in the target's deps.  A " +
              "test whose pool runs in a second runtime needs \"istanbul\", " +
              "which instruments at transform time; v8 reads counters out " +
              "of node's inspector, which such a runtime does not have.",
        default = "",
        values = ["", "v8", "istanbul"],
    ),
}

ts_test = rule(
    implementation = _ts_test_impl,
    test = True,
    attrs = dict(
        TS_COMPILE_ATTRS | _TEST_ATTRS | WORKERS_POOL_ATTRS | LAUNCHER_ATTRS,
        # The report names the compiled .js and Bazel's manifest the .ts, so
        # the merger is the rule's; docs/rules/ts-test.md § Coverage.
        _lcov_merger = attr.label(
            cfg = "exec",
            default = Label("//tools/lcov_merger"),
            executable = True,
        ),
    ),
    toolchains = TS_COMPILE_TOOLCHAINS + [
        config_common.toolchain_type(
            JS_RUNTIME_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
    ],
    doc = """Compiles TypeScript tests with ts_compile's actions and runs them.

srcs, deps and tsconfig are ts_compile's, and the node_modules forest tsgo
checked the tests against is the tree they run in. `runner` names the target
that runs the compiled files, //ts/runners:vitest by default; `config`, `data`,
`coverage_provider` and `wrangler_config` are the vitest runner's.
""",
)
