"""The depth a resolution link is written for has to be the depth it sits at.

A link target is relative and counts `..` up to the tree root. A link INTO a
package that has links of its own therefore hands those inner links a depth they
were not written for: reached through the outer link they start one level short
and resolve outside the tree entirely.

Nothing notices while the whole chain is symlinks, because resolution moves into
the physical directory at the first hop. Bazel restages a tree artifact though --
staging one into a sandbox replaces its internal links -- and an inner link left
behind at the wrong depth is then dangling. `chmod` on one of those is ENOENT,
one of the two ways a nested-Bazel integration test has failed in CI; the other
is that the restaging itself comes out short, which is why the tree is no longer
allowed to come out of a cache (see NEVER_FROM_A_CACHE in node_modules.bzl).

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

# The shape the CI failure was found in: semver, in a nested node_modules inside
# a scoped transitive dependency, with a file in a subdirectory next to two at
# the package root. The directory that turned up in CI held one of the three.
_NESTED_DEST = "@babel/helper-compilation-targets/node_modules/semver"
_NESTED_STORE = ".pnpm/semver@6.3.1/node_modules/semver"
_NESTED_FILES = [
    ("external/+npm+npm__semver__6.3.1/node_modules/semver/package.json", "package.json"),
    ("external/+npm+npm__semver__6.3.1/node_modules/semver/range.bnf", "range.bnf"),
    ("external/+npm+npm__semver__6.3.1/node_modules/semver/bin/semver.js", "bin/semver.js"),
]

def _every_file_of_a_nested_package_is_placed(ctx):
    env = unittest.begin(ctx)
    lines = node_modules_testing.file_link_lines(
        _NESTED_FILES,
        _NESTED_DEST,
        _NESTED_STORE,
    )

    placed = {}
    for line in lines:
        op, target, dest = line.split("\t")
        asserts.equals(env, "S", op, "a file is placed as a file link, got {}".format(line))
        placed[dest] = target

    for _, rel in _NESTED_FILES:
        dest = "{}/{}".format(_NESTED_DEST, rel)
        asserts.true(
            env,
            dest in placed,
            "{} ships {} and nothing places it, got {}".format(_NESTED_DEST, rel, sorted(placed)),
        )
        if dest not in placed:
            continue

        # Counted off the file's own path, so `bin/semver.js` gets one more `..`
        # than the two files at the package root do. One count for the whole
        # package leaves the subdirectory's link resolving a level short of the
        # tree root, which is outside the tree.
        up = "../" * (len(dest.split("/")) - 1)
        asserts.equals(
            env,
            "{}{}/{}".format(up, _NESTED_STORE, rel),
            placed[dest],
            "the link for {} has to reach the store copy from its own directory".format(rel),
        )
    return unittest.end(env)

chained_link_is_materialised_test = unittest.make(_chained_link_is_materialised_instead)
no_link_lands_on_a_package_that_has_links_test = unittest.make(_no_link_lands_on_a_package_that_has_links)
every_file_of_a_nested_package_is_placed_test = unittest.make(_every_file_of_a_nested_package_is_placed)

def link_depth_test_suite(name):
    unittest.suite(
        name,
        chained_link_is_materialised_test,
        no_link_lands_on_a_package_that_has_links_test,
        every_file_of_a_nested_package_is_placed_test,
    )
