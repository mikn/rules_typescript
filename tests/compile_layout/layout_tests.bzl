"""Analysis-time coverage for the layout ts_compile gives a multi-directory target.

The go_test next to this file reads the files that were actually written. These
tests read what the rule declared and told oxc, which is where a
single-common-directory assumption shows up first: one --strip-dir-prefix has to
be the package, not the directory of whichever src sorted first.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "JsInfo")

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

def _package_relative(f):
    marker = _PKG + "/"
    return f.path[f.path.find(marker) + len(marker):]

def _action(env, mnemonic):
    for action in analysistest.target_actions(env):
        if action.mnemonic == mnemonic:
            return action
    return None

_DATA_SRCS = [
    "gamma/README.md",
    "gamma/data.json",
    "gamma/logo.svg",
    "gamma/package.json",
    "gamma/styles.css",
]

def _data_srcs_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    files = target[DefaultInfo].files.to_list()
    asserts.equals(
        env,
        [
            "gamma/README.md",
            "gamma/data.json",
            "gamma/index.d.ts",
            "gamma/index.js",
            "gamma/index.js.map",
            "gamma/logo.svg",
            "gamma/package.json",
            "gamma/styles.css",
        ],
        sorted([_package_relative(f) for f in files]),
        "DefaultInfo: the compiled module and every data src",
    )
    asserts.equals(
        env,
        [],
        [_package_relative(f) for f in files if f.is_source],
        "every file in DefaultInfo is staged under bazel-bin",
    )

    js = target[JsInfo]
    asserts.equals(
        env,
        _DATA_SRCS,
        sorted([_package_relative(f) for f in js.data_files.to_list()]),
        "JsInfo.data_files",
    )
    asserts.equals(
        env,
        _DATA_SRCS,
        sorted([
            _package_relative(f)
            for f in js.transitive_data_files.to_list()
        ]),
        "JsInfo.transitive_data_files on a target without deps",
    )

    # The JSON srcs are tsgo inputs -- an import resolves to data.json, and the
    # manifest decides the module format -- and nothing else among the data is.
    tsgo = _action(env, "TsgoDeclare") or _action(env, "TsgoCheck")
    asserts.true(env, tsgo != None, "ts_compile runs no tsgo action")
    if tsgo != None:
        asserts.equals(
            env,
            ["gamma/data.json", "gamma/index.ts", "gamma/package.json"],
            sorted([
                _package_relative(f)
                for f in tsgo.inputs.to_list()
                if not f.is_directory and "/gamma/" in f.path
            ]),
            "the tsgo action's inputs under gamma/",
        )

    # `include` is the program's root files; a JSON is reached by import.
    config = _action(env, "TsConfig")
    asserts.true(env, config != None, "ts_compile runs no TsConfig action")
    if config != None:
        asserts.equals(
            env,
            [_PKG + "/gamma/index.ts"],
            [arg for arg in config.argv if arg.startswith(_PKG + "/")],
            "the tsconfig step's root files",
        )
    return analysistest.end(env)

data_srcs_test = analysistest.make(_data_srcs_impl)

def _transitive_data_impl(ctx):
    env = analysistest.begin(ctx)
    js = analysistest.target_under_test(env)[JsInfo]
    asserts.equals(
        env,
        [],
        js.data_files.to_list(),
        "a consumer stages no data of its own",
    )
    asserts.equals(
        env,
        _DATA_SRCS,
        sorted([
            _package_relative(f)
            for f in js.transitive_data_files.to_list()
        ]),
        "JsInfo.transitive_data_files carries the dep's data srcs",
    )
    return analysistest.end(env)

transitive_data_test = analysistest.make(_transitive_data_impl)
