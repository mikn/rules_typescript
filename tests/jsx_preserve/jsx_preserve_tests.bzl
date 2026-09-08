"""What the rule declares for a .tsx under jsx: preserve, before any action."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_PKG = "tests/jsx_preserve/"

def _declared_outputs_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    got = sorted([
        f.path[f.path.find(_PKG) + len(_PKG):]
        for f in target[DefaultInfo].files.to_list()
    ])
    asserts.equals(
        env,
        ["view.d.ts", "view.jsx", "view.jsx.map"],
        got,
        "declared outputs",
    )

    config = [
        a
        for a in analysistest.target_actions(env)
        if a.mnemonic == "TsConfig"
    ]
    asserts.equals(env, 1, len(config), "TsConfig actions")
    if len(config) == 1:
        asserts.true(
            env,
            "-jsx=preserve" in config[0].argv,
            "the TsConfig step is told the declaration",
        )
    return analysistest.end(env)

declared_outputs_test = analysistest.make(_declared_outputs_impl)
