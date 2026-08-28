"""Analysis-time proof of how an @types/* dep reaches tsgo.

The build test next door proves the globals are in scope; these assertions pin
the mechanism, because the previous one -- a derived `typeRoots` -- also produced
a tsconfig that looked plausible while resolving nothing.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

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
