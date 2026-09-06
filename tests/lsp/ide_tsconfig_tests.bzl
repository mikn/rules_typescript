"""Analysis-time proof of what the IDE tsconfig says: first-party `paths` and
nothing npm, and the per-package programs the root block cannot carry.

npm packages and `@types/*` globals reach the editor through the checkout's
node_modules, the route tsc takes, so the generated file names none of them.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

def _written_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _nested_config(env, dest):
    """The nested tsconfig written for `dest`, found through the copy it declares.

    Not by basename: the root config and every nested one are single-output .json
    writes, and _written_config would return whichever came first.
    """
    for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list():
        if entry.dest != dest:
            continue
        for action in analysistest.target_actions(env):
            if entry.file in action.outputs.to_list():
                return json.decode(action.content)
    return None

def _installed(env):
    return [
        entry.dest
        for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list()
    ]

def _editor_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # zod and @types/node are the fixture's deps, and the checkout's node_modules
    # answers both, so the map holds the fixture's own package and no more.
    asserts.equals(
        env,
        ["tests/lsp/*"],
        sorted(config["compilerOptions"]["paths"].keys()),
        "the paths map names first-party packages only",
    )
    asserts.equals(env, None, config.get("files"), "no @types entry point is named in `files`")
    asserts.equals(env, None, config.get("include"), "so the implicit include is left implicit")
    asserts.equals(env, None, config["compilerOptions"].get("typeRoots"), "no typeRoots is derived")
    asserts.equals(
        env,
        [],
        _installed(env),
        "nothing is installed beside the tsconfig: no npm copy, no .bazel/npm",
    )
    return analysistest.end(env)

editor_paths_test = analysistest.make(_editor_paths_impl)

_MERGED_PACKAGE = "tests/lsp/option_groups"

def _option_merge_impl(ctx):
    env = analysistest.begin(ctx)
    config = _nested_config(env, _MERGED_PACKAGE + "/tsconfig.json")
    asserts.true(env, config != None, "no nested tsconfig was written for " + _MERGED_PACKAGE)
    if config == None:
        return analysistest.end(env)

    # One directory, one program: two targets naming one tsconfig get a single
    # file extending it, holding both their sources.
    asserts.equals(
        env,
        ["../../../tsconfig.json", "./two.tsconfig.json"],
        config["extends"],
        "the root first, then the tsconfig both targets check under",
    )
    asserts.equals(
        env,
        ["fallthrough.ts", "params.ts"],
        config["include"],
        "both targets' sources are in that program",
    )
    return analysistest.end(env)

option_merge_test = analysistest.make(_option_merge_impl)

def _member_paths_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The checkout's node_modules holds pnpm's link to a member, so the editor
    # resolves it there; a `paths` key would send it to Bazel's declarations.
    paths = config["compilerOptions"]["paths"]
    asserts.equals(
        env,
        [],
        [key for key in paths if key == "pulse" or key.startswith("pulse/")],
        "a workspace member got a paths key: " + str(sorted(paths)),
    )
    return analysistest.end(env)

member_paths_test = analysistest.make(_member_paths_impl)

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

baseline_conflict_test = _fails_with("extend the tsconfig baselines ")
