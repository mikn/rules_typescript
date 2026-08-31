"""Analysis-time coverage for a `module_name` dep nested under its consumer.

A dep in a sibling package is reached by climbing out of the consumer's bin
directory, so every `paths` value it produces opens with `..` and is a relative
path by accident. A dep BELOW the consumer has nothing to climb, and the bare
result TypeScript reads as a module specifier rather than a path (TS5090).
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "explicitly_relative")

def _paths_of(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"].get("paths", {})
    return None

def _nested_module_paths_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _paths_of(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    values = [value for key in sorted(paths) for value in paths[key]]
    asserts.equals(
        env,
        [],
        [v for v in values if not v.startswith(".")],
        "every paths value is visibly relative",
    )

    # Without this the fixture could stop being the nested case -- every value
    # opening with `..` is the sibling shape, which never had the bug.
    asserts.true(
        env,
        [v for v in values if not v.startswith("..")],
        "the dep's declaration root is under the consumer: " + str(values),
    )
    return analysistest.end(env)

nested_module_paths_test = analysistest.make(_nested_module_paths_impl)

def _explicitly_relative_impl(ctx):
    env = unittest.begin(ctx)
    asserts.equals(env, "./lib", explicitly_relative("lib"), "a bare path gets the prefix")
    asserts.equals(env, "../lib", explicitly_relative("../lib"), "a climbing path is left alone")
    asserts.equals(env, ".", explicitly_relative("."), "the tsconfig's own directory")
    return unittest.end(env)

explicitly_relative_test = unittest.make(_explicitly_relative_impl)
