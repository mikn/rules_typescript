"""ts_test: ts_compile's actions over the test files, run under a runner target.

The rule takes TS_COMPILE_ATTRS and the test attributes; compile_program
registers the actions a ts_compile over the same srcs would, and the forest it
builds is the tree the tests run in. `runner` names the target providing
TsTestRunnerInfo whose `launch` writes the launcher config. The macro compiles
the TypeScript entries of `setup_files` and `global_setup` before declaring the
rule. docs/rules/ts-test.md is the reference.
"""

load(
    "//tools/launcher:launcher.bzl",
    "LAUNCHER_ATTRS",
    "declare_launcher",
    "rlocation_path",
)
load("//ts/private:providers.bzl", "JsInfo", "TsTestRunnerInfo")
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
    "ts_compile",
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
        dep[JsInfo].source_files
        for dep in ctx.attr.deps
        if JsInfo in dep and _same_package(dep.label, ctx.label)
    ]

def _ts_test_impl(ctx):
    program = compile_program(ctx)
    runner = ctx.attr.runner[TsTestRunnerInfo]

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
    if ctx.file.runtime:
        runtime_binary = ctx.file.runtime
    else:
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
    for target in ctx.attr.data + launched.runfiles_of:
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
    "vitest": attr.label(
        doc = "Explicit label for the vitest binary.",
        allow_single_file = True,
        executable = True,
        cfg = "exec",
    ),
    "runtime": attr.label(
        doc = "Per-target override for the JS runtime binary (e.g. a custom " +
              "Node wrapper). When set, takes priority over the js_runtime " +
              "toolchain.",
        allow_single_file = True,
        executable = True,
        cfg = "exec",
    ),
    "env": attr.string_dict(
        doc = "Additional environment variables for the test.",
    ),
    "environment": attr.string(
        doc = "Vitest test environment, e.g. 'node', 'jsdom', 'happy-dom', " +
              "'edge-runtime', or the name of a custom vitest environment " +
              "package.  Any value vitest accepts is allowed; the matching " +
              "package (jsdom, happy-dom, ...) must be in the target's " +
              "deps.  Emitted as test.environment in the generated config, " +
              "where it overrides an environment set by the `config` attr.",
        default = "",
    ),
    "coverage": attr.bool(
        doc = "When True, also enables vitest coverage instrumentation " +
              "during normal `bazel test` runs (in addition to `bazel " +
              "coverage`).  Coverage during `bazel coverage` is always " +
              "enabled regardless of this attr — `bazel coverage " +
              "//path:test` works on every vitest ts_test target without " +
              "any opt-in.  Requires the @vitest/coverage-* package " +
              "matching `coverage_provider` to be present in node_modules.",
        default = False,
    ),
    "config": attr.label(
        doc = "A vitest config file (.ts/.mts/.js/.mjs).  It is MERGED into " +
              "the generated config rather than replacing it, so the " +
              "Bazel-owned layer (root, cacheDir, preserveSymlinks, " +
              "coverage.allowExternal, the Workers-pool half) survives.  A " +
              "config that default-exports an array is read as a list of " +
              "vitest projects (test.projects).  The modules it imports " +
              "relatively are `config_srcs`.",
        allow_single_file = [".ts", ".mts", ".cts", ".js", ".mjs", ".cjs"],
    ),
    "config_srcs": attr.label_list(
        doc = "The modules `config` imports relatively, and theirs: staged " +
              "with the config's copy at their paths relative to the " +
              "config's package, so its imports resolve there.  A file " +
              "outside that package is an analysis error.",
        allow_files = True,
    ),
    "config_json": attr.string(
        doc = "Inline vitest config as a JSON object, occupying the same " +
              "precedence layer as the `config` file.  Set through the " +
              "ts_test macro by passing a dict to `config`.",
        default = "",
    ),
    "setup_files": attr.label_list(
        doc = "Files run before each test file (vitest test.setupFiles).  " +
              "TypeScript sources are compiled by the ts_test macro; the " +
              "rule itself takes the compiled .js.  Appended after any " +
              "setupFiles from the `config` attr.",
        allow_files = True,
    ),
    "global_setup": attr.label_list(
        doc = "Files run once for the whole test run (vitest " +
              "test.globalSetup).  TypeScript sources are compiled by the " +
              "ts_test macro.",
        allow_files = True,
    ),
    "data": attr.label_list(
        doc = "Extra runfiles for the test: fixtures, files a `setup_files` " +
              "entry imports, anything read at runtime.",
        allow_files = True,
    ),
    "snapshots": attr.label_list(
        doc = "Checked-in vitest snapshot files (__snapshots__/*.snap). " +
              "Listing them makes them readable inside the test sandbox, " +
              "which is what turns a stale snapshot into a failure.",
        allow_files = [".snap"],
    ),
    "globals": attr.bool(
        doc = "Enables vitest's global describe/it/expect (test.globals). " +
              "The matching `types` entry is the tsconfig's; this attr is " +
              "only the runtime one.",
        default = False,
    ),
    "reporters": attr.string_list(
        doc = "Vitest reporters (test.reporters), e.g. " +
              "[\"default\", \"junit\"].",
    ),
    "coverage_thresholds": attr.string_dict(
        doc = "Coverage thresholds (test.coverage.thresholds), e.g. " +
              "{\"lines\": \"80\", \"perFile\": \"true\"}.  Values that " +
              "look like numbers or booleans are emitted as such.  Only " +
              "enforced when coverage runs: `bazel coverage`, or `bazel " +
              "test` with coverage = True.",
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
        # Bazel's coverage protocol: `bazel coverage` merges the shards' files
        # with this binary, --coverage_output_generator's or bazel_tools'.
        _lcov_merger = attr.label(
            cfg = "exec",
            default = configuration_field(
                fragment = "coverage",
                name = "output_generator",
            ),
            executable = True,
        ),
    ),
    fragments = ["coverage"],
    toolchains = TS_COMPILE_TOOLCHAINS + [
        config_common.toolchain_type(
            JS_RUNTIME_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
    ],
    doc = """Compiles TypeScript tests with ts_compile's actions and runs them.

srcs, deps and tsconfig are ts_compile's, and the node_modules forest tsgo
checked the tests against is the tree they run in. `runner` names the target
that runs the compiled files, //ts/runners:vitest by default; the remaining
attributes are the vitest runner's.
""",
)

