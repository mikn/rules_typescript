"""A ts_test's runtime node_modules is the forest its tsgo action resolved
against: one tree, built once, the ts_compile dep's closure included."""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")

_ActionsInfo = provider(
    "The actions a target registered.",
    fields = ["actions"],
)

def _actions_aspect_impl(target, _ctx):
    return [_ActionsInfo(actions = target.actions)]

_actions_aspect = aspect(implementation = _actions_aspect_impl)

def _only_output(action, suffix):
    outputs = action.outputs.to_list()
    if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
        return outputs[0]
    return None

def _runtime_tree_is_the_forest_impl(ctx):
    env = unittest.begin(ctx)
    actions = ctx.attr.test[_ActionsInfo].actions
    manifests = [a for a in actions if _only_output(a, "__manifest.txt")]
    trees = [
        f
        for a in actions
        if a.mnemonic == "NodeModulesTree"
        for f in a.outputs.to_list()
    ]
    tsgo = [a for a in actions if a.mnemonic in ("TsgoDeclare", "TsgoCheck")]
    launchers = [a for a in actions if _only_output(a, "_test_launcher.json")]
    asserts.equals(env, 1, len(manifests), "one node_modules manifest")
    asserts.equals(env, 1, len(trees), "one node_modules tree")
    asserts.equals(env, 1, len(tsgo), "one tsgo action")
    asserts.equals(env, 1, len(launchers), "one launcher config")
    if [len(manifests), len(trees), len(tsgo), len(launchers)] == [1, 1, 1, 1]:
        asserts.true(
            env,
            len([
                line
                for line in manifests[0].content.splitlines()
                if line.split("\t")[0] == "C" and
                   line.endswith("zod/package.json")
            ]) > 0,
            "zod, declared by the ts_compile dep alone, is in the tree",
        )
        asserts.true(
            env,
            "-node_modules=" + trees[0].path in tsgo[0].argv,
            "tsgo resolves against the tree: " + str(tsgo[0].argv),
        )
        linked = '"node_modules": "{}/{}"'.format(
            ctx.workspace_name,
            trees[0].short_path,
        )
        asserts.true(
            env,
            linked in launchers[0].content,
            "the launcher runs the tests in the same tree: " +
            launchers[0].content,
        )
    return unittest.end(env)

runtime_tree_is_the_forest_test = unittest.make(
    _runtime_tree_is_the_forest_impl,
    attrs = {"test": attr.label(aspects = [_actions_aspect])},
)
