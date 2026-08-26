"""What --instrumentation_filter selects, read off the test target's provider.

The report itself is only reachable from a `bazel coverage` run, so what a
checked-in test can pin is the selection Bazel hands the runner: the same flag,
two values, one dep on either side of it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _instrumented_files(env):
    info = analysistest.target_under_test(env)[InstrumentedFilesInfo]
    return sorted([f.short_path for f in info.instrumented_files.to_list()])

def _selects_both_packages_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.equals(
        env,
        [
            "tests/vitest/coverage/math.test.ts",
            "tests/vitest/coverage/same_package.ts",
            "tests/vitest/math.ts",
        ],
        _instrumented_files(env),
    )
    return analysistest.end(env)

def _selects_one_package_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.equals(
        env,
        [
            "tests/vitest/coverage/math.test.ts",
            "tests/vitest/coverage/same_package.ts",
        ],
        _instrumented_files(env),
    )
    return analysistest.end(env)

def _selects_nothing_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.equals(env, [], _instrumented_files(env))
    return analysistest.end(env)

_COVERAGE_ON = {"//command_line_option:collect_code_coverage": True}

selects_both_packages_test = analysistest.make(
    _selects_both_packages_impl,
    config_settings = _COVERAGE_ON | {
        "//command_line_option:instrumentation_filter": "^//tests/vitest[/:]",
    },
)

selects_one_package_test = analysistest.make(
    _selects_one_package_impl,
    config_settings = _COVERAGE_ON | {
        "//command_line_option:instrumentation_filter": "^//tests/vitest/coverage[/:]",
    },
)

selects_nothing_test = analysistest.make(
    _selects_nothing_impl,
    config_settings = _COVERAGE_ON | {
        "//command_line_option:instrumentation_filter": "-tests/vitest",
    },
)
