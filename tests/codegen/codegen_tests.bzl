"""What an `outs` ts_codegen provides: its declarations and JavaScript, as a dep."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "JsInfo", "TsDeclarationInfo")

def _outs_codegen_providers_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    asserts.true(env, TsDeclarationInfo in target, "an outs ts_codegen provides TsDeclarationInfo, so it can be a dep")
    asserts.true(env, JsInfo in target, "an outs ts_codegen provides JsInfo, so it can be a dep")
    if TsDeclarationInfo not in target or JsInfo not in target:
        return analysistest.end(env)

    # A `.d.ts` out is a declaration a consumer's tsconfig `types` names; a
    # `.ts` out is a source for a consumer's srcs and travels in neither.
    asserts.equals(
        env,
        ["generated-globals.d.ts"],
        [f.basename for f in target[TsDeclarationInfo].declaration_files.to_list()],
        "the declaration outs",
    )
    asserts.equals(
        env,
        [f.basename for f in target[TsDeclarationInfo].declaration_files.to_list()],
        [f.basename for f in target[TsDeclarationInfo].transitive_declaration_files.to_list()],
        "a codegen has no deps, so its closure is its own outs",
    )
    asserts.equals(env, [], target[JsInfo].js_files.to_list(), "no JavaScript out")
    asserts.equals(env, [], target[TsDeclarationInfo].transitive_npm_packages.to_list(), "no npm closure")
    return analysistest.end(env)

outs_codegen_providers_test = analysistest.make(_outs_codegen_providers_impl)
