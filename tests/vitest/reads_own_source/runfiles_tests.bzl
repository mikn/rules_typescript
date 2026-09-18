"""Analysis-time proof of which sources a ts_test's runfiles hold."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_HELD = [
    "tests/vitest/reads_own_source/test/reads.test.ts",
    "tests/vitest/reads_own_source/src/index.ts",
    "tests/vitest/reads_own_source/src/index.js",
    "tests/vitest/math.js",
]

def _package_sources_impl(ctx):
    env = analysistest.begin(ctx)
    runfiles = analysistest.target_under_test(env)[DefaultInfo].default_runfiles
    files = [f.short_path for f in runfiles.files.to_list()]
    own = sorted([p for p in files if not p.startswith("../")])
    for path in _HELD:
        asserts.true(
            env,
            path in files,
            path + " is in the runfiles: " + str(own),
        )
    asserts.false(
        env,
        "tests/vitest/math.ts" in files,
        "another package's source is out of the runfiles: " + str(own),
    )
    return analysistest.end(env)

package_sources_runfiles_test = analysistest.make(_package_sources_impl)
