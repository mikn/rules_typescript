"""Analysis test: which lockfile's store a ruleset-internal node_modules links.

Root-module `translate_lock` wins for *any* hub name (npm/extensions.bzl), so no
name here is reserved to the ruleset: a consumer-reachable target naming `@npm`
resolves into whatever lockfile the consumer registered, and the `dev_dependency`
hubs do not exist for a consumer at all. A store tree sits in its lockfile's
package, so the package every tree of a target's files lies under is the
lockfile it came from."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _store_provenance_impl(ctx):
    env = analysistest.begin(ctx)
    prefix = ctx.attr.store_package + "/node_modules/.pnpm/"
    trees = [
        f.short_path
        for f in analysistest.target_under_test(env)[DefaultInfo]
            .files
            .to_list()
        if f.is_directory
    ]
    asserts.true(
        env,
        len(trees) > 0,
        "no store tree among the files -- the test would pass vacuously",
    )
    asserts.equals(
        env,
        [],
        [p for p in trees if not p.startswith(prefix)],
        "these trees come from a store other than {}'s".format(
            ctx.attr.store_package,
        ),
    )
    return analysistest.end(env)

store_provenance_test = analysistest.make(
    _store_provenance_impl,
    attrs = {
        "store_package": attr.string(
            mandatory = True,
            doc = "The lockfile's package, where its store sits.",
        ),
    },
)
