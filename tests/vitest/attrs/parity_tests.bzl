"""ts_test compiles its srcs with ts_compile's actions: over the same srcs,
deps and tsconfig, the two register the same TsConfig, TsEmit and tsgo
actions, argv for argv once the target's package and name are put aside."""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")

_ActionsInfo = provider(
    "The actions a target registered.",
    fields = ["actions"],
)

def _actions_aspect_impl(target, _ctx):
    return [_ActionsInfo(actions = target.actions)]

_actions_aspect = aspect(implementation = _actions_aspect_impl)

_COMPILE_MNEMONICS = ["TsConfig", "TsEmit", "TsgoDeclare", "TsgoCheck"]

def _compile_argv(target):
    """Each compile action's argv, the target's package and name abstracted."""
    out = {}
    for action in target[_ActionsInfo].actions:
        if action.mnemonic not in _COMPILE_MNEMONICS:
            continue
        out[action.mnemonic] = [
            arg.replace(target.label.package, "<package>").replace(
                target.label.name,
                "<name>",
            )
            for arg in action.argv
        ]
    return out

def _compile_parity_impl(ctx):
    env = unittest.begin(ctx)
    compile = _compile_argv(ctx.attr.compile)
    test = _compile_argv(ctx.attr.test)
    asserts.true(
        env,
        len(compile) > 0,
        "the ts_compile registers a compile action",
    )
    asserts.equals(
        env,
        sorted(compile.keys()),
        sorted(test.keys()),
        "the ts_test registers the compile actions the ts_compile does",
    )
    for mnemonic in compile:
        asserts.equals(
            env,
            compile[mnemonic],
            test.get(mnemonic),
            mnemonic + " argv",
        )
    return unittest.end(env)

compile_parity_test = unittest.make(
    _compile_parity_impl,
    attrs = {
        "compile": attr.label(aspects = [_actions_aspect]),
        "test": attr.label(aspects = [_actions_aspect]),
    },
)
