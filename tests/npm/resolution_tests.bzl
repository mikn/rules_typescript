"""Analysis-time coverage for the resolutions a tree cannot express side by side.

Two resolutions of one name in a closure is ordinary, and the tree places both.
Two of them declared side by side on the SAME node_modules target is not:
`node_modules/<name>` is one directory and Node resolves the bare name to it, so
there is no arrangement that answers `import "<name>"` with both. Picking one and
carrying on is the failure mode this whole layout exists to remove, so it has to
be an error instead.

Two peer resolutions of one version are the same conflict and not a softer one.
The tarball is shared, so the directory's own files would be right either way --
but the resolutions disagree about what the package's own dependencies resolve
to, and that disagreement lands in the single `node_modules` inside that one
directory.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

conflicting_direct_versions_test = _fails_with("depends on two versions of 'minimatch' at once")
conflicting_direct_peers_test = _fails_with("depends on two resolutions of 'ansi-styles@6.2.3' at once")
