"""Analysis-time coverage for the layout ts_compile gives a multi-directory target.

The go_test next to this file reads the files that were actually written. These
tests read what the rule declared and told oxc, which is where a
single-common-directory assumption shows up first: one --strip-dir-prefix has to
be the package, not the directory of whichever src sorted first.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "explicitly_relative")

_PKG = "tests/compile_layout"

def _declared_outputs_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    marker = _PKG + "/"
    got = sorted([
        f.path[f.path.find(marker) + len(marker):]
        for f in target[DefaultInfo].files.to_list()
    ])
    asserts.equals(
        env,
        [
            "alpha/one.d.ts",
            "alpha/one.d.ts.map",
            "alpha/one.js",
            "alpha/one.js.map",
            "alpha/three.d.ts",
            "alpha/three.d.ts.map",
            "alpha/three.js",
            "alpha/three.js.map",
            "beta/deep/two.d.ts",
            "beta/deep/two.d.ts.map",
            "beta/deep/two.js",
            "beta/deep/two.js.map",
        ],
        got,
        "declared outputs",
    )
    return analysistest.end(env)

declared_outputs_test = analysistest.make(
    _declared_outputs_impl,
    config_settings = {str(Label("//ts:declaration_map")): True},
)

def _oxc_strip_prefix_impl(ctx):
    env = analysistest.begin(ctx)
    oxc_actions = [
        action
        for action in analysistest.target_actions(env)
        if action.mnemonic == "OxcCompile"
    ]

    # Two sibling directories are one source root, so one invocation.
    asserts.equals(env, 1, len(oxc_actions), "OxcCompile actions")
    if len(oxc_actions) != 1:
        return analysistest.end(env)

    argv = oxc_actions[0].argv
    asserts.equals(env, _PKG, argv[argv.index("--strip-dir-prefix") + 1], "--strip-dir-prefix")
    asserts.true(
        env,
        argv[argv.index("--out-dir") + 1].endswith("/" + _PKG),
        "--out-dir is the package's bin directory: " + argv[argv.index("--out-dir") + 1],
    )
    return analysistest.end(env)

oxc_strip_prefix_test = analysistest.make(_oxc_strip_prefix_impl)

def _paths_value_impl(ctx):
    env = unittest.begin(ctx)

    # A target under the tsconfig's own directory -- a ts_codegen tree in the
    # consuming package, say -- relativizes to a bare segment, which TypeScript
    # reads as a package name and rejects with TS5090.
    asserts.equals(
        env,
        "./compiled/index.d.ts",
        explicitly_relative("compiled/index.d.ts"),
        "a bare segment names itself relative",
    )
    for already in ("./here", "../../there", "/abs/elsewhere"):
        asserts.equals(
            env,
            already,
            explicitly_relative(already),
            "an already-relative value is unchanged",
        )

    return unittest.end(env)

paths_value_test = unittest.make(_paths_value_impl)
