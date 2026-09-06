"""Where a `pkg/<subpath>` key points when the manifest maps the subpath through a pattern.

The build test beside this proves the import resolves; these pin the `paths`
values behind it, in the tsconfig ts_compile writes and in the editor's, since
two rules write the two and they agree only by construction.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

_REPO = "npm_workers__unenv__"

def _config_of(env, suffix):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(suffix):
            return json.decode(action.content)
    return None

def _in_package(value):
    """Whether a build `paths` value points into unenv's own repository."""
    for segment in value.split("/"):
        if _REPO in segment:
            return True
    return False

def _pattern_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _config_of(env, ".tsconfig.json")
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)
    paths = config["compilerOptions"]["paths"]

    # `./*` maps to `./dist/runtime/*.d.mts`: the target first, star and suffix
    # kept, then the package root and the entry's directory the wildcard guesses.
    wildcard = paths.get("unenv/*", [])
    asserts.equals(env, 3, len(wildcard), "unenv/*: " + str(wildcard))
    if len(wildcard) == 3:
        asserts.true(
            env,
            wildcard[0].endswith("/dist/runtime/*.d.mts") and _in_package(wildcard[0]),
            "the manifest's pattern first: " + str(wildcard),
        )
        asserts.true(
            env,
            _in_package(wildcard[1]) and wildcard[1].endswith("/node_modules/unenv/*"),
            "the package root second: " + str(wildcard),
        )
        asserts.true(env, wildcard[2].endswith("/dist/*"), "the entry's directory last: " + str(wildcard))

    # A starred key whose target has no star: every match is that one file.
    fixed = paths.get("unenv/mock/proxy-cjs/*", [])
    asserts.true(
        env,
        len(fixed) > 0 and fixed[0].endswith("/lib/mock.d.cts") and _in_package(fixed[0]),
        "unenv/mock/proxy-cjs/*: " + str(fixed),
    )

    # The exact subpath beside it keeps its own key.
    exact = paths.get("unenv/mock/proxy-cjs", [])
    asserts.true(
        env,
        len(exact) == 1 and exact[0].endswith("/lib/mock.d.cts"),
        "unenv/mock/proxy-cjs: " + str(exact),
    )
    return analysistest.end(env)

pattern_paths_test = analysistest.make(_pattern_paths_impl)

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
