"""The tree reaches the compile as an input, and the check without one.

Expanding the tree in the strict-deps manifest is what used to kill the action,
and the reason is structural: the check reads only the target's own sources, so
a dep's directory is one it holds no input for. Pinning both halves keeps the
next person from "fixing" the expansion by staging the tree there.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "TsDeclarationInfo")

def _tree_reaches_compile_impl(ctx):
    env = analysistest.begin(ctx)

    trees = [
        f
        for f in ctx.attr.tree[TsDeclarationInfo].declaration_files.to_list()
        if f.is_directory
    ]
    asserts.equals(env, 1, len(trees), "the codegen target provides no directory")

    staged = []
    for action in analysistest.target_actions(env):
        for f in action.inputs.to_list():
            if f in trees:
                staged.append(action.mnemonic)

    asserts.true(
        env,
        "TsgoDeclare" in staged,
        "the tree is no input to the type-check, so nothing it declares is in the program",
    )
    asserts.false(
        env,
        "TsStrictDeps" in staged,
        "the check reads the tree, which is the only thing expanding it would need",
    )

    return analysistest.end(env)

tree_reaches_compile_test = analysistest.make(
    _tree_reaches_compile_impl,
    attrs = {
        "tree": attr.label(mandatory = True, doc = "The ts_codegen target under test."),
    },
)
