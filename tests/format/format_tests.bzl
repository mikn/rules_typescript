"""Formatting must validate checked-in inputs without touching generated outputs."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _format_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    actions = [action for action in analysistest.target_actions(env) if action.mnemonic == "TsFormat"]
    asserts.equals(env, 1 if ctx.attr.enabled else 0, len(actions))
    if actions:
        action = actions[0]
        inputs = [file.short_path for file in action.inputs.to_list()]
        for source in ["source.ts", "data.json", "config.json"]:
            asserts.true(env, "tests/format/" + source in inputs)
        asserts.false(env, "tests/format/generated.ts" in inputs)
        asserts.true(env, "-verify-copies" in action.argv)
        for output in action.outputs.to_list():
            asserts.true(env, output in target[OutputGroupInfo]._validation.to_list())
    return analysistest.end(env)

_configured_test = analysistest.make(
    _format_impl,
    attrs = {"enabled": attr.bool(default = True)},
    config_settings = {str(Label("//ts:format")): str(Label("//tests/format:configured"))},
)
_disabled_test = analysistest.make(
    _format_impl,
    attrs = {"enabled": attr.bool(default = False)},
    config_settings = {str(Label("//ts:format")): str(Label("//ts:no_format"))},
)

def format_test_suite(name):
    _configured_test(name = "generated_outputs_are_not_reformatted", target_under_test = ":program")
    _disabled_test(name = "disabled_formatter_adds_no_validation", target_under_test = ":program")
    native.test_suite(name = name, tests = [":generated_outputs_are_not_reformatted", ":disabled_formatter_adds_no_validation"])
