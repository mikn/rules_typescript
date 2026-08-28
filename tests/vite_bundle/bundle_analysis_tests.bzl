"""Analysis-time coverage for the ts_bundle attrs that only mean anything in app mode.

A lib bundle declares its output files by name, so there is neither a directory
to copy static files into nor a hashed filename to map. Both guards fail at
analysis time rather than producing a bundle that silently dropped the attr, and
each target under test is tagged manual so `bazel build //...` does not stop on
the failure being asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

public_dir_needs_app_mode_test = _fails_with("'public_dir' requires mode")
manifest_needs_app_mode_test = _fails_with("'manifest' requires mode")
