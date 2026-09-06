"""Analysis-time proof that every `paths` value names a library file.

TypeScript classifies a `paths` match by its path alone: under a node_modules
segment it is a library file, type-checked and never emitted; under none it is
project source, emit-eligible and checked against `rootDir` (TS6059 the moment
the file is a .ts). The build tests beside this prove the package resolves; this
pins which of the two every value the ruleset writes for it is.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_PACKAGE = "@cloudflare/workers-types"

def _paths_of(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"]["paths"]
    return None

def _library_paths_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _paths_of(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    segment = "/node_modules/" + _PACKAGE + "/"
    for key in (_PACKAGE, _PACKAGE + "/*"):
        values = paths.get(key)
        asserts.true(env, values != None, "{} has no paths entry".format(key))
        for value in values or []:
            asserts.true(
                env,
                segment in value,
                "{} names a file outside node_modules, which TypeScript takes for project source: {}".format(key, value),
            )
    return analysistest.end(env)

library_paths_test = analysistest.make(_library_paths_impl)
