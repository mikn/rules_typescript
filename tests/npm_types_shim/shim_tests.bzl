"""Analysis-time proof of how a forwarding shim's target reaches the editor's program.

The build test beside this proves the globals are in scope under the forest;
this pins the editor's route, which has no node_modules to walk.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _editor_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _editor_forwarded_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The editor has no node_modules to walk either, so its `files` names the
    # same three entries the build's does, each installed under npm_dir.
    asserts.equals(
        env,
        [
            "./.bazel/npm/@types/bun/index.d.ts",
            "./.bazel/npm/@types/node/index.d.ts",
            "./.bazel/npm/bun-types/index.d.ts",
        ],
        config.get("files"),
        "the editor lists the whole chain",
    )
    return analysistest.end(env)

editor_forwarded_test = analysistest.make(_editor_forwarded_impl)
