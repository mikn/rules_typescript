"""Analysis-time proof that a ts_compile dep's npm closure is in the runtime tree.

The vitest run beside this proves lib.js resolved zod; this pins the route: the
package's files are inputs of the action that builds the test's node_modules.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _tree_action(env):
    for action in analysistest.target_actions(env):
        if action.mnemonic == "NodeModulesTree":
            return action
    return None

def _dep_closure_inputs_impl(ctx):
    env = analysistest.begin(ctx)
    action = _tree_action(env)
    asserts.true(env, action != None, "the auto node_modules target runs no NodeModulesTree action")
    if action == None:
        return analysistest.end(env)
    zod = [f.path for f in action.inputs.to_list() if "npm__zod__" in f.path]
    asserts.true(
        env,
        len(zod) > 0,
        "zod is declared by the ts_compile dep alone, and none of the tree action's " +
        "{} inputs is one of its files".format(len(action.inputs.to_list())),
    )
    return analysistest.end(env)

dep_closure_inputs_test = analysistest.make(_dep_closure_inputs_impl)
