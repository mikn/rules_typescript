"""ts_test compiles its srcs with ts_compile's actions: over the same srcs,
deps and tsconfig, the two register the same TsConfig and tsgo actions, argv
for argv once the target's package and name are put aside, and the vitest
test's TsEmit is the compile's ES-modules emit -- -es_modules, oxc's inputs
alone (docs/rules/ts-test.md § Runners)."""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")

_ActionsInfo = provider(
    "The actions a target registered.",
    fields = ["actions"],
)

def _actions_aspect_impl(target, _ctx):
    return [_ActionsInfo(actions = target.actions)]

_actions_aspect = aspect(implementation = _actions_aspect_impl)

_COMPILE_MNEMONICS = ["TsConfig", "TsEmit", "TsgoDeclare", "TsgoCheck"]

_TSGO_INPUTS = ("-tsconfig=", "-node_modules=", "-scratch=", "-tsgo=")

def _es_modules_emit(argv):
    """The compile's TsEmit argv as the vitest runner's program registers it."""
    flags = [
        a
        for a in argv[2:]
        if a.startswith("-") and not a.startswith(_TSGO_INPUTS)
    ]
    srcs = [a for a in argv[2:] if not a.startswith("-")]
    return argv[:2] + flags + ["-es_modules"] + srcs

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
        want = compile[mnemonic]
        if mnemonic == "TsEmit":
            want = _es_modules_emit(want)
        asserts.equals(env, want, test.get(mnemonic), mnemonic + " argv")
    return unittest.end(env)

compile_parity_test = unittest.make(
    _compile_parity_impl,
    attrs = {
        "compile": attr.label(aspects = [_actions_aspect]),
        "test": attr.label(aspects = [_actions_aspect]),
    },
)
