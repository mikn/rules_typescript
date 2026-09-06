"""Where the `pkg/*` wildcard is rooted.

The targets next to this file prove an `exports` subpath resolves. These prove
the wildcard behind it is rooted where npm would look: with no `exports` map --
which is most of the registry -- `pkg/sub` is a plain path under the package
root, so the root has to be the first substitution. Rooting it at the entry
declaration's own directory instead doubles that directory for every subpath
written the way the package's own layout spells it.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//ts/private:ts_compile.bzl", "subpath_pattern_paths", "subpath_roots")

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
