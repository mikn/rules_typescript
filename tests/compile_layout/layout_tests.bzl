"""Analysis-time coverage for the layout ts_compile gives a multi-directory target.

The sh_test next to this file reads the files that were actually written. These
tests read what the rule told the two compilers to write, which is where a
single-common-directory assumption shows up first: one --strip-dir-prefix and
one rootDir have to be the package, not the directory of whichever src sorted
first.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "explicitly_relative", "include_entry", "mixed_src_packages")

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

def _mixed_src_packages_impl(ctx):
    env = unittest.begin(ctx)

    # The srcs of //tests/compile_layout:siblings, which is exactly the layout a
    # BUILD file in alpha/ or beta/deep/ would turn into the rejected one.
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, ["alpha/one.ts", "beta/deep/two.ts", ":generated.ts"]),
        "a subtree of one package is what a multi-directory target is made of",
    )

    asserts.equals(
        env,
        ["//tests/compile_layout/alpha:one.ts", "//other:two.ts"],
        mixed_src_packages(_PKG, [
            "three.ts",
            "//tests/compile_layout/alpha:one.ts",
            "//other:two.ts",
            "//" + _PKG + ":four.ts",
        ]),
        "only a label naming another package is one this target compiles twice",
    )

    # //tests/compiler_options/analysis:from_exec_root, and every ts_compile in
    # the top-level package: one root, and it is the exec root.
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, ["//tests/compiler_options/subtree:root.ts"]),
        "srcs that are ALL from elsewhere hang off one root like any other",
    )

    # vite_types prepends this to every src list it touches, and a declaration is
    # passed through rather than compiled: no output, no rootDir, no second copy.
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, ["one.ts", "@rules_typescript//ts:vite_env.d.ts"]),
        "a declaration from another package is passed through",
    )

    asserts.equals(
        env,
        ["@other_repo//ts:one.ts"],
        mixed_src_packages("ts", ["two.ts", "@other_repo//ts:one.ts"]),
        "another repository is another package even at the same path",
    )

    # An empty repository part -- `@//` and the canonical `@@//` -- is this one.
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, ["one.ts", "@@//" + _PKG + ":two.ts", "@//" + _PKG + ":three.ts"]),
        "the canonical and apparent spellings of this package are this package",
    )
    asserts.equals(
        env,
        ["@@//other:two.ts"],
        mixed_src_packages(_PKG, ["one.ts", "@@//other:two.ts"]),
        "a canonical label still has a package to compare",
    )

    # The top-level package IS the exec root a foreign src hangs off: one root.
    asserts.equals(
        env,
        [],
        mixed_src_packages("", ["one.ts", "//other:two.ts"]),
        "the top-level package and the exec root are the same root",
    )

    # A select decides its srcs after loading is over; iterating one fails.
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, select({"//conditions:default": ["one.ts", "//other:two.ts"]})),
        "a select is not a list and holds nothing this phase can read",
    )
    asserts.equals(
        env,
        [],
        mixed_src_packages(_PKG, ["one.ts"] + select({"//conditions:default": ["//other:two.ts"]})),
        "a list concatenated with a select is a select too",
    )

    return unittest.end(env)

mixed_src_packages_test = unittest.make(_mixed_src_packages_impl)
