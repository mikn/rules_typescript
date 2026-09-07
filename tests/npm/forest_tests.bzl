"""Analysis-time proof that tsgo reads npm packages out of a node_modules forest.

The build tests beside this prove the programs resolve; these pin the route.
The tsgo action stages one tree artifact, `<name>/node_modules`, laid out as
node_modules.bzl lays out a runtime tree, and no npm file at its own exec-root
path: tsgo walks the forest as it walks a pnpm install. The tsconfig step is
handed the direct @types deps and nothing about the closure, so a transitive
@types package is in the forest and out of `types`.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _action(env, mnemonic):
    for action in analysistest.target_actions(env):
        if action.mnemonic == mnemonic:
            return action
    return None

def forest_manifest(env):
    """The forest's manifest lines, or None when the target builds no forest."""
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith("__manifest.txt"):
            return action.content.splitlines()
    return None

def linked_from(manifest, rel):
    """The exec path the manifest copies to `rel` inside the tree, or None."""
    for line in manifest:
        parts = line.split("\t")
        if len(parts) == 3 and parts[0] == "C" and parts[2] == rel:
            return parts[1]
    return None

def _forest_impl(ctx):
    env = analysistest.begin(ctx)
    name = analysistest.target_under_test(env).label.name

    tsgo = _action(env, "TsgoDeclare") or _action(env, "TsgoCheck")
    asserts.true(env, tsgo != None, "ts_compile runs no tsgo action")
    if tsgo == None:
        return analysistest.end(env)
    inputs = tsgo.inputs.to_list()
    trees = [f.path for f in inputs if f.is_directory]
    asserts.equals(
        env,
        1,
        len([p for p in trees if p.endswith("/" + name + "/node_modules")]),
        "the tsgo action stages one tree artifact <name>/node_modules: " + str(trees),
    )

    # A file staged at its own exec path would be a second copy of one the
    # forest holds, and TypeScript would take the two for two modules.
    asserts.equals(
        env,
        [],
        [f.path for f in inputs if not f.is_directory and "/node_modules/" in f.path],
        "no npm file is staged beside the forest",
    )

    config = _action(env, "TsConfig")
    asserts.true(env, config != None, "ts_compile writes its tsconfig in no action")
    if config != None:
        asserts.equals(
            env,
            ctx.attr.types_deps,
            [arg[len("-types_dep="):] for arg in config.argv if arg.startswith("-types_dep=")],
            "the tsconfig step is handed the direct @types deps and nothing else",
        )

    manifest = forest_manifest(env)
    asserts.true(env, manifest != None, "no forest manifest was written")
    if manifest != None:
        for rel in ctx.attr.linked:
            asserts.true(env, linked_from(manifest, rel) != None, "the forest holds " + rel)
    return analysistest.end(env)

forest_test = analysistest.make(
    _forest_impl,
    attrs = {
        "types_deps": attr.string_list(
            doc = "The @types packages the target declares directly, by the name a `types` entry uses.",
        ),
        "linked": attr.string_list(
            doc = "Paths inside the forest the target's closure has to put a file at.",
        ),
    },
)
