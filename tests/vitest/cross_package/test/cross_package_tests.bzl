"""Analysis-time proof: a ts_test's program emits no declarations, and the
compiled module of a src from another package is held at the src's path."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_LIB = "tests/vitest/cross_package/lib/"
_TEST = "tests/vitest/cross_package/test/"

def _cross_package_program_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    actions = analysistest.target_actions(env)
    mnemonics = {a.mnemonic: True for a in actions}
    asserts.true(env, "TsgoCheck" in mnemonics, "the program is checked")
    asserts.false(env, "TsgoDeclare" in mnemonics, "no declaration emit")
    asserts.equals(
        env,
        [],
        [
            f.short_path
            for a in actions
            for f in a.outputs.to_list()
            if f.basename.endswith(".d.ts")
        ],
        "no .d.ts is an output",
    )
    asserts.false(
        env,
        hasattr(target[OutputGroupInfo], "declarations"),
        "no declarations output group",
    )

    emits = [a for a in actions if a.mnemonic == "TsEmit"]
    asserts.equals(env, 1, len(emits), "one emit over both trees")
    if emits:
        asserts.equals(
            env,
            ["-root=", "-root=" + _TEST[:-1]],
            sorted([a for a in emits[0].argv if a.startswith("-root=")]),
            "the exec root and the package: one oxc run per root",
        )

    runfiles = target[DefaultInfo].default_runfiles
    placed = {
        entry.path: entry.target_file.short_path
        for entry in runfiles.symlinks.to_list()
    }
    for name in ["engine.js", "engine.js.map", "state.js", "state.js.map"]:
        asserts.equals(
            env,
            _TEST + _LIB + name,
            placed.get(_LIB + name),
            name + " is held at the src's path",
        )
    files = [f.short_path for f in runfiles.files.to_list()]
    for path in [_LIB + "engine.ts", _TEST + _LIB + "engine.js"]:
        asserts.true(env, path in files, path + " is in the runfiles")
    return analysistest.end(env)

cross_package_program_test = analysistest.make(_cross_package_program_impl)
