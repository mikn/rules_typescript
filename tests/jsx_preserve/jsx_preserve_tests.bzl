"""What the rule declares for a .tsx under jsx: preserve, before any action,
and what the store of a member under it writes."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//tests/npm:store_tests.bzl", "staged_inputs")

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
        ["view.jsx", "view.jsx.map"],
        got,
        "the default outputs",
    )
    asserts.equals(
        env,
        ["view.d.ts"],
        [
            f.path[f.path.find(_PKG) + len(_PKG):]
            for f in target[OutputGroupInfo].declarations.to_list()
        ],
        "the declarations output group",
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

def _member_view_impl(ctx):
    env = analysistest.begin(ctx)
    staged = [
        a
        for a in analysistest.target_actions(env)
        if a.mnemonic == "NpmStore"
    ]
    asserts.equals(env, 1, len(staged), "the store stages the member once")
    root = "/tests/jsx_preserve/member/"
    linked = sorted([
        f.path[f.path.find(root) + len(root):] if root in f.path else f.basename
        for f in staged_inputs(staged)
    ])
    asserts.equals(
        env,
        [
            "jsx-runtime.d.ts",
            "jsx-runtime.js",
            "jsx-runtime.js.map",
            "member.package.json",
            "view.d.ts",
            "view.jsx",
            "view.jsx.map",
        ],
        linked,
        "the tree holds the .jsx at the path the manifest as built names",
    )
    return analysistest.end(env)

member_view_test = analysistest.make(_member_view_impl)
