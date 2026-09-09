"""Analysis-time guards on the node:test runner."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(*messages):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        for message in messages:
            asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

vitest_attr_test = _fails_with(
    "the node:test runner reads none of config, coverage_provider, " +
    "wrangler_config.",
    "configures vitest, which this target does not run",
)
