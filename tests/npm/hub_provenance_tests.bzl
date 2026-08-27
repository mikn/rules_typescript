"""Analysis test: which npm hub a ruleset-internal target's files come from.

Root-module `translate_lock` wins for *any* hub name (npm/extensions.bzl), so no
name here is reserved to the ruleset: a consumer-reachable target naming `@npm`
resolves into whatever lockfile the consumer registered, and the `dev_dependency`
hubs do not exist for a consumer at all. These tests pin which hub two such
targets resolve; they do not make the name they expect immune to being claimed.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

# Package repos are named "<hub>__<pkg>__<version>" (npm/lazy.bzl), so the hub
# name is a path prefix; @npm supplies the canonical per-extension part.
_EXTENSION_PREFIX = "external/" + Label("@npm").repo_name.removesuffix("npm")

def _hub_provenance_impl(ctx):
    env = analysistest.begin(ctx)
    expected = _EXTENSION_PREFIX + ctx.attr.hub + "__"

    paths = []
    for action in analysistest.target_actions(env):
        paths.extend([f.path for f in action.inputs.to_list()])

    from_npm_hubs = [p for p in paths if p.startswith(_EXTENSION_PREFIX)]
    asserts.true(
        env,
        len(from_npm_hubs) > 0,
        "no npm package among the action inputs -- the test would pass vacuously",
    )
    asserts.equals(
        env,
        [],
        [p for p in from_npm_hubs if not p.startswith(expected)],
        "these files come from a hub other than @{}".format(ctx.attr.hub),
    )
    return analysistest.end(env)

hub_provenance_test = analysistest.make(
    _hub_provenance_impl,
    attrs = {
        "hub": attr.string(mandatory = True),
    },
)
