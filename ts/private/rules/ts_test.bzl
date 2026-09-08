"""ts_test: compiles TypeScript tests and runs them under vitest or node:test.

The macro creates a ts_compile over `srcs`, a node_modules tree from `deps`
(the npm deps, their closures and every other dep's npm closure), compiles
the TypeScript `setup_files` and `global_setup`, and a runner rule over the
compiled .js: the test, plus the `.update_snapshots` and `.reads` companions
under vitest. The runner rule assembles the runfiles and the launcher config;
the vitest config is actions/vitest.bzl's and the Workers pool's half of the
environment actions/workers_pool.bzl's. docs/rules/ts-test.md is the
reference: the config layers, snapshots, sharding, coverage and `.reads`.
"""

load(
    "//tools/launcher:launcher.bzl",
    "LAUNCHER_ATTRS",
    "declare_launcher",
    "rlocation_path",
)
load(
    "//ts/private:node_modules.bzl",
    "build_node_modules_action",
    "collect_npm_packages",
)
load(
    "//ts/private:providers.bzl",
    "JsInfo",
    "NpmPackageInfo",
    "TsDeclarationInfo",
)
load(
    "//ts/private:runtime.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "get_js_runtime",
)
load(
    "//ts/private/actions:vitest.bzl",
    "tsconfig_paths_action",
    "vitest_config_action",
)
load(
    "//ts/private/actions:workers_pool.bzl",
    "WORKERS_POOL_ATTRS",
    "workers_pool_environment",
)
load("//ts/private/rules:ts_compile.bzl", "ts_compile")

# No provider constraint on deps: the ts_test macro passes its whole deps list,
# @npm// labels and ts_compile targets alike.

def _ts_auto_node_modules_impl(ctx):
    direct = [
        dep[NpmPackageInfo]
        for dep in ctx.attr.deps
        if NpmPackageInfo in dep
    ]

    # A dep's compiled JS value-imports the packages it declared, so the tree
    # follows the closure its declarations already carry; the test's own first.
    dep_closure = depset(
        transitive = [
            dep[TsDeclarationInfo].transitive_npm_packages
            for dep in ctx.attr.deps
            if TsDeclarationInfo in dep and NpmPackageInfo not in dep
        ],
        order = "postorder",
    )
    packages_to_link = collect_npm_packages(direct + dep_closure.to_list())

    input_file_sets = [npm_info.all_files for npm_info in packages_to_link]

    # The leaf is literally node_modules, for Node's parent-directory walk; the
    # target-named directory above it keeps two tests in one package apart.
    out_dir = build_node_modules_action(
        ctx,
        packages_to_link,
        input_file_sets,
        output_name = "{}/node_modules".format(ctx.label.name),
    )

    return [
        DefaultInfo(
            files = depset([out_dir]),
            runfiles = ctx.runfiles(files = [out_dir]),
        ),
    ]

