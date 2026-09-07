"""Analysis-time coverage for ts_compile.

Three kinds of assertion live here, none of which a build test can make:

  - what the rule writes at analysis and tells oxc -- the baseline tsconfig and
    the oxc command line, read straight out of the registered actions (the
    action tsconfig itself is tsaction's output; written_tsconfig_test.go reads
    it after the build);
  - that every guard fails, with the message that names the way out.

A guard's target is tagged manual so that `bazel build //...` does not try to
analyse it and stop on the very failure being asserted.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _written_file_action(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return action
    return None

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

# Restated, not imported: a change to _BASELINE_OPTIONS has to be made here too.
_BASELINE_KEYS = {
    "strict": True,
    "module": "Preserve",
    "target": "es2022",
    "jsx": "react-jsx",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

def _baseline_file_impl(ctx):
    """The baseline is a file the action config extends FIRST, and it names no resolver.

    TypeScript couples moduleResolution to `module`: a layer that owns the one
    may state the other, and the baseline's `module` is beaten by any tsconfig
    that sets its own, so it leaves the resolver to tsgo to derive.
    """
    env = analysistest.begin(ctx)
    baseline = _written_file_action(env, ".tsconfig_baseline.json")
    asserts.true(env, baseline != None, "ts_compile wrote no baseline to extend")
    if baseline == None:
        return analysistest.end(env)

    opts = json.decode(baseline.content)["compilerOptions"]
    for key, want in _BASELINE_KEYS.items():
        asserts.equals(env, want, opts.get(key), key)
    asserts.equals(env, None, opts.get("moduleResolution"), "moduleResolution stays out of the baseline")
    asserts.equals(env, sorted(_BASELINE_KEYS), sorted(opts), "the baseline holds these keys and no other")
    return analysistest.end(env)

baseline_file_test = analysistest.make(_baseline_file_impl)

def _fails_with(message, config_settings = {}):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True, config_settings = config_settings)

declaration_map_without_tsgo_test = _fails_with(
    "--//ts:declaration_map needs the tsgo declaration emit",
    config_settings = {
        str(Label("//ts:declarations")): "oxc",
        str(Label("//ts:declaration_map")): True,
    },
)
mixed_source_roots_test = _fails_with("different roots, and one declaration emit has one rootDir")
jsx_source_test = _fails_with("which oxc has no output extension for")
