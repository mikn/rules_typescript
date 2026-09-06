"""Where the `pkg/*` wildcard is rooted.

The targets next to this file prove an `exports` subpath resolves. These prove
the wildcard behind it is rooted where npm would look: with no `exports` map --
which is most of the registry -- `pkg/sub` is a plain path under the package
root, so the root has to be the first substitution. Rooting it at the entry
declaration's own directory instead doubles that directory for every subpath
written the way the package's own layout spells it.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "subpath_pattern_paths", "subpath_roots")

def _paths_of(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"]["paths"]
    return None

# The package root is `node_modules/<name>` inside the npm repository: nothing
# stands between it and the `*`.
def _rooted_at_package(value, pkg):
    if not value.endswith("/*"):
        return False
    directory = value[:-len("/*")]
    return pkg.repo_prefix in directory and directory.endswith("/node_modules/" + pkg.name)

_PACKAGES = [
    # Every one of these enters through a declaration in a subdirectory, so the
    # root and the entry directory are two different answers.
    struct(name = "postcss", repo_prefix = "npm__postcss__", entry_dir = "/lib"),
    struct(name = "vite", repo_prefix = "npm__vite__", entry_dir = "/dist/node"),
    struct(name = "rolldown", repo_prefix = "npm__rolldown__", entry_dir = "/dist"),
]

def _wildcard_root_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _paths_of(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    for pkg in _PACKAGES:
        value = paths.get(pkg.name + "/*")
        asserts.true(env, value != None, "{} has no wildcard key".format(pkg.name))
        if value == None:
            continue
        asserts.true(
            env,
            _rooted_at_package(value[0], pkg),
            "{}/* must look under the package root first, not {}".format(pkg.name, value[0]),
        )
        asserts.true(
            env,
            len(value) > 1 and value[1].endswith(pkg.entry_dir + "/*"),
            "{}/* dropped the entry directory fallback: {}".format(pkg.name, value),
        )
    return analysistest.end(env)

wildcard_root_test = analysistest.make(_wildcard_root_impl)

def _subpath_roots_impl(ctx):
    env = unittest.begin(ctx)

    # Two answers: the root npm resolves against, then the layout guess.
    asserts.equals(
        env,
        ["../../ext/pkg/*", "../../ext/pkg/dist/*"],
        subpath_roots("bin/web", "ext/pkg", "../../ext/pkg/dist"),
    )

    # A package entered through its own root directory has only one answer, and
    # repeating it would make TypeScript stat the same path twice.
    asserts.equals(
        env,
        ["../../ext/pkg/*"],
        subpath_roots("bin/web", "ext/pkg", "../../ext/pkg"),
    )
    return unittest.end(env)

subpath_roots_test = unittest.make(_subpath_roots_impl)

def _subpath_pattern_paths_impl(ctx):
    env = unittest.begin(ctx)

    # The manifest's target first, star kept, then the two guesses spelled with
    # the key's own prefix and suffix.
    asserts.equals(
        env,
        ["../../ext/pkg/dist/esm/*", "../../ext/pkg/*", "../../ext/pkg/dist/*"],
        subpath_pattern_paths("../../ext/pkg", "../../ext/pkg/dist", "./*", "dist/esm/*"),
    )
    asserts.equals(
        env,
        ["../../ext/pkg/dist/types/utils/*.d.ts", "../../ext/pkg/utils/*", "../../ext/pkg/dist/utils/*"],
        subpath_pattern_paths("../../ext/pkg", "../../ext/pkg/dist", "./utils/*", "dist/types/utils/*.d.ts"),
    )

    # A key with a suffix of its own keeps it in the guesses; a starless target
    # is one file every match resolves to.
    asserts.equals(
        env,
        ["../../ext/pkg/libesm/*.d.ts", "../../ext/pkg/lib/*.js"],
        subpath_pattern_paths("../../ext/pkg", "../../ext/pkg", "./lib/*.js", "libesm/*.d.ts"),
    )
    asserts.equals(
        env,
        ["../../ext/pkg/lib/mock.d.cts", "../../ext/pkg/mock/proxy-cjs/*", "../../ext/pkg/dist/mock/proxy-cjs/*"],
        subpath_pattern_paths("../../ext/pkg", "../../ext/pkg/dist", "./mock/proxy-cjs/*", "lib/mock.d.cts"),
    )

    # A pattern mapping a directory to itself is the root guess, not repeated.
    asserts.equals(
        env,
        ["../../ext/pkg/types/*", "../../ext/pkg/dist/node/types/*"],
        subpath_pattern_paths("../../ext/pkg", "../../ext/pkg/dist/node", "./types/*", "types/*"),
    )
    return unittest.end(env)

subpath_pattern_paths_test = unittest.make(_subpath_pattern_paths_impl)
