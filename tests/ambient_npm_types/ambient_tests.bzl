"""Analysis-time proof of how an @types/* dep reaches tsgo.

The build test next door proves the globals are in scope; these assertions pin
the mechanism, because the previous one -- a derived `typeRoots` -- also produced
a tsconfig that looked plausible while resolving nothing.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

def _written_tsconfig(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _node_entries(config):
    return [
        entry
        for entry in config.get("files", [])
        if "types_node" in entry and entry.endswith("/index.d.ts")
    ]

def _ambient_entry_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    asserts.equals(
        env,
        1,
        len(_node_entries(config)),
        "@types/node's entry point is in `files`: " + str(config.get("files", [])),
    )

    # A typeRoot is a directory whose *children* are the type packages, and one
    # npm repo per package leaves no such directory to name.
    asserts.equals(
        env,
        None,
        config["compilerOptions"].get("typeRoots"),
        "no typeRoots is derived",
    )
    return analysistest.end(env)

ambient_entry_test = analysistest.make(_ambient_entry_impl)

def _no_ambient_entry_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    # Same sources, no @types/node: the entry has to come from the dep, or the
    # test above passes for a target that never asked for node's globals.
    asserts.equals(
        env,
        [],
        _node_entries(config),
        "nothing puts node's globals in scope: " + str(config.get("files", [])),
    )
    return analysistest.end(env)

no_ambient_entry_test = analysistest.make(_no_ambient_entry_impl)

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
