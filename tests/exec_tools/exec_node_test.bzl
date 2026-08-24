"""Analysis tests: build actions must run the exec platform's Node.

The target platform under test is windows_amd64, for which this ruleset ships a
Node.js runtime.  A rule that runs Node inside an action must still reach for the
exec platform's binary — resolving the target platform's one produces an action
that cannot run at all.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "BundlerInfo")

_FOREIGN = "nodejs_windows_amd64"

def _action_node_impl(ctx):
    env = analysistest.begin(ctx)
    mnemonic = ctx.attr.mnemonic
    matched = []

    for action in analysistest.target_actions(env):
        if action.mnemonic != mnemonic:
            continue
        matched.append(action)
        argv = action.argv
        asserts.true(
            env,
            argv != None and len(argv) > 0,
            "{} action has no argv to check".format(mnemonic),
        )
        if argv:
            asserts.false(
                env,
                _FOREIGN in argv[0],
                "{} must run the exec platform's node, got: {}".format(mnemonic, argv[0]),
            )

    asserts.true(
        env,
        len(matched) > 0,
        "no {} action found — the test would pass vacuously".format(mnemonic),
    )
    return analysistest.end(env)

exec_node_action_test = analysistest.make(
    _action_node_impl,
    attrs = {
        "mnemonic": attr.string(mandatory = True),
    },
    config_settings = {
        "//command_line_option:platforms": [Label("//platforms:windows_amd64")],
    },
)

def _bundler_node_impl(ctx):
    env = analysistest.begin(ctx)
    deps = analysistest.target_under_test(env)[BundlerInfo].runtime_deps.to_list()
    foreign = [f.path for f in deps if _FOREIGN in f.path]
    asserts.equals(
        env,
        [],
        foreign,
        "the bundler wrapper must invoke the exec platform's node",
    )
    return analysistest.end(env)

exec_node_bundler_test = analysistest.make(
    _bundler_node_impl,
    config_settings = {
        "//command_line_option:platforms": [Label("//platforms:windows_amd64")],
    },
)
