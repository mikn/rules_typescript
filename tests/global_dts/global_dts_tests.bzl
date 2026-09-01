"""Reaches the classification a compile cannot show.

A module-scoped .d.ts wrongly called global would put a module in the consumer's
program and change nothing observable there, so the entry file itself is the
only place that half of the answer is visible.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "TsDeclarationInfo")

def _global_entry_files_impl(ctx):
    return [DefaultInfo(files = ctx.attr.target[TsDeclarationInfo].global_entry_files)]

global_entry_files = rule(
    implementation = _global_entry_files_impl,
    attrs = {
        "target": attr.label(mandatory = True, providers = [TsDeclarationInfo]),
    },
    doc = "The global-declaration entry `target` provides, as a file a test can read.",
)

def _unknown_private_global_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.expect_failure(env, "private_globals on")
    asserts.expect_failure(env, "which is not in srcs")
    asserts.expect_failure(env, "Did you mean 'tests/global_dts/shim.d.ts'?")
    return analysistest.end(env)

unknown_private_global_test = analysistest.make(
    _unknown_private_global_impl,
    expect_failure = True,
)
