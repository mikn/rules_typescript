"""Analysis-time guards on runner = "node:test"."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(*messages):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        for message in messages:
            asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

vitest_attr_test = _fails_with(
    'runner "node:test" reads none of environment, globals',
    "configures vitest, which this target does not run",
)
css_module_test = _fails_with('runner "node:test" cannot load a CSS module')
