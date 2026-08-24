"""Analysis-time coverage for the one multi-version case a tree cannot express.

Two versions of one name in a closure is ordinary, and the tree places both. Two
versions of one name declared side by side on the SAME node_modules target is
not: `node_modules/<name>` is one directory and Node resolves the bare name to
it, so there is no arrangement that answers `import "<name>"` with both. Picking
one and carrying on is the failure mode this whole layout exists to remove, so it
has to be an error instead.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

conflicting_direct_versions_test = _fails_with("depends on two versions of 'minimatch' at once")