def _compile_setup_sources(name, sources, deps, tsconfig, visibility, tags):
    """Compiles the .ts/.tsx entries of `sources`, passing the rest through."""
    ts_sources = [s for s in sources if s.endswith(".ts") or s.endswith(".tsx")]
    if not ts_sources:
        return sources
    ts_compile(
        name = name,
        srcs = ts_sources,
        deps = deps,
        tsconfig = tsconfig,
        visibility = visibility,
        tags = tags,
    )
    return [":" + name] + [s for s in sources if s not in ts_sources]

def ts_test_macro(
        name,
        srcs,
        deps = [],
        tsconfig = None,
        config = None,
        setup_files = [],
        global_setup = [],
        tags = [],
        visibility = None,
        **kwargs):
    """Declares a ts_test over `srcs`, its setup entries compiled.

    Args:
        name: the test's name.
        srcs: the test files, as ts_compile's srcs.
        deps: what the tests import, as ts_compile's deps.
        tsconfig: the test program's tsconfig, as ts_compile's; the setup
            compiles take it too.
        config: a vitest config file's label, or an inline dict.
        setup_files: vitest's setupFiles; a .ts/.tsx entry is compiled with
            `deps` and `tsconfig`, the rest pass through.
        global_setup: vitest's globalSetup, compiled like setup_files.
        tags: the test's tags; `manual` reaches the setup compiles too.
        visibility: the test's, and the setup compiles' -- public when
            unset, so an IDE tsconfig can name them.
        **kwargs: every other attribute of the rule.
    """
    compile_visibility = visibility if visibility else ["//visibility:public"]

    # `manual` reaches the setup compiles: a wildcard that skips the test must
    # not analyse them.
    wildcard_tags = ["manual"] if "manual" in tags else []

    if type(config) == "dict":
        kwargs["config_json"] = json.encode(config)
    elif config:
        kwargs["config"] = config

    ts_test(
        name = name,
        srcs = srcs,
        deps = deps,
        tsconfig = tsconfig,
        setup_files = _compile_setup_sources(
            name = "_{}_setup".format(name),
            sources = setup_files,
            deps = deps,
            tsconfig = tsconfig,
            visibility = compile_visibility,
            tags = wildcard_tags,
        ),
        global_setup = _compile_setup_sources(
            name = "_{}_global_setup".format(name),
            sources = global_setup,
            deps = deps,
            tsconfig = tsconfig,
            visibility = compile_visibility,
            tags = wildcard_tags,
        ),
        tags = tags,
        visibility = visibility,
        **kwargs
    )
