"""Analysis-time proof of how a forwarding shim's target reaches the program.

The build test beside these proves the globals are in scope; these pin the
route, since `paths["bun-types"]` already pointed at the right file while the
directive resolved to nothing.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _written_tsconfig(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _entries(config, repo):
    return [
        entry
        for entry in config.get("files", [])
        if "/" + repo + "/" in entry and entry.endswith("/index.d.ts")
    ]

def _forwarded_entries_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)
    files = str(config.get("files", []))

    asserts.equals(env, 1, len(_entries(config, "+npm+npm__types_bun__1_3_5")), "the shim itself: " + files)
    asserts.equals(env, 1, len(_entries(config, "+npm+npm__bun-types__1_3_5")), "the package it forwards to: " + files)

    # bun-types' own entry references `node` and nothing here declares
    # @types/node: it arrives only if the chain is followed past the first hop.
    asserts.equals(env, 1, len(_entries(config, "+npm+npm__types_node__22_20_1")), "and what that entry references in turn: " + files)
    return analysistest.end(env)

forwarded_entries_test = analysistest.make(_forwarded_entries_impl)

def _untyped_forward_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)
    files = str(config.get("files", []))

    # untyped_packages promises no `files` entry for the named package, and a
    # directive is a second route to the same file. The shim itself stays.
    asserts.equals(env, 1, len(_entries(config, "+npm+npm__types_bun__1_3_5")), "the shim stays: " + files)
    asserts.equals(env, [], _entries(config, "+npm+npm__bun-types__1_3_5"), "the package named in untyped_packages answers no directive: " + files)
    asserts.equals(env, [], _entries(config, "+npm+npm__types_node__22_20_1"), "and nothing behind it is reached: " + files)
    return analysistest.end(env)

untyped_forward_test = analysistest.make(_untyped_forward_impl)

def _editor_config(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".json"):
            return json.decode(action.content)
    return None

def _editor_forwarded_impl(ctx):
    env = analysistest.begin(ctx)
    config = _editor_config(env)
    asserts.true(env, config != None, "ide_tsconfig wrote no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The editor has no node_modules to walk either, so its `files` names the
    # same three entries the build's does, each installed under npm_dir.
    asserts.equals(
        env,
        [
            "./.bazel/npm/@types/bun/index.d.ts",
            "./.bazel/npm/@types/node/index.d.ts",
            "./.bazel/npm/bun-types/index.d.ts",
        ],
        config.get("files"),
        "the editor lists the whole chain",
    )
    return analysistest.end(env)

editor_forwarded_test = analysistest.make(_editor_forwarded_impl)