_ts_auto_node_modules = rule(
    implementation = _ts_auto_node_modules_impl,
    attrs = {
        "deps": attr.label_list(
            doc = "Any deps: an npm package is linked, and every other dep " +
                  "contributes the npm closure its TsDeclarationInfo carries.",
        ),
    },
    toolchains = [
        # The exec-platform tool builds the tree, and mandatory: a misconfigured
        # setup fails here instead of falling back silently.
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    doc = "Internal rule: builds a node_modules tree from the deps' npm " +
          "packages and the npm closure of every other dep.",
)

# node:test is configured by CLI flags and the test file alone, so the rule
# rejects the vitest-shaped attrs rather than generating a config nothing reads.

RUNNER_VITEST = "vitest"
RUNNER_NODE_TEST = "node:test"

_RUNNERS = [RUNNER_VITEST, RUNNER_NODE_TEST]

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

def _ts_test_runner_impl(ctx):
    transitive_js_sets = []
    transitive_data_sets = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_data_sets.append(dep[JsInfo].transitive_data_files)

    transitive_js = depset(
        transitive = transitive_js_sets,
        order = "postorder",
    )

    test_js_files = ctx.files.compiled_tests

    # DefaultInfo also carries the .d.ts and .js.map beside every module; vitest
    # takes this list as the files to run.
    test_entry_points = [
        f
        for f in test_js_files
        if f.extension in ("js", "jsx", "mjs", "cjs")
    ]

    node_modules_files = ctx.files.node_modules

    # The workspace members in the tree: the packages with no extracted
    # manifest.
    npm_direct = [
        dep[NpmPackageInfo]
        for dep in ctx.attr.deps
        if NpmPackageInfo in dep
    ]
    npm_closure = depset(
        transitive = [
            dep[TsDeclarationInfo].transitive_npm_packages
            for dep in ctx.attr.deps
            if TsDeclarationInfo in dep and NpmPackageInfo not in dep
        ],
        order = "postorder",
    )
    members = [
        info
        for info in collect_npm_packages(npm_direct + npm_closure.to_list())
        if info.package_dir == None
    ]
    inline_members = sorted({m.package_name: True for m in members}.keys())
    member_manifests = _member_manifests(ctx, members)
    runtime_data_sets = [_without(
        depset(transitive = transitive_data_sets),
        member_manifests,
    )]
    package_sources = _package_sources(ctx)

    vitest_bin = ctx.file.vitest  # may be None
    vitest_is_npm_bin = vitest_bin != None

    runtime_binary = None
    runtime_args = []
    if ctx.file.runtime:
        runtime_binary = ctx.file.runtime
    else:
        js_runtime = get_js_runtime(ctx)
        if js_runtime:
            runtime_binary = js_runtime.runtime_binary
            runtime_args = js_runtime.args_prefix

    # The launcher shards over this list of runfiles paths.
    test_files_list = ctx.actions.declare_file(
        "{}_test_files.txt".format(ctx.label.name),
    )
    ctx.actions.write(
        output = test_files_list,
        content = "\n".join([
            rlocation_path(ctx, f)
            for f in test_entry_points
        ]) + "\n",
    )

    if ctx.attr.runner == RUNNER_NODE_TEST:
        return _node_test_providers(
            ctx,
            runtime_files = depset(
                transitive = [transitive_js] + runtime_data_sets +
                             package_sources,
            ),
            test_files_list = test_files_list,
            node_modules_files = node_modules_files,
            runtime_binary = runtime_binary,
            runtime_args = runtime_args,
            symlinks = member_manifests,
        )

    pool = workers_pool_environment(ctx, node_modules_files, runtime_data_sets)
    runtime_data_sets = pool.runtime_data_sets
    tsconfig_paths = tsconfig_paths_action(ctx)
    written = vitest_config_action(
        ctx,
        test_entry_points = test_entry_points,
        node_modules_files = node_modules_files,
        pool_layer = pool.layer,
        tsconfig_paths = tsconfig_paths,
        inline_members = inline_members,
    )
    vitest_config = written.config

    # Every path is a runfiles path; the launcher resolves them through the
    # runfiles library, so manifest-only layouts work like symlink trees.
    vitest_cfg = {
        "config_file": rlocation_path(ctx, vitest_config),
        "test_files_list": rlocation_path(ctx, test_files_list),
        "update_snapshots": ctx.attr.update_snapshots,
        "coverage": ctx.attr.coverage,
    }
    if vitest_bin:
        vitest_cfg["vitest"] = rlocation_path(ctx, vitest_bin)

        # An npm_bin wrapper resolves its own Node; prefixing the runtime again
        # would run a launcher under a launcher.
        vitest_cfg["vitest_is_npm_bin"] = vitest_is_npm_bin
    if node_modules_files:
        vitest_cfg["node_modules"] = rlocation_path(ctx, node_modules_files[0])

        # The canonical bin entry from vitest's package.json#bin, reached inside
        # the node_modules tree artifact.
        vitest_cfg["vitest_in_tree"] = "vitest/vitest.mjs"
    if ctx.attr.reads_report:
        vitest_cfg["reads_hook"] = rlocation_path(ctx, ctx.file._reads_hook)

    # A sandboxed test must fail on a stale or missing .snap, never write one,
    # and vitest only stops writing when it believes it is running in CI.
    env = dict(ctx.attr.env)
    if not ctx.attr.update_snapshots:
        env.setdefault("CI", "true")

    config = {
        "label": str(ctx.label),
        "mode": "vitest",
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "env": env,
        "vitest": vitest_cfg,
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)

    launcher = declare_launcher(
        ctx,
        config,
        basename = "{}_test_launcher".format(ctx.label.name),
    )

    runfiles_files = (
        [test_files_list, vitest_config] + launcher.files +
        test_js_files +
        ctx.files.srcs +
        node_modules_files +
        ctx.files.setup_files +
        ctx.files.global_setup +
        ctx.files.data +
        ctx.files.snapshots
    )
    if vitest_bin:
        runfiles_files.append(vitest_bin)
    if runtime_binary:
        runfiles_files.append(runtime_binary)
    runfiles_files += written.user_config_files + pool.files
    if tsconfig_paths:
        runfiles_files.append(tsconfig_paths)
    if ctx.attr.reads_report:
        runfiles_files.append(ctx.file._reads_hook)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        # The .js, the data srcs and the package's sources: each is in the
        # sandbox only because it is named here.
        transitive_files = depset(
            transitive = [transitive_js] + runtime_data_sets + package_sources,
        ),
        root_symlinks = launcher.root_symlinks,
        symlinks = dict(member_manifests) | pool.symlinks,
    )
    for target in ctx.attr.data + ctx.attr.setup_files + ctx.attr.global_setup:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    # An npm_bin vitest is itself launcher-driven, so it needs its own config
    # staged in this test's runfiles, not just its executable.
    if ctx.attr.vitest:
        runfiles = runfiles.merge(ctx.attr.vitest[DefaultInfo].default_runfiles)

    return [
        DefaultInfo(
            executable = launcher.executable,
            runfiles = runfiles,
        ),
        # Exposes the config vitest actually ran with, for debugging and for
        # tests that pin the layering.
        OutputGroupInfo(vitest_config = depset([vitest_config])),
        # `srcs` is not a source attribute here: the same files reach the
        # collection through `compiled_tests`'s ts_compile, under its label.
        coverage_common.instrumented_files_info(
            ctx,
            dependency_attributes = ["compiled_tests", "deps"],
            extensions = _INSTRUMENTED_EXTENSIONS,
            baseline_coverage_files = [],
        ),
    ]

