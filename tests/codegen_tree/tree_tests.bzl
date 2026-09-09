"""The tree is a tsgo input, and one line of the ownership manifest.

A directory has no file list at analysis time, so the manifest names it as its
own path and the check owns every file under it by that entry.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

def _tree_reaches_compile_impl(ctx):
    env = analysistest.begin(ctx)

    trees = [
        f
        for f in ctx.attr.tree[TsInfo].declarations.to_list()
        if f.is_directory
    ]
    asserts.equals(env, 1, len(trees), "the codegen target provides no tree")
    tsgo = [
        a
        for a in analysistest.target_actions(env)
        if a.mnemonic in ("TsgoDeclare", "TsgoCheck")
    ]
    asserts.equals(env, 1, len(tsgo), "one tsgo action")
    if len(trees) != 1 or len(tsgo) != 1:
        return analysistest.end(env)
    tree = trees[0]
    asserts.true(
        env,
        tree in tsgo[0].inputs.to_list(),
        "the tree is no tsgo input: nothing it declares is in the program",
    )

    name = analysistest.target_under_test(env).label.name
    manifest_name = name + ".ownership"
    writers = [
        a
        for a in analysistest.target_actions(env)
        if manifest_name in [f.basename for f in a.outputs.to_list()]
    ]
    asserts.equals(env, 1, len(writers), "one action writes the manifest")
    if len(writers) == 1 and writers[0].content != None:
        lines = writers[0].content.split("\n")
        owning = [
            l
            for l in lines
            if l.startswith("file\t") and l.endswith("\t" + tree.path)
        ]
        asserts.equals(
            env,
            1,
            len(owning),
            "the manifest names the tree once, as its path:\n" +
            "\n".join(lines),
        )
    return analysistest.end(env)

tree_reaches_compile_test = analysistest.make(
    _tree_reaches_compile_impl,
    attrs = {
        "tree": attr.label(
            mandatory = True,
            doc = "The ts_codegen under test.",
        ),
    },
)
