"""The ES-modules emit at analysis: which TsEmit actions a program tsgo emits
and a vitest test register, and what each reads."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_PKG = "tests/vitest/commonjs"

def _emit_actions(env):
    return [
        action
        for action in analysistest.target_actions(env)
        if action.mnemonic == "TsEmit"
    ]

def _flags(action, prefix):
    return [a for a in action.argv if a.startswith(prefix)]

def _es_twins_impl(ctx):
    env = analysistest.begin(ctx)
    emits = _emit_actions(env)
    asserts.equals(env, 2, len(emits), "TsEmit: the program's and its twins'")
    program = [a for a in emits if "-es_modules" not in a.argv]
    twins = [a for a in emits if "-es_modules" in a.argv]
    asserts.equals(env, 1, len(program), "the program's emit, by module")
    asserts.equals(env, 1, len(twins), "the twins' emit, -es_modules")
    if len(program) != 1 or len(twins) != 1:
        return analysistest.end(env)

    asserts.equals(env, 1, len(_flags(program[0], "-tsgo=")), "tsgo emits")
    asserts.equals(env, [], _flags(twins[0], "-tsgo="), "the twins are oxc's")
    asserts.equals(env, [], _flags(twins[0], "-node_modules="), "no chain")
    out_dirs = _flags(twins[0], "-out_dir=")
    twins_dir = "/" + _PKG + "/commonjs.es"
    asserts.true(
        env,
        len(out_dirs) == 1 and out_dirs[0].endswith(twins_dir),
        "the twins land under <name>.es: " + str(out_dirs),
    )
    asserts.equals(
        env,
        [_PKG + "/commonjs.es/" + f for f in ["helper.js", "vitest.setup.js"]],
        sorted([f.short_path for f in twins[0].outputs.to_list()]),
        "one twin per .ts, at its package-relative path",
    )
    return analysistest.end(env)

es_twins_test = analysistest.make(_es_twins_impl)

def _es_modules_emit_impl(ctx):
    env = analysistest.begin(ctx)
    emits = _emit_actions(env)
    asserts.equals(env, 1, len(emits), "TsEmit: the ES-modules emit alone")
    if len(emits) != 1:
        return analysistest.end(env)
    asserts.true(env, "-es_modules" in emits[0].argv, "-es_modules")
    asserts.equals(env, [], _flags(emits[0], "-tsgo="), "oxc's alone")
    return analysistest.end(env)

es_modules_emit_test = analysistest.make(_es_modules_emit_impl)
