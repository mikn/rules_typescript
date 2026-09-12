"""A ts_compile's declarations are the `declarations` output group and
TsInfo.declarations, never a default output: a leaf runs TsgoCheck alone,
TsgoDeclare runs when a dependent's compile reads the .d.ts."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

_PKG = "tests/declarations/"

def _rel(f):
    return f.path[f.path.find(_PKG) + len(_PKG):]

def _action(env, mnemonic):
    for action in analysistest.target_actions(env):
        if action.mnemonic == mnemonic:
            return action
    return None

def _value_after(argv, flag):
    if flag not in argv:
        return None
    return argv[argv.index(flag) + 1]

def _leaf_declares_on_demand_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    groups = target[OutputGroupInfo]

    asserts.equals(
        env,
        ["lib.js", "lib.js.map"],
        sorted([_rel(f) for f in target[DefaultInfo].files.to_list()]),
        "the default outputs: the .js and its map, no declaration",
    )
    asserts.equals(
        env,
        ["lib.d.ts"],
        [_rel(f) for f in groups.declarations.to_list()],
        "the declarations output group",
    )
    asserts.equals(
        env,
        ["lib.d.ts"],
        [_rel(f) for f in target[TsInfo].declarations.to_list()],
        "TsInfo.declarations",
    )

    check = _action(env, "TsgoCheck")
    asserts.true(env, check != None, "the leaf runs TsgoCheck")
    if check != None:
        argv = check.argv
        asserts.true(env, "--noEmit" in argv, "the check emits nothing")
        asserts.true(env, "--explainFiles" in argv, "the check lists the edges")
        asserts.equals(
            env,
            1,
            len([a for a in argv if a.startswith("-check=")]),
            "the check reads the ownership manifest",
        )
        asserts.equals(
            env,
            ["lib.tscheck"],
            [_rel(f) for f in check.outputs.to_list()],
            "the check's output is its stamp",
        )
        asserts.true(
            env,
            check.outputs.to_list()[0] in groups._validation.to_list(),
            "the stamp is in _validation",
        )

    declare = _action(env, "TsgoDeclare")
    asserts.true(env, declare != None, "the leaf registers TsgoDeclare")
    if declare != None:
        argv = declare.argv
        asserts.equals(
            env,
            ["lib.d.ts"],
            [_rel(f) for f in declare.outputs.to_list()],
            "the declare's outputs are the declarations",
        )
        for flag in [
            "--declaration",
            "--emitDeclarationOnly",
            "--noEmitOnError",
        ]:
            asserts.true(env, flag in argv, "the declare passes " + flag)
        asserts.equals(env, "false", _value_after(argv, "--noEmit"), "--noEmit")
        out_dir = _value_after(argv, "--outDir") or ""
        asserts.true(
            env,
            out_dir.endswith("/tests/declarations"),
            "--outDir is the package's bin directory: " + out_dir,
        )
        asserts.equals(
            env,
            "tests/declarations",
            _value_after(argv, "--rootDir"),
            "--rootDir",
        )
        asserts.true(env, "--explainFiles" not in argv, "no listing")
        asserts.equals(
            env,
            [],
            [a for a in argv if a.startswith(("-check=", "-stamp="))],
            "the declare checks no ownership and writes no stamp",
        )
    return analysistest.end(env)

leaf_declares_on_demand_test = analysistest.make(_leaf_declares_on_demand_impl)

def _check_reads_the_deps_declarations_impl(ctx):
    env = analysistest.begin(ctx)
    dep_dts = ctx.attr.dep[TsInfo].declarations.to_list()
    asserts.equals(
        env,
        ["lib.d.ts"],
        [_rel(f) for f in dep_dts],
        "the dep's declarations",
    )

    for mnemonic in ["TsgoCheck", "TsEmit"]:
        action = _action(env, mnemonic)
        asserts.true(env, action != None, "the dependent runs " + mnemonic)
        if action != None:
            inputs = action.inputs.to_list()
            for f in dep_dts:
                asserts.true(
                    env,
                    f in inputs,
                    "{} reads the dep's {}".format(mnemonic, _rel(f)),
                )
    return analysistest.end(env)

check_reads_the_deps_declarations_test = analysistest.make(
    _check_reads_the_deps_declarations_impl,
    attrs = {
        "dep": attr.label(
            mandatory = True,
            doc = "The ts_compile the target under test depends on.",
        ),
    },
)
