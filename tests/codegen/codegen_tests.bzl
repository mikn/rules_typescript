"""What an `outs` ts_codegen provides: its declarations and JavaScript, as a dep."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

def _outs_codegen_providers_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    asserts.true(
        env,
        TsInfo in target,
        "an outs ts_codegen provides TsInfo, so it can be a dep",
    )
    if TsInfo not in target:
        return analysistest.end(env)
    info = target[TsInfo]

    # A `.d.ts` out is a declaration a consumer's tsconfig `types` names; a
    # `.ts` out is a source for a consumer's srcs and travels in neither.
    asserts.equals(
        env,
        ["generated-globals.d.ts"],
        [f.basename for f in info.declarations.to_list()],
        "the declaration outs",
    )
    asserts.equals(
        env,
        [f.basename for f in info.declarations.to_list()],
        [f.basename for f in info.transitive_declarations.to_list()],
        "a codegen has no deps, so its closure is its own outs",
    )
    asserts.equals(env, [], info.js.to_list(), "no JavaScript out")
    asserts.equals(env, [], info.npm_packages.to_list(), "no npm closure")
    return analysistest.end(env)

outs_codegen_providers_test = analysistest.make(_outs_codegen_providers_impl)