def _node_test_providers(
        ctx,
        runtime_files,
        test_files_list,
        node_modules_files,
        runtime_binary,
        runtime_args,
        symlinks):
    """Providers for a runner = "node:test" target: no generated config."""
    set_attrs = [
        attr_name
        for attr_name, value in [
            ("config", ctx.attr.config or ctx.attr.config_json),
            ("coverage", ctx.attr.coverage),
            ("coverage_provider", ctx.attr.coverage_provider),
            ("coverage_thresholds", ctx.attr.coverage_thresholds),
            ("environment", ctx.attr.environment),
            ("global_setup", ctx.attr.global_setup),
            ("globals", ctx.attr.globals),
            ("reporters", ctx.attr.reporters),
            ("setup_files", ctx.attr.setup_files),
            ("snapshots", ctx.attr.snapshots),
            ("update_snapshots", ctx.attr.update_snapshots),
            ("vitest", ctx.attr.vitest),
            ("wrangler_config", ctx.attr.wrangler_config),
        ]
        if value
    ]
    if set_attrs:
        message = ('ts_test {}: runner "{}" reads none of {}. Every one of ' +
                   "them configures vitest, which this target does not run. " +
                   "Drop them, or drop `runner` to run the test under vitest.")
        fail(message.format(ctx.label, RUNNER_NODE_TEST, ", ".join(set_attrs)))

    node_test_cfg = {
        "test_files_list": rlocation_path(ctx, test_files_list),
        "resolve_hook": rlocation_path(ctx, ctx.file._node_test_hook),
    }
    if node_modules_files:
        node_test_cfg["node_modules"] = rlocation_path(
            ctx,
            node_modules_files[0],
        )

    config = {
        "label": str(ctx.label),
        "mode": "node_test",
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "env": dict(ctx.attr.env),
        "node_test": node_test_cfg,
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)

    launcher = declare_launcher(
        ctx,
        config,
        basename = "{}_test_launcher".format(ctx.label.name),
    )

    runfiles_files = (
        [test_files_list, ctx.file._node_test_hook] + launcher.files +
        ctx.files.compiled_tests +
        ctx.files.srcs +
        node_modules_files +
        ctx.files.data
    )
    if runtime_binary:
        runfiles_files.append(runtime_binary)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        transitive_files = runtime_files,
        root_symlinks = launcher.root_symlinks,
        symlinks = symlinks,
    )
    for target in ctx.attr.data:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    return [
        DefaultInfo(
            executable = launcher.executable,
            runfiles = runfiles,
        ),
        coverage_common.instrumented_files_info(
            ctx,
            dependency_attributes = ["compiled_tests", "deps"],
            extensions = _INSTRUMENTED_EXTENSIONS,
            baseline_coverage_files = [],
        ),
    ]

