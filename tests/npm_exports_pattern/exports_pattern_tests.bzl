"""Where a `pkg/<subpath>` key points when the manifest maps the subpath through a pattern.

The build test beside this proves the import resolves through the forest; this
pins the `paths` values the editor's tsconfig writes for it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _config_of(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return json.decode(action.content)
    return None

def _pattern_paths_editor_impl(ctx):
    env = analysistest.begin(ctx)
    config = _config_of(env, ".json")
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)
    paths = config["compilerOptions"]["paths"]

    # The same three values, from the installed tree the editor reads.
    asserts.equals(
        env,
        [
            "./.bazel/npm/unenv/dist/runtime/*.d.mts",
            "./.bazel/npm/unenv/*",
            "./.bazel/npm/unenv/dist/*",
        ],
        paths.get("unenv/*"),
        "the manifest's pattern ahead of the wildcard's guesses",
    )
    asserts.equals(
        env,
        [
            "./.bazel/npm/unenv/lib/mock.d.cts",
            "./.bazel/npm/unenv/mock/proxy-cjs/*",
            "./.bazel/npm/unenv/dist/mock/proxy-cjs/*",
        ],
        paths.get("unenv/mock/proxy-cjs/*"),
        "a starred key answered by one file",
    )
    return analysistest.end(env)

pattern_paths_editor_test = analysistest.make(_pattern_paths_editor_impl)
