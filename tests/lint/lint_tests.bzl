"""Analysis-time proof of the TsLint action under each lint_config.

The action runs the config's binary through `tsaction stamp` over the
program's sources as execroot-anchored paths, passes `--config` and
`--max-warnings=0` only when the config sets them, and its stamp is in
`_validation`; a config naming no binary registers no action. The linter that
runs for real is //tests/lint_real's oxlint, through the root module's tag.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_LINT = str(Label("//ts:lint"))
_PKG = "tests/lint"
_FROM_EXECROOT = "{{EXECROOT}}/"

def _tslint_actions(env):
    return [
        action
        for action in analysistest.target_actions(env)
        if action.mnemonic == "TsLint"
    ]

def _lints_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    actions = _tslint_actions(env)
    asserts.equals(env, 1, len(actions), "TsLint actions")
    if len(actions) != 1:
        return analysistest.end(env)
    action = actions[0]
    argv = action.argv

    stamps = action.outputs.to_list()
    asserts.equals(
        env,
        [target.label.name + ".tslint"],
        [f.basename for f in stamps],
        "the stamp",
    )
    asserts.true(
        env,
        stamps[0] in target[OutputGroupInfo]._validation.to_list(),
        "the stamp is in _validation",
    )

    asserts.equals(env, "stamp", argv[1], "the tsaction subcommand")
    linter = argv[argv.index("--") + 1]
    asserts.true(
        env,
        linter.endswith("/" + _PKG + "/fake_linter"),
        "the linter is the config's binary: " + linter,
    )
    asserts.equals(
        env,
        ctx.attr.fail_on_warnings,
        "--max-warnings=0" in argv,
        "--max-warnings=0 iff fail_on_warnings",
    )
    asserts.equals(
        env,
        ctx.attr.config != "",
        "--config" in argv,
        "--config iff the config names a file",
    )
    anchored = [
        arg[len(_FROM_EXECROOT):]
        for arg in argv
        if arg.startswith(_FROM_EXECROOT)
    ]
    config = [ctx.attr.config] if ctx.attr.config else []
    want = [_PKG + "/" + f for f in config + ctx.attr.linted]
    asserts.equals(env, want, anchored, "the config and the srcs, anchored")
    inputs = [f.short_path for f in action.inputs.to_list()]
    for path in want:
        asserts.true(env, path in inputs, path + " is an input")
    return analysistest.end(env)

_LINTS_ATTRS = {
    "linted": attr.string_list(
        doc = "The package-relative srcs the action lints, in order.",
    ),
    "config": attr.string(
        doc = "The package-relative config file passed as --config, or \"\".",
    ),
    "fail_on_warnings": attr.bool(),
}

strict_lint_test = analysistest.make(
    _lints_impl,
    attrs = _LINTS_ATTRS,
    config_settings = {_LINT: str(Label("//tests/lint:strict"))},
)

loose_lint_test = analysistest.make(
    _lints_impl,
    attrs = _LINTS_ATTRS,
    config_settings = {_LINT: str(Label("//tests/lint:loose"))},
)

def _no_lint_impl(ctx):
    env = analysistest.begin(ctx)
    asserts.equals(
        env,
        0,
        len(_tslint_actions(env)),
        "TsLint actions under a lint_config naming no binary",
    )
    return analysistest.end(env)

no_lint_test = analysistest.make(
    _no_lint_impl,
    config_settings = {_LINT: str(Label("//tests/lint:none"))},
)

def lint_test_suite(name):
    """The three configs over :clean, the strict one over :clean_test too."""
    strict_lint_test(
        name = name + "_strict",
        target_under_test = ":clean",
        linted = ["clean.ts", "types.d.ts"],
        config = "lint.json",
        fail_on_warnings = True,
    )
    strict_lint_test(
        name = name + "_strict_ts_test",
        target_under_test = ":clean_test",
        linted = ["clean.test.ts"],
        config = "lint.json",
        fail_on_warnings = True,
    )
    loose_lint_test(
        name = name + "_loose",
        target_under_test = ":clean",
        linted = ["clean.ts", "types.d.ts"],
    )
    no_lint_test(
        name = name + "_none",
        target_under_test = ":clean",
    )
    native.test_suite(
        name = name,
        tests = [
            ":" + name + "_strict",
            ":" + name + "_strict_ts_test",
            ":" + name + "_loose",
            ":" + name + "_none",
        ],
    )
