load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

def _source_actions_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    info = target[TsInfo]
    asserts.equals(env, [], info.js.to_list())
    asserts.equals(env, [], info.declarations.to_list())
    asserts.equals(env, ["library.ts"], [f.basename for f in info.runtime_sources.to_list()])
    mnemonics = [action.mnemonic for action in analysistest.target_actions(env)]
    asserts.true(env, "TsgoCheck" in mnemonics)
    asserts.false(env, "TsEmit" in mnemonics)
    asserts.false(env, "TsgoDeclare" in mnemonics)
    asserts.true(env, len(target[OutputGroupInfo]._validation.to_list()) > 0)
    return analysistest.end(env)

source_actions_test = analysistest.make(_source_actions_impl)

def _node_rejects_source_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.expect_failure(env, "requires emitted JavaScript or declarations from")
    asserts.expect_failure(env, "//tests/source_mode/lib:library")
    asserts.expect_failure(env, "Set emit = True")
    return analysistest.end(env)

node_rejects_source_test = analysistest.make(_node_rejects_source_impl, expect_failure = True)
