"""Analysis-time proof that a dep's npm closure is in the consumer's `paths`.

The build test beside this proves lib.d.ts resolved its import; this pins the
route, since zod's declarations were already among the action inputs.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _written_tsconfig(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _closure_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)
    paths = config["compilerOptions"].get("paths", {})
    keys = str(sorted(paths.keys()))
    asserts.true(env, "zod" in paths, "no zod key, so lib.d.ts cannot resolve its import: " + keys)
    asserts.true(env, "zod/*" in paths, "no zod/* key: " + keys)
    return analysistest.end(env)

closure_paths_test = analysistest.make(_closure_paths_impl)
