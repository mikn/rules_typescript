"""Analysis-time proof that a bare import and a `types` entry resolve to two files of one package.

@cloudflare/workers-types names no entry point and ships index.ts, a module,
beside index.d.ts, a global script. TypeScript answers `import ... from
"@cloudflare/workers-types"` with the first -- index.ts comes before index.d.ts
once the manifest is silent -- and `compilerOptions.types` with the second, since
resolveTypeReferenceDirective reads no .ts. The build test beside this proves the
import type-checks; these pin which file each role is written as, in the tsconfig
ts_compile hands tsgo and in the editor's.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

_PACKAGE = "@cloudflare/workers-types"
_ROOT = "/node_modules/" + _PACKAGE + "/"

def _config_named(env, basename):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename == basename:
            return json.decode(action.content)
    return None

def _module_entry_paths_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    config = _config_named(env, target.label.name + ".tsconfig.json")
    asserts.true(env, config != None, "ts_compile generated no tsconfig")
    if config == None:
        return analysistest.end(env)

    pinned = config["compilerOptions"]["paths"].get(_PACKAGE, [])
    asserts.true(
        env,
        len(pinned) == 1 and pinned[0].endswith(_ROOT + "index.ts"),
        "a bare import resolves to the module, index.ts, under the segment that makes it a library file: " + str(pinned),
    )

    # The other role is untouched: a types-only package's root declaration joins
    # `files` unasked, and the module is not among them.
    ours = [f for f in config.get("files", []) if _ROOT in f]
    asserts.equals(
        env,
        1,
        len([f for f in ours if f.endswith(_ROOT + "index.d.ts")]),
        "the global script is in `files`: " + str(ours),
    )
    asserts.equals(
        env,
        [],
        [f for f in ours if not f.endswith(".d.ts")],
        "and nothing but declarations is: " + str(ours),
    )
    return analysistest.end(env)

module_entry_paths_test = analysistest.make(_module_entry_paths_impl)

def _module_entry_editor_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)
    config = _config_named(env, target.label.name + ".json")
    asserts.true(env, config != None, "ide_tsconfig wrote no root tsconfig")
    if config == None:
        return analysistest.end(env)

    installed = "./.bazel/npm/" + _PACKAGE + "/"
    asserts.equals(
        env,
        [installed + "index.ts"],
        config["compilerOptions"]["paths"].get(_PACKAGE),
        "the editor pins the same module",
    )

    # A paths value the editor cannot open resolves to nothing, silently: the
    # module has to be among the files installed under npm_dir.
    copies = [e.dest for e in target[WorkspaceCopyInfo].entries.to_list() if e.dest.endswith(_PACKAGE + "/index.ts")]
    asserts.equals(
        env,
        [installed[2:] + "index.ts"],
        copies,
        "and installs it: " + str(copies),
    )
    return analysistest.end(env)

module_entry_editor_test = analysistest.make(_module_entry_editor_impl)
