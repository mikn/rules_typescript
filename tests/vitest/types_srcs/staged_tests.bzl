"""Analysis-time proof that a ts_test `types_srcs` label reaches the type-check.

The ts_test next door running green is the end-to-end half. These assertions
pin the mechanism: what makes a file-shaped entry resolve is the file being in
the action's inputs, and a tsconfig carrying the rebased entry looks right
either way.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_STAGED = "tests/vitest/types_srcs/staged/ambient.d.ts"

def _test_compile_stages_impl(ctx):
    env = analysistest.begin(ctx)

    inputs = []
    tsconfig = None
    for action in analysistest.target_actions(env):
        if action.mnemonic == "TsgoDeclare":
            inputs = [f.short_path for f in action.inputs.to_list()]
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            tsconfig = action.content

    asserts.true(
        env,
        _STAGED in inputs,
        "the declaration the entry names is an input of the test compile's " +
        "type-check action",
    )

    asserts.true(env, tsconfig != None, "the generated ts_compile wrote no tsconfig")
    if tsconfig == None:
        return analysistest.end(env)

    types = json.decode(tsconfig)["compilerOptions"]["types"]
    asserts.equals(
        env,
        1,
        len([e for e in types if e.endswith("/" + _STAGED)]),
        "the entry, rebased onto the test compile's config: {}".format(types),
    )
    return analysistest.end(env)

test_compile_stages_test = analysistest.make(_test_compile_stages_impl)
