"""Analysis-time proof of which file of a package the editor pins for a bare import.

@cloudflare/workers-types names no entry point and ships index.ts, a module,
beside index.d.ts, a global script. TypeScript answers `import ... from
"@cloudflare/workers-types"` with the first -- index.ts comes before index.d.ts
once the manifest is silent. The build test beside this proves the import
type-checks through the forest; this pins which file the editor's tsconfig is
written with.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:tsconfig_aspect.bzl", "WorkspaceCopyInfo")

_PACKAGE = "@cloudflare/workers-types"

def _config_named(env, basename):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename == basename:
            return json.decode(action.content)
    return None

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
