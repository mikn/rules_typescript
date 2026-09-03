"""Analysis-time proof that a ts_test alias reaches the program the tests are in.

A `paths` key naming only the source tree looks right and leaves every
generated declaration under the alias -- written to bazel-bin, never beside its
source -- unresolved, so both values are asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _tsconfig_paths(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"].get("paths", {})
    return None

def _test_compile_paths_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _tsconfig_paths(env)
    asserts.true(env, paths != None, "the generated ts_compile wrote no tsconfig")
    if paths == None:
        return analysistest.end(env)

    values = paths.get("@shared/*")
    asserts.true(
        env,
        values != None,
        "the test compile's paths carry the alias; none of its " +
        "{} keys is \"@shared/*\"".format(len(paths)),
    )
    if values == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        1,
        len([
            v
            for v in values
            if v.startswith("..") and v.endswith("/tests/vitest/path_aliases/shared/*")
        ]),
        "one value climbs out of bazel-bin to the source tree: " + str(values),
    )
    asserts.equals(
        env,
        1,
        len([v for v in values if v == "./shared/*"]),
        "and one is the package's own bin directory, where a generated " +
        "declaration under the alias lands: " + str(values),
    )
    return analysistest.end(env)

test_compile_paths_test = analysistest.make(_test_compile_paths_impl)