# Every target under test answers for its own label under
# --instrumentation_filter, so each builds its own InstrumentedFilesInfo.
_INSTRUMENTED_EXTENSIONS = [
    "ts",
    "tsx",
    "mts",
    "cts",
    "js",
    "jsx",
    "mjs",
    "cjs",
]

def _instrumented_files_aspect_impl(_target, ctx):
    return coverage_common.instrumented_files_info(
        ctx,
        source_attributes = ["srcs"] if hasattr(ctx.rule.attr, "srcs") else [],
        dependency_attributes = (
            ["deps"] if hasattr(ctx.rule.attr, "deps") else []
        ),
        extensions = _INSTRUMENTED_EXTENSIONS,
        # The runner reports on the compiled .js; a baseline naming the .ts
        # would be a second name for the same code, with no lines at all.
        baseline_coverage_files = [],
    )

_instrumented_files_aspect = aspect(
    implementation = _instrumented_files_aspect_impl,
    attr_aspects = ["deps"],
    doc = "Internal: gives every target under test an InstrumentedFilesInfo " +
          "of its own.",
)

# Shared attribute dict for both the test and executable runner variants.
_RUNNER_ATTRS = {
    "compiled_tests": attr.label_list(
        aspects = [_instrumented_files_aspect],
        doc = "Label of the ts_compile target containing compiled test .js " +
              "files.",
        allow_files = [".js", ".jsx"],
    ),
    "deps": attr.label_list(
        aspects = [_instrumented_files_aspect],
        doc = "ts_compile and other targets whose .js files may be available " +
              "at test runtime; a dep's data srcs are in the runfiles beside " +
              "its .js, and a dep in the test's package has its TypeScript " +
              "srcs there too, at their source paths.",
    ),
    "node_modules": attr.label(
        doc = "A node_modules target providing the runtime npm dependency " +
              "tree.",
        allow_files = True,
    ),
    "tsconfig": attr.label(
        doc = "The tsconfig `compiled_tests` was built under. Its `paths` " +
              "resolve at run time to the compiled modules they named at " +
              "compile time (docs/rules/ts-test.md § A paths Alias).",
        allow_single_file = [".json"],
    ),
    "_tsaction": attr.label(
        default = Label("//ts/tools/tsaction"),
        executable = True,
        cfg = "exec",
    ),
    "_node_test_hook": attr.label(
        default = Label("//ts/private:node_test_hook.mjs"),
        allow_single_file = True,
    ),
    "_reads_hook": attr.label(
        default = Label("//ts/private:reads_hook.cjs"),
        allow_single_file = True,
    ),
    "runner": attr.string(
        doc = "Which test runner runs the compiled tests: \"vitest\" " +
              "(default) or \"node:test\", node's own runner, for tests " +
              "written against the node:test module.  A vitest attr set " +
              "under \"node:test\" is an analysis error rather than a " +
              "silently dropped setting.",
        default = RUNNER_VITEST,
        values = _RUNNERS,
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
    "srcs": attr.label_list(
        doc = "The TypeScript test sources `compiled_tests` was built from, " +
              "staged in the runfiles at their source paths; each compiled " +
              "test file maps back to the snapshot file its source implies.",
        allow_files = [".ts", ".tsx", ".mts", ".cts"],
    ),
    "snapshots": attr.label_list(
        doc = "Checked-in vitest snapshot files (__snapshots__/*.snap). " +
              "Listing them makes them readable inside the test sandbox, " +
              "which is what turns a stale snapshot into a failure.",
        allow_files = [".snap"],
    ),
    "globals": attr.bool(
        doc = "Enables vitest's global describe/it/expect (test.globals). " +
              "The matching `types` entry is the ts_test macro's half; " +
              "this attr is only the runtime one.",
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
    "update_snapshots": attr.bool(
        doc = "Internal: when True this runner writes snapshots (passes " +
              "--update). Used by the update_snapshots variant of ts_test.",
        default = False,
    ),
    "reads_report": attr.bool(
        doc = "Internal: when True this runner preloads the fs hook and " +
              "prints the workspace files the tests read outside the " +
              "runfiles. Used by the .reads variant of ts_test.",
        default = False,
    ),
}

_ts_test_runner_test = rule(
    implementation = _ts_test_runner_impl,
    test = True,
    attrs = dict(
        _RUNNER_ATTRS | WORKERS_POOL_ATTRS | LAUNCHER_ATTRS,
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
    toolchains = [
        config_common.toolchain_type(
            JS_RUNTIME_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal test runner rule; use ts_test macro instead.",
)

_ts_test_runner_binary = rule(
    implementation = _ts_test_runner_impl,
    executable = True,
    attrs = _RUNNER_ATTRS | WORKERS_POOL_ATTRS | LAUNCHER_ATTRS,
    toolchains = [
        config_common.toolchain_type(
            JS_RUNTIME_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal executable runner: a test's `.update_snapshots` and " +
          "`.reads` companions, and a ts_test(update_snapshots = True).",
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

def ts_test(
        name,
        srcs,
        deps = [],
        node_modules = None,
        npm_workspace_name = "npm",
        vitest = None,
        runtime = None,
        env = {},
        args = [],
        size = "medium",
        timeout = None,
        tags = [],
        tsconfig = None,
        visibility = None,
        runner = RUNNER_VITEST,
        environment = "",
        coverage = False,
        config = None,
        config_srcs = [],
        wrangler_config = None,
        setup_files = [],
        global_setup = [],
        data = [],
        globals = False,
        reporters = [],
        coverage_thresholds = {},
        coverage_provider = "",
        snapshots = [],
        update_snapshots = False):
    """Compiles TypeScript test files and runs them under vitest or node:test.

    Internally creates a ts_compile target for the test sources, then a
    test runner rule that runs the compiled .js outputs under `runner`.

    Args:
        name:              Name of the test target.
        srcs:              TypeScript test source files (.ts, .tsx).
        deps:              ts_compile or ts_npm_package targets the tests
                           import. ts_test builds a node_modules tree from
                           every dep that provides NpmPackageInfo, their
                           transitive npm deps, and the npm closure each other
                           dep carries in TsDeclarationInfo, so a ts_compile
                           dep's own npm packages are at hand at runtime
                           without being listed here.  A package the test
                           files themselves import is a direct dep, as in any
                           ts_compile.
        node_modules:      Optional: explicit node_modules target for runtime
                           npm resolution. When set, the auto-generation of an
                           internal node_modules target is skipped entirely.
        npm_workspace_name: Name of the npm workspace used by
                           npm_translate_lock. Defaults to "npm" (the
                           conventional name for the @npm repository).  Set
                           this if your WORKSPACE uses a non-default name,
                           e.g. npm_workspace_name = "my_npm". This param is
                           informational only; it does not affect rule
                           generation when node_modules = None, because auto
                           node_modules construction uses NpmPackageInfo
                           provider detection rather than label string
                           matching.
        vitest:            Explicit label for the vitest binary (optional).
        runtime:           Per-target JS runtime binary override (optional).
                           Takes priority over the js_runtime toolchain.
        env:               Extra environment variables for the test runner.
        args:              The runner's command-line flags: node's under
                           "node:test" (`--experimental-test-module-mocks`
                           for `mock.module`), vitest's under "vitest";
                           `bazel test --test_arg` appends to them.
        size:              Bazel test size (default "medium").
        timeout:           Bazel test timeout.
        tags:              Bazel tags. `manual` also reaches the targets this
                           macro generates, which no BUILD file names and a
                           wildcard would otherwise analyse; every other tag
                           goes to the test rule alone.
        visibility:        Bazel visibility for the test target, and for the
                           ts_compile targets this macro generates from `srcs`,
                           `setup_files` and `global_setup`. Those default to
                           `//visibility:public` when the test declares none,
                           so that an IDE tsconfig can name them.
        runner:            Which runner runs the compiled tests: "vitest"
                           (default) or "node:test". "node:test" is for tests
                           written against node's own runner -- vitest's
                           collector never sees a `test()` registered with
                           node:test, so such a file collects zero tests under
                           the default. `tsconfig` applies on either runner.
                           node:test configures itself
                           from CLI flags and the test file, so every
                           vitest-shaped attr (`config`, `environment`,
                           `globals`, `reporters`, `setup_files`,
                           `global_setup`, `snapshots`, the coverage trio and
                           `vitest`) is an analysis error under it, and
                           `bazel coverage` is unsupported. `--test_filter`
                           reaches it as node's --test-name-pattern; sharding
                           works as it does for vitest.
        environment:       Vitest test environment: 'node', 'happy-dom', or
                           'jsdom'. Requires the corresponding package in
                           node_modules.
        coverage:          When True, also enables coverage during `bazel test`
                           (not just `bazel coverage`).  Coverage during
                           `bazel coverage` is always on regardless of this
                           attr — every vitest ts_test supports `bazel coverage`
                           without any opt-in.
        tsconfig:          Forwarded to every ts_compile this macro generates --
                           the one over `srcs` and the ones over `setup_files`
                           and `global_setup`: the package's own tsconfig.json,
                           or a ts_config target when that file extends others.
                           Every compiler option the tests check under is its:
                           the `lib` a worker test needs, a `types` entry naming
                           a pool's ambient module (`cloudflare:test`) or
                           `vitest/globals`, the `paths` the test files import
                           through, which the vitest runner resolves at run
                           time to the compiled modules they named.
        config:            Vitest config, either a label pointing at a config
                           file (.ts/.mts/.js/.mjs) or an inline dict.  It is
                           MERGED into the config rules_typescript generates
                           rather than replacing it -- see "Vitest config
                           generation" in this file for the layering.  A
                           config file that default-exports an array is read
                           as a list of vitest projects (test.projects), and
                           each project in it receives the Bazel layer and the
                           attribute layer too.
        config_srcs:       The modules `config` imports relatively, and theirs,
                           staged with the config's copy at their paths relative
                           to the config's package.
        wrangler_config:   The wrangler config a Workers-pool `config` names
                           through `wrangler.configPath`. A copy whose `main`
                           (and every `env.<name>.main`) names the compiled
                           entry beside it is staged at this file's own runfiles
                           path; the file is not also listed in `data`.
        setup_files:       Files run before every test file (test.setupFiles),
                           in the order listed -- compiled .ts/.tsx entries
                           first, then plain .js/.mjs ones.  TypeScript
                           entries are compiled with the same deps as the
                           tests.  All of them run after any setupFiles the
                           `config` attr contributes.
        global_setup:      Files run once around the whole test run
                           (test.globalSetup); compiled like setup_files.
        data:              Extra runfiles: fixtures the tests read, and files
                           that `setup_files` entries import.
        globals:           Enables vitest's global describe/it/expect
                           (test.globals). The type program sees them through a
                           `types` entry naming "vitest/globals" in `tsconfig`,
                           with vitest among `deps`.
        reporters:         Vitest reporters (test.reporters).
        coverage_thresholds: Coverage thresholds (test.coverage.thresholds),
                           e.g. {"lines": "80"}.  Enforced only when coverage
                           runs (`bazel coverage`, or `bazel test` with
                           coverage = True).
        coverage_provider: Vitest coverage provider, "v8" (vitest's default) or
                           "istanbul".  A pool that runs the tests in a second
                           runtime needs "istanbul": v8 coverage is read out of
                           node's inspector, which that runtime does not have.
                           The matching @vitest/coverage-* package must be in
                           deps.
        snapshots:         Checked-in vitest snapshot files
                           (`glob(["__snapshots__/*.snap"])`). Listing them is
                           what makes them readable inside the test sandbox,
                           and so what turns a stale one into a failure; a
                           snapshot the test needs and cannot read fails too.
                           Write them with the generated updater:

                               bazel run //path:my_test.update_snapshots

        update_snapshots:  Makes THIS target the executable updater instead of
                           a test. Every vitest ts_test already declares
                           `<name>.update_snapshots`, so this is only for an
                           updater that has to stand on its own — and it
                           compiles `srcs` itself, so it cannot share a package
                           with a ts_test over the same files.

    Example:
        ts_test(
            name = "button_test",
            srcs = ["Button.test.tsx"],
            deps = [":button", "@npm//:react", "@npm//:vitest"],
        )

    DOM testing example:
        ts_test(
            name = "component_test",
            srcs = ["Button.test.tsx"],
            deps = [
                ":button",
                "@npm//:react",
                "@npm//:@testing-library/react",
                "@npm//:vitest",
            ],
            environment = "happy-dom",
        )

    Custom npm workspace example:
        ts_test(
            name = "schema_test",
            srcs = ["schema.test.ts"],
            deps = [":schema", "@my_npm//:zod", "@my_npm//:vitest"],
            npm_workspace_name = "my_npm",
        )
    """

    # An IDE tsconfig has to be able to name the test's compile, and a private
    # visibility is one no BUILD file can widen: the test's own, public without.
    compile_name = "_{}_compile".format(name)
    compile_visibility = visibility if visibility else ["//visibility:public"]

    # `manual` reaches every generated target: a wildcard that skips the test
    # must not analyse its compiles; //tests/node_test/analysis fails by design.
    wildcard_tags = ["manual"] if "manual" in tags else []

    ts_compile(
        name = compile_name,
        srcs = srcs,
        deps = deps,
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    # A select() in deps cannot be iterated at macro time: no tree is generated
    # and the caller names node_modules.
    if node_modules == None:
        if type(deps) != "list":
            pass
        elif deps:
            nm_name = "_{}_node_modules".format(name)
            _ts_auto_node_modules(
                name = nm_name,
                deps = deps,
                visibility = ["//visibility:private"],
                tags = wildcard_tags,
            )
            node_modules = ":{}".format(nm_name)

    # vitest loads setupFiles and globalSetup through the tests' module graph,
    # so they are JavaScript by the time the runner starts.
    setup_labels = _compile_setup_sources(
        name = "_{}_setup".format(name),
        sources = setup_files,
        deps = deps,
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )
    global_setup_labels = _compile_setup_sources(
        name = "_{}_global_setup".format(name),
        sources = global_setup,
        deps = deps,
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    runner_kwargs = {
        "name": name,
        "runner": runner,
        "compiled_tests": [":{}".format(compile_name)],
        "srcs": srcs,
        "snapshots": snapshots,
        "deps": deps,
        "env": env,
        "args": args,
        "environment": environment,
        "coverage": coverage,
        "coverage_thresholds": coverage_thresholds,
        "coverage_provider": coverage_provider,
        "config_srcs": config_srcs,
        "data": data,
        "global_setup": global_setup_labels,
        "globals": globals,
        "reporters": reporters,
        "setup_files": setup_labels,
        "update_snapshots": update_snapshots,
    }
    if node_modules:
        runner_kwargs["node_modules"] = node_modules
    if tsconfig:
        runner_kwargs["tsconfig"] = tsconfig
    if vitest:
        runner_kwargs["vitest"] = vitest
    if runtime:
        runner_kwargs["runtime"] = runtime
    if visibility:
        runner_kwargs["visibility"] = visibility
    if type(config) == "dict":
        runner_kwargs["config_json"] = json.encode(config)
    elif config:
        runner_kwargs["config"] = config
    if wrangler_config:
        runner_kwargs["wrangler_config"] = wrangler_config

    if update_snapshots:
        # Produce an executable target (not a test) so `bazel run` works.
        # size/timeout are test-only attrs; omit them for the executable rule.
        _ts_test_runner_binary(**(runner_kwargs | {"tags": wildcard_tags}))
        return

    # The companions share the test's compile (a second ts_compile over the
    # same srcs declares the same .js outputs); node:test has neither.
    if runner == RUNNER_VITEST:
        _ts_test_runner_binary(
            **(runner_kwargs | {
                "name": "{}.update_snapshots".format(name),
                "update_snapshots": True,
                "tags": wildcard_tags,
            })
        )
        _ts_test_runner_binary(
            **(runner_kwargs | {
                "name": "{}.reads".format(name),
                "reads_report": True,
                "tags": wildcard_tags,
            })
        )

    runner_kwargs["size"] = size
    runner_kwargs["tags"] = tags
    if timeout:
        runner_kwargs["timeout"] = timeout
    _ts_test_runner_test(**runner_kwargs)
