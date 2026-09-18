"""A target resolves its importer's resolution of a name. app-a links
ansi-regex@5.0.1; a target under it naming the root's other major fails
analysis naming the importer's link, the label to write instead."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _importer_resolution_wins_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.expect_failure(env, "links ansi-regex@5.0.1")
    asserts.expect_failure(env, "@npm_features//tests/npm/app_a:ansi-regex")
    return analysistest.end(env)

importer_resolution_wins_test = analysistest.make(
    _importer_resolution_wins_impl,
    expect_failure = True,
)
