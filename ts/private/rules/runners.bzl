"""The two runners ts_test ships, one target each under //ts/runners.

A runner is a target providing TsTestRunnerInfo, the way a toolchain is:
ts_test compiles the tests and builds the forest, and the runner's `launch`
turns them into the launcher config and the runfiles of one test.
docs/rules/ts-test.md § Runners.
"""

load("//tools/launcher:launcher.bzl", "rlocation_path")
load("//ts/private:providers.bzl", "TsTestRunnerInfo")
load(
    "//ts/private/actions:vitest.bzl",
    "tsconfig_paths_action",
    "vitest_config_action",
)
load("//ts/private/actions:workers_pool.bzl", "workers_pool_environment")

def _vitest_launch(ctx, test):
    pool = workers_pool_environment(
        ctx,
        test.node_modules_files,
        test.runtime_data_sets,
    )
    tsconfig_paths = tsconfig_paths_action(ctx)
    written = vitest_config_action(
        ctx,
        test_entry_points = test.entry_points,
        node_modules_files = test.node_modules_files,
        pool_layer = pool.layer,
        tsconfig_paths = tsconfig_paths,
        inline_members = test.inline_members,
    )

    # Every path is a runfiles path; the launcher resolves them through the
    # runfiles library, so manifest-only layouts work like symlink trees.
    section = {
        "config_file": rlocation_path(ctx, written.config),
        "test_files_list": rlocation_path(ctx, test.test_files_list),
        "reads_hook": rlocation_path(ctx, test.runner.hook),
    }
    if test.node_modules_files:
        section["node_modules"] = rlocation_path(
            ctx,
            test.node_modules_files[0],
        )

        # The canonical bin entry from vitest's package.json#bin, reached inside
        # the node_modules tree artifact.
        section["vitest_in_tree"] = "vitest/vitest.mjs"

    # A sandboxed test must fail on a stale or missing .snap, never write one,
    # and vitest only stops writing when it believes it is running in CI.
    env = dict(ctx.attr.env)
    env.setdefault("CI", "true")

    files = (
        [written.config, test.runner.hook] + pool.files +
        written.user_config_files
    )
    if tsconfig_paths:
        files.append(tsconfig_paths)
    return struct(
        mode = "vitest",
        section = section,
        env = env,
        files = files,
        symlinks = pool.symlinks,
        transitive_files = depset(transitive = (
            [test.transitive_js] + pool.runtime_data_sets +
            test.package_sources
        )),
        # The config vitest ran with, for debugging and for the tests that pin
        # the layering.
        output_groups = {"vitest_config": depset([written.config])},
    )

# node:test is configured by CLI flags and the test file alone, so the runner
# refuses the vitest attributes instead of generating a config nothing reads.
def _node_test_launch(ctx, test):
    set_attrs = [
        name
        for name, value in [
            ("config", ctx.attr.config),
            ("config_srcs", ctx.attr.config_srcs),
            ("coverage_provider", ctx.attr.coverage_provider),
            ("wrangler_config", ctx.attr.wrangler_config),
        ]
        if value
    ]
    if set_attrs:
        fail(("ts_test {}: the node:test runner reads none of {}. Every one " +
              "of them configures vitest, which this target does not run. " +
              "Drop them, or drop `runner` to run the test under " +
              "vitest.").format(ctx.label, ", ".join(set_attrs)))

    section = {
        "test_files_list": rlocation_path(ctx, test.test_files_list),
        "resolve_hook": rlocation_path(ctx, test.runner.hook),
    }
    if test.node_modules_files:
        section["node_modules"] = rlocation_path(
            ctx,
            test.node_modules_files[0],
        )
    return struct(
        mode = "node_test",
        section = section,
        env = dict(ctx.attr.env),
        files = [test.runner.hook],
        symlinks = {},
        transitive_files = depset(transitive = (
            [test.transitive_js] + test.runtime_data_sets +
            test.package_sources
        )),
        output_groups = {},
    )

def _vitest_runner_impl(ctx):
    return [TsTestRunnerInfo(
        packages = ["vitest"],
        hook = ctx.file._reads_hook,
        launch = _vitest_launch,
    )]

vitest_runner = rule(
    implementation = _vitest_runner_impl,
    attrs = {
        "_reads_hook": attr.label(
            default = Label("//ts/private:reads_hook.cjs"),
            allow_single_file = True,
        ),
    },
    doc = "The vitest runner: the generated config over the user's, vitest " +
          "from the test's node_modules tree, and under `bazel run <test> " +
          "-- --reads` the report of the workspace files the tests read.",
)

def _node_test_runner_impl(ctx):
    return [TsTestRunnerInfo(
        packages = [],
        hook = ctx.file._resolve_hook,
        launch = _node_test_launch,
    )]

node_test_runner = rule(
    implementation = _node_test_runner_impl,
    attrs = {
        "_resolve_hook": attr.label(
            default = Label("//ts/private:node_test_hook.mjs"),
            allow_single_file = True,
        ),
    },
    doc = "node's own runner: `node --test` over the compiled files, with " +
          "the resolve hook that answers a relative `.ts` specifier with " +
          "the compiled sibling.",
)
