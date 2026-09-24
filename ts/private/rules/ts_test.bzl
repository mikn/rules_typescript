"""ts_test: ts_compile's actions over the test files, run under a runner target.

The rule takes TS_COMPILE_ATTRS and the test attributes; compile_program
registers the actions a ts_compile over the same srcs would, less the
declaration emit, every dep under the test's tsconfig checked from its
sources, and the importer chain tsgo resolved against is what the tests run
in: the runfiles hold the chain's links and the store files the program
reaches at their own paths.
`runner` names the target providing TsTestRunnerInfo whose `launch` writes the
launcher config. docs/rules/ts-test.md is the reference.
"""

load(
    "//tools/launcher:launcher.bzl",
    "LAUNCHER_TOOLCHAINS",
    "declare_launcher",
    "rlocation_path",
)
load("//ts/private:node_modules.bzl", "runfiles_dir")
load("//ts/private:providers.bzl", "TsInfo", "TsTestRunnerInfo", "require_emitted")
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
_SOURCE_EXTENSIONS = ["ts", "tsx", "mts", "cts"]

def _same_package(a, b):
    return a.package == b.package and a.repo_name == b.repo_name

def _package_sources(ctx):
    return [
        dep[TsInfo].sources
        for dep in ctx.attr.deps
        if _same_package(dep.label, ctx.label)
    ]

# A src outside the test's package compiles into the test's output tree; the
# relative import from a compiled sibling reaches it at the src's own path.
def _placed_at_src_paths(ctx, program):
    pkg = ctx.label.package
    if not pkg:
        return {}
    placed = {}
    for src, outputs in program.emitted.items():
        if src.short_path.startswith(("../", pkg + "/")):
            continue
        for out in outputs:
            placed[out.short_path[len(pkg) + 1:]] = out
    return placed

def _chain(ctx, program):
    return struct(
        dirs = [importer.dir for importer in program.importers],
        rlocations = [
            runfiles_dir(ctx, importer.label)
            for importer in program.importers
        ],
        npm_files = program.npm_files,
    )

def _ts_test_impl(ctx):
    runner = ctx.attr.runner[TsTestRunnerInfo]
    supports_sources = getattr(runner, "supports_source_inputs", False)
    program = compile_program(
        ctx,
        es_modules = runner.es_modules,
        package_program = True,
        declarations = False,
    )

    if not supports_sources:
        require_emitted(ctx.label, program.info, "runner {}".format(ctx.attr.runner.label))

    linked = {info.package_name: True for info in program.packages}
    missing = [name for name in runner.packages if name not in linked]
    if missing:
        fail(("ts_test {}: {} runs the tests and needs {} in the " +
              "npm closure, which no dep provides.\nAdd the hub label " +
              "of each to deps.").format(
            ctx.label,
            ctx.attr.runner.label,
            ", ".join(missing),
        ))

    entry_extensions = _ENTRY_EXTENSIONS + (_SOURCE_EXTENSIONS if program.runtime_sources else [])
    entry_points = [f for f in program.js + program.runtime_sources if f.extension in entry_extensions]

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

    # The workspace members in the closure: the packages with no extracted
    # manifest.
    members = [info for info in program.packages if info.package_dir == None]
    chain = _chain(ctx, program)
    placed = _placed_at_src_paths(ctx, program)

    launched = runner.launch(ctx, struct(
        entry_points = entry_points,
        entry_extensions = entry_extensions,
        test_files_list = test_files_list,
        chain = chain,
        transitive_js = program.transitive_js,
        runtime_sources = program.transitive_runtime_sources,
        es_twins = program.es_twins,
        placed = placed,
        runtime_data_sets = [program.transitive_data, program.transitive_runtime_sources],
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
        ctx.files.srcs + ctx.files.data + launched.files
    )
    if runtime_binary:
        files.append(runtime_binary)
    runfiles = ctx.runfiles(
        files = files,
        transitive_files = depset(
            transitive = [launched.transitive_files, chain.npm_files],
        ),
        root_symlinks = launcher.root_symlinks,
        symlinks = launched.symlinks | placed,
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
              "over the Bazel layer (root, cacheDir, the module ids, " +
              "server.fs.allow, coverage.allowExternal), so every " +
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
        TS_COMPILE_ATTRS | _TEST_ATTRS | WORKERS_POOL_ATTRS,
        # Bazel reads a test's coverage merger from this attribute; the target
        # runs the tools toolchain's, docs/rules/ts-test.md § Coverage.
        _lcov_merger = attr.label(
            cfg = "exec",
            default = Label("//ts/toolchain:lcov_merger_resolved"),
            executable = True,
        ),
    ),
    fragments = ["platform"],
    toolchains = TS_COMPILE_TOOLCHAINS + LAUNCHER_TOOLCHAINS + [
        config_common.toolchain_type(
            JS_RUNTIME_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
    ],
    doc = """Compiles TypeScript tests with ts_compile's actions and runs them.

srcs, deps, tsconfig and node_modules are ts_compile's; a dep under the test's
tsconfig is checked from its sources, one program with the package's compile,
and a dep under another through its declarations. The program emits no
declarations of its own, so its srcs may hang off several roots: a file of
another package the tests import is a src, its compiled module held at the
src's own path in the runfiles. The importer chain tsgo
checked the tests against is what they run in: the chain's links and the store
files the program reaches sit in the runfiles at their own paths. `runner`
names the target that runs the compiled files, //ts/runners:vitest by default;
`config`, `data`, `coverage_provider` and `wrangler_config` are the vitest
runner's.
""",
)
