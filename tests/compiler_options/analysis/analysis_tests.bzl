"""Analysis-time coverage for ts_compile.

Two kinds of assertion live here, both of which a build test cannot make:

  - what the rule *tells* the compilers -- the generated tsconfig and the oxc
    command line, read straight out of the registered actions;
  - that every guard fails, with the message that names the way out.

A guard's target is tagged manual so that `bazel build //...` does not try to
analyse it and stop on the very failure being asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "CssInfo")

def _written_file_action(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return action
    return None

def _generated_tsconfig_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    config = json.decode(action.content)
    opts = config["compilerOptions"]

    # A JavaScript src is in `include`, so tsgo has to be told to read it.
    asserts.equals(env, True, opts.get("allowJs"), "allowJs")
    asserts.equals(env, True, opts.get("checkJs"), "checkJs reached tsgo")
    asserts.equals(env, True, opts.get("declarationMap"), "declarationMap")

    # The declarations land where Bazel declared them: alongside the .js in the
    # package's bin directory, at the depth rootDir gives them.
    asserts.equals(env, ".", opts["outDir"], "outDir")
    asserts.equals(env, opts["outDir"], opts["declarationDir"], "declarationDir")
    asserts.true(
        env,
        opts["rootDir"].endswith("/tests/compiler_options/analysis"),
        "rootDir is the package: " + opts["rootDir"],
    )

    # include carries every src at its own depth -- the nested one included.
    nested = [entry for entry in config["include"] if entry.endswith("/nested/leaf.ts")]
    asserts.equals(env, 1, len(nested), "include entry for the nested source: " + str(config["include"]))
    return analysistest.end(env)

generated_tsconfig_test = analysistest.make(_generated_tsconfig_impl)

def _oxc_command_line_impl(ctx):
    env = analysistest.begin(ctx)
    oxc_actions = [
        action
        for action in analysistest.target_actions(env)
        if action.mnemonic == "OxcCompile"
    ]

    # One invocation, not one per directory: --strip-dir-prefix is the package,
    # so a source's depth below it survives into --out-dir.
    asserts.equals(env, 1, len(oxc_actions), "OxcCompile actions")
    if len(oxc_actions) != 1:
        return analysistest.end(env)

    argv = oxc_actions[0].argv
    out_dir = argv[argv.index("--out-dir") + 1]
    asserts.true(
        env,
        out_dir.endswith("/tests/compiler_options/analysis"),
        "--out-dir is the package's bin directory: " + out_dir,
    )
    asserts.equals(
        env,
        "tests/compiler_options/analysis",
        argv[argv.index("--strip-dir-prefix") + 1],
        "--strip-dir-prefix",
    )
    return analysistest.end(env)

oxc_command_line_test = analysistest.make(_oxc_command_line_impl)

def _exec_root_source_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    opts = json.decode(action.content)["compilerOptions"]

    # The exec root is a source root like any other. Read as a boolean it is
    # indistinguishable from "no root dir", and rootDir then points at the bin
    # directory the tsconfig sits in -- no source is under that, so tsgo writes
    # the declarations somewhere Bazel never declared.
    asserts.true(
        env,
        opts["rootDir"].startswith("../") and not opts["rootDir"].endswith("/bin"),
        "rootDir climbs out to the exec root: " + opts["rootDir"],
    )
    asserts.equals(env, ".", opts["outDir"], "outDir")
    return analysistest.end(env)

exec_root_source_test = analysistest.make(_exec_root_source_impl)

def _forwarded_files_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    # DefaultInfo is this target's own outputs. A dep's .css reaches a consumer
    # through CssInfo, which is the provider that says what it is.
    default_files = [f.basename for f in target[DefaultInfo].files.to_list()]
    asserts.equals(
        env,
        [],
        [name for name in default_files if name.endswith(".css")],
        "DefaultInfo carries a dep's CSS: " + str(default_files),
    )
    asserts.true(
        env,
        "styled.js" in default_files,
        "DefaultInfo is missing this target's own output: " + str(default_files),
    )

    # ts_compile produces no CSS, so its direct set is empty and the closure
    # travels in the transitive one.
    asserts.equals(env, [], target[CssInfo].css_files.to_list(), "CssInfo.css_files")
    asserts.equals(
        env,
        ["styles.css"],
        [f.basename for f in target[CssInfo].transitive_css_files.to_list()],
        "CssInfo.transitive_css_files",
    )
    return analysistest.end(env)

forwarded_files_test = analysistest.make(_forwarded_files_impl)

def _quoted_option_impl(ctx):
    env = analysistest.begin(ctx)
    action = _written_file_action(env, ".tsconfig.json")
    asserts.true(env, action != None, "ts_compile generated no tsconfig")
    if action == None:
        return analysistest.end(env)

    # json.decode is the assertion: a tsconfig assembled by string
    # concatenation does not survive a value with a quote and a backslash in it.
    opts = json.decode(action.content)["compilerOptions"]
    asserts.equals(env, "a\"b\\c", opts["jsxFactory"], "jsxFactory round-tripped")
    return analysistest.end(env)

quoted_option_test = analysistest.make(_quoted_option_impl)

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

bazel_owned_option_test = _fails_with("compilerOptions.outDir is set by the rule")
alias_into_output_tree_test = _fails_with("points into the output tree")
alias_without_inputs_test = _fails_with("where none of this target's inputs live")
rejected_tsgo_arg_test = _fails_with("Only flags that report on the program are allowed")
declaration_map_without_tsgo_test = _fails_with("declaration_map on")
mixed_source_roots_test = _fails_with("different roots, and one declaration emit has one rootDir")
jsx_source_test = _fails_with("which oxc has no output extension for")
