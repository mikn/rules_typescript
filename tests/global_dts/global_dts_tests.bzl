"""Reaches the classification a compile cannot show.

A module-scoped .d.ts wrongly called global would put a module in the consumer's
program and change nothing observable there, so the entry file itself is the
only place that half of the answer is visible. The same goes the other way for
the default: a global nobody exported leaves no artifact behind at all, and the
absence of an entry is what says so.
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

def _no_global_entry_impl(ctx):
    env = analysistest.begin(ctx)
    entries = analysistest.target_under_test(env)[TsDeclarationInfo].global_entry_files.to_list()
    asserts.equals(
        env,
        [],
        [f.basename for f in entries],
        "a target that exports no globals provides no entry, so a consumer has nothing to list",
    )
    return analysistest.end(env)

no_global_entry_test = analysistest.make(_no_global_entry_impl)

def _unknown_public_global_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.expect_failure(env, "public_globals on")
    asserts.expect_failure(env, "which is not in srcs")
    asserts.expect_failure(env, "Did you mean 'tests/global_dts/shim.d.ts'?")
    return analysistest.end(env)

unknown_public_global_test = analysistest.make(
    _unknown_public_global_impl,
    expect_failure = True,
)
