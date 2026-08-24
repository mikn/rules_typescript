"""Analysis-time coverage for the layout ts_compile gives a multi-directory target.

The sh_test next to this file reads the files that were actually written. These
tests read what the rule told the two compilers to write, which is where a
single-common-directory assumption shows up first: one --strip-dir-prefix and
one rootDir have to be the package, not the directory of whichever src sorted
first.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "include_entry")

_PKG = "tests/compile_layout"

def _tsconfig_of(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

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

declared_outputs_test = analysistest.make(_declared_outputs_impl)

def _tsconfig_layout_impl(ctx):
    env = analysistest.begin(ctx)
    config = _tsconfig_of(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    opts = config["compilerOptions"]

    # The declarations land in the directory Bazel declared them in, at the
    # depth their path below the package gives them.
    asserts.equals(env, ".", opts["outDir"], "outDir")
    asserts.equals(env, opts["outDir"], opts["declarationDir"], "declarationDir")
    asserts.true(
        env,
        opts["rootDir"].endswith("/" + _PKG),
        "rootDir is the package, not a src's directory: " + opts["rootDir"],
    )

    # Every src at its own depth, and every include entry pointing back out of
    # the bin directory the tsconfig sits in.
    tails = sorted([entry[entry.find(_PKG):] for entry in config["include"]])
    asserts.equals(
        env,
        [
            _PKG + "/alpha/one.ts",
            _PKG + "/alpha/three.ts",
            _PKG + "/beta/deep/two.ts",
        ],
        tails,
        "include",
    )
    for entry in config["include"]:
        asserts.true(env, entry.startswith("../"), "include entry is not relative: " + entry)
    return analysistest.end(env)

tsconfig_layout_test = analysistest.make(_tsconfig_layout_impl)

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

def _include_entry_impl(ctx):
    env = unittest.begin(ctx)

    bin_dir = "bazel-out/k8-fastbuild/bin"

    asserts.equals(
        env,
        "../../../../../tests/compile_layout/alpha/one.ts",
        include_entry(bin_dir + "/" + _PKG, _PKG + "/alpha", "one.ts"),
        "a src below the package keeps its depth",
    )

    # A src in the workspace root has no directory component at all, and the
    # exec root is what that names: an entry relative to the tsconfig's own
    # directory points at the bin tree, where no source is.
    asserts.equals(
        env,
        "../../../index.ts",
        include_entry(bin_dir, "", "index.ts"),
        "a src in the workspace root",
    )

    return unittest.end(env)

include_entry_test = unittest.make(_include_entry_impl)
