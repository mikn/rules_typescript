"""Analysis-time coverage for which version of a name sits flat in a target's forest.

`node_modules/<name>` is one directory, and a target reaching two versions of
one name -- its own, and an older copy inside a dependent's closure -- can have
only one of them there. The one its sources mean is the one it declared: pnpm
installed that version for this importer and the bundler resolves it, so a flat
entry holding the other type-checks the code against a package that is not the
one it runs on.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load(":forest_tests.bzl", "forest_manifest", "linked_from")

def _direct_version_wins_impl(ctx):
    env = analysistest.begin(ctx)
    manifest = forest_manifest(env)
    asserts.true(env, manifest != None, "ts_compile built no forest")
    if manifest == None:
        return analysistest.end(env)

    source = linked_from(manifest, ctx.attr.package + "/package.json")
    asserts.true(env, source != None, "'{}' has no flat entry in the forest".format(ctx.attr.package))
    if source != None:
        asserts.true(
            env,
            ctx.attr.want_dir in source,
            "node_modules/{} is copied from {}, want the repository of {}".format(ctx.attr.package, source, ctx.attr.want_dir),
        )
    return analysistest.end(env)

direct_version_wins_test = analysistest.make(
    _direct_version_wins_impl,
    attrs = {
        "package": attr.string(mandatory = True),
        "want_dir": attr.string(mandatory = True),
    },
)
