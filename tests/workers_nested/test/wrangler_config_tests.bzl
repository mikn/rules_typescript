"""Analysis-time proof of what `wrangler_config` stages, and of what it refuses."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_SOURCE = "tests/workers_nested/wrangler.jsonc"

def _wrangler_config_runfiles_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    runfiles = target[DefaultInfo].default_runfiles
    files = [f.short_path for f in runfiles.files.to_list()]

    patches = [a for a in analysistest.target_actions(env) if a.mnemonic == "WranglerTestConfig"]
    asserts.equals(env, 1, len(patches), "one WranglerTestConfig action")
    if patches:
        argv = patches[0].argv
        asserts.true(
            env,
            "--config" in argv and argv[argv.index("--config") + 1].endswith(_SOURCE),
            "the source is the action's --config: " + str(argv),
        )

    # The copy takes the source's runfiles path and keeps its own as well, which
    # is what admits its realpath when a `?raw` load re-resolves it.
    symlinks = [(s.path, s.target_file.basename) for s in runfiles.symlinks.to_list()]
    asserts.equals(env, [(_SOURCE, "_worker_test_wrangler.jsonc")], symlinks, "the symlink at the source's path")
    asserts.true(env, "tests/workers_nested/test/_worker_test_wrangler.jsonc" in files, "the copy at its own path: " + str(files))

    # A runfiles file at the symlink's path wins over it silently, so the
    # asset_library dep's copy of the source is kept out.
    asserts.false(env, _SOURCE in files, "the dep's copy of the source is out of the runfiles")

    # A wrangler `rules` module the compiled worker imports.
    asserts.true(env, "tests/workers_nested/src/greeting.txt" in files, "a dep's AssetInfo files are in the runfiles: " + str(files))
    return analysistest.end(env)

wrangler_config_runfiles_test = analysistest.make(_wrangler_config_runfiles_impl)

def _fails_with(*messages):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        for message in messages:
            asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

data_shadow_test = _fails_with(_SOURCE + " is staged through wrangler_config; do not list it in data too.")
