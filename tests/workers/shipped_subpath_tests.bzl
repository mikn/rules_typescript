"""Analysis-time proof of how a `types` entry naming a shipped subpath reaches tsgo.

The build test beside it proves the subpath's globals are in scope; this pins
which of the package's declarations the generated tsconfig lists for it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

def _written_tsconfig(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)
    return None

def _shipped_subpath_entry_impl(ctx):
    env = analysistest.begin(ctx)
    config = _written_tsconfig(env)
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    # The manifest has no `exports`, so the entry resolved to the
    # experimental/index.d.ts the package ships, and that file is in `files`.
    ours = [f for f in config.get("files", []) if "cloudflare_workers-types" in f]
    asserts.equals(
        env,
        1,
        len([f for f in ours if f.endswith("/experimental/index.d.ts")]),
        "the subpath's declaration is in `files`: " + str(config.get("files", [])),
    )

    # A types-only package's root joins `files` unasked; the entry names which of
    # its declarations this target compiles against, and the root stays out.
    asserts.equals(
        env,
        [],
        [f for f in ours if not f.endswith("/experimental/index.d.ts")],
        "the root entry yields to the one the target named: " + str(ours),
    )
    return analysistest.end(env)

shipped_subpath_entry_test = analysistest.make(_shipped_subpath_entry_impl)

def _nested_config(env, dest):
    for entry in analysistest.target_under_test(env)[WorkspaceCopyInfo].entries.to_list():
        if entry.dest == dest:
            for action in analysistest.target_actions(env):
                if entry.file in action.outputs.to_list():
                    return json.decode(action.content)
    return None

def _shipped_subpath_editor_impl(ctx):
    env = analysistest.begin(ctx)
    config = _nested_config(env, "tests/workers/tsconfig.json")
    asserts.true(env, config != None, "ide_tsconfig wrote no nested tsconfig")
    if config == None:
        return analysistest.end(env)

    # As the build answers it: the file in `files`, the root out, and no `types`
    # entry left for tsserver to resolve through a node_modules it lacks.
    ours = [f for f in config.get("files", []) if "@cloudflare/workers-types/" in f]
    asserts.equals(
        env,
        ["../../.bazel/npm/@cloudflare/workers-types/experimental/index.d.ts"],
        ours,
        "the subpath's declaration, and only it: " + str(config.get("files", [])),
    )
    asserts.equals(
        env,
        None,
        config["compilerOptions"].get("types"),
        "the entry the build resolved is not left for tsc to resolve",
    )
    return analysistest.end(env)

shipped_subpath_editor_test = analysistest.make(_shipped_subpath_editor_impl)
