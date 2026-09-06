"""The ts_test's runtime node_modules and its compile's forest plan one tree, the
ts_compile dep's closure included: what tsgo resolved against is what node runs."""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")

_ActionsInfo = provider("The actions a target registered.", fields = ["actions"])

def _actions_aspect_impl(target, _ctx):
    return [_ActionsInfo(actions = target.actions)]

_actions_aspect = aspect(implementation = _actions_aspect_impl)

def _manifest(actions):
    for action in actions:
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith("__manifest.txt"):
            return sorted(action.content.splitlines())
    return None

def _runtime_tree_is_the_forest_impl(ctx):
    env = unittest.begin(ctx)
    runtime = _manifest(ctx.attr.runtime[_ActionsInfo].actions)
    forest = _manifest(ctx.attr.compile[_ActionsInfo].actions)
    asserts.true(env, runtime != None, "the node_modules target writes no tree manifest")
    asserts.true(env, forest != None, "the compile target writes no forest manifest")
    if runtime != None and forest != None:
        asserts.equals(env, forest, runtime, "the runtime tree and the type-check's forest are one layout")
        asserts.true(
            env,
            len([line for line in runtime if line.split("\t")[0] == "C" and line.endswith("zod/package.json")]) > 0,
            "zod, declared by the ts_compile dep alone, is in the tree",
        )
    return unittest.end(env)

runtime_tree_is_the_forest_test = unittest.make(
    _runtime_tree_is_the_forest_impl,
    attrs = {
        "runtime": attr.label(aspects = [_actions_aspect]),
        "compile": attr.label(aspects = [_actions_aspect]),
    },
)
