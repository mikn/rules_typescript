"""Analysis-time proof of how an @types/* dep reaches the editor's nested config.

The build test next door proves the globals are in scope for the build; this
pins the editor's half, because a derived `typeRoots` once produced a tsconfig
that looked plausible while resolving nothing.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

def _nested_config(env, dest):
    for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list():
        if entry.dest == dest:
            for action in analysistest.target_actions(env):
                if entry.file in action.outputs.to_list():
                    return json.decode(action.content)
    return None

def _bare_types_entry_impl(ctx):
    env = analysistest.begin(ctx)
    config = _nested_config(env, "tests/ambient_npm_types/tsconfig.json")
    asserts.true(env, config != None, "ide_tsconfig wrote no nested tsconfig")
    if config == None:
        return analysistest.end(env)

    # `node` is answered by @types/node, whose declarations this config names in
    # `files` -- so the entry itself has nothing left to resolve, and TypeScript
    # reports TS2688 for one it cannot resolve. Which entries to strip is
    # ts_compile's resolver's answer; a copy of it here recognised `pkg` and
    # `pkg/sub` only, and left this one standing.
    asserts.equals(
        env,
        None,
        config["compilerOptions"].get("types"),
        "the `types` entry an @types/* dep answers is not left for tsc to resolve",
    )
    asserts.true(
        env,
        [f for f in config.get("files", []) if "@types/node" in f],
        "and its declarations are in `files` instead: " + str(config.get("files")),
    )
    return analysistest.end(env)

bare_types_entry_test = analysistest.make(_bare_types_entry_impl)
