"""The depth a resolution link is written for has to be the depth it sits at.

A link target is relative and counts `..` up to the tree root. A link INTO a
package that has links of its own therefore hands those inner links a depth they
were not written for: reached through the outer link they start one level short
and resolve outside the tree entirely.

Nothing notices while the whole chain is symlinks, because resolution moves into
the physical directory at the first hop. Bazel restages a tree artifact though --
staging one into a sandbox replaces its internal links -- and an inner link left
behind at the wrong depth is then dangling. `chmod` on one of those is ENOENT,
which is what failed the remix_ssr integration test in CI, at a different path
each run because it depends which chain is walked first.

This is asserted here rather than against a built tree because no test can read
the tree as the rule emitted it: Bazel restages it on the way in.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//ts/private:node_modules.bzl", "node_modules_testing")

# The shape that failed in CI: vite-node depends on a non-primary vite, which
# depends on a non-primary picomatch. Linking vite into vite-node would leave
# picomatch's link -- written with five `..` for the store -- one level short.
_STORE_PATHS = {
    "vite-node@1.6.1": "vite-node",
    "vite@7.3.1": ".pnpm/vite@7.3.1/node_modules/vite",
    "picomatch@4.0.3": ".pnpm/picomatch@4.0.3/node_modules/picomatch",
}

_EDGES = {
    "vite-node@1.6.1": [("vite", "vite@7.3.1")],
    "vite@7.3.1": [("picomatch", "picomatch@4.0.3")],
}

def _depth(path):
    return len(path.split("/"))

def _chained_link_is_materialised_instead(ctx):
    env = unittest.begin(ctx)
    plan = node_modules_testing.plan_links(_EDGES, _STORE_PATHS)

    asserts.equals(
        env,
        [("vite@7.3.1", "vite-node/node_modules/vite")],
        plan.materialise,
        "vite has links of its own, so it is placed under vite-node rather than linked to",
    )

    # Its picomatch link is re-emitted for the position the copy sits at, and the
    # store's own copy keeps the one written for the store.
    asserts.true(
        env,
        ("vite-node/node_modules/vite/node_modules/picomatch", _STORE_PATHS["picomatch@4.0.3"]) in plan.links,
        "the copy needs its own picomatch link, got {}".format(plan.links),
    )
    asserts.true(
        env,
        (".pnpm/vite@7.3.1/node_modules/vite/node_modules/picomatch", _STORE_PATHS["picomatch@4.0.3"]) in plan.links,
        "the store copy keeps its picomatch link, got {}".format(plan.links),
    )
    return unittest.end(env)

def _no_link_lands_on_a_package_that_has_links(ctx):
    env = unittest.begin(ctx)
    plan = node_modules_testing.plan_links(_EDGES, _STORE_PATHS)
    by_store = {store: key for key, store in _STORE_PATHS.items()}
    for link_path, store_path in plan.links:
        target_key = by_store.get(store_path)
        if target_key in _EDGES:
            asserts.equals(
                env,
                _depth(store_path),
                _depth(link_path),
                "{} points at {}, which has links of its own, from a different depth -- " +
                "those links resolve outside the tree once this one is materialised".format(
                    link_path,
                    store_path,
                ),
            )
    return unittest.end(env)

chained_link_is_materialised_test = unittest.make(_chained_link_is_materialised_instead)
no_link_lands_on_a_package_that_has_links_test = unittest.make(_no_link_lands_on_a_package_that_has_links)

def link_depth_test_suite(name):
    unittest.suite(
        name,
        chained_link_is_materialised_test,
        no_link_lands_on_a_package_that_has_links_test,
    )
