"""Unit tests for the pnpm lockfile reader.

Most of what these pin is a NON-feature. catalogs, overrides and
packageExtensions need no implementation at all: pnpm resolves every one of them
before it writes the lockfile, so the resolved graph already carries the answer
and re-deriving it would mean redoing pnpm's work with worse information.

That makes them fragile in a specific way. Each of those blocks is shaped like a
block we do read, and each sits at indent 0 next to the ones we do read, so the
only thing keeping them out is that every reader anchors on its own section
header and stops at the next indent-0 line. A reader rewritten to scan the file
for `specifier:`/`version:` pairs, or for `name@version` keys, would start
silently pulling in catalog entries and override selectors. These tests fail if
that happens.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load(
    "//ts/private:npm_translate_lock.bzl",
    "parse_importers",
    "parse_patched_dependencies",
    "parse_pnpm_lock",
)

# A lockfile with one of every block, in the order pnpm writes them.
#
# The catalog is the adversarial part: its entries carry a `link:` version and an
# npm-alias version, so a reader that scanned globally instead of anchoring would
# report a workspace member and an alias that do not exist. `overrides:` is
# adversarial the same way for the packages reader -- `lodash@4` and
# `pkg>lodash` look like package keys.
_LOCKFILE = """lockfileVersion: '9.0'

settings:
  autoInstallPeers: true

catalogs:
  default:
    catalog-linked:
      specifier: workspace:*
      version: link:packages/catalog-only
    catalog-aliased:
      specifier: npm:lodash@9.9.9
      version: lodash@9.9.9
  ts6:
    typescript:
      specifier: npm:@typescript/typescript6@6.0.2
      version: 6.0.2

overrides:
  lodash@4: 4.18.1
  '@lovable/workflow-sdk>esbuild': 0.28.1

patchedDependencies:
  '@pierre/diffs@1.3.1': 08b7159491e7c1a510536ff7395f8fc02e744b7ea3413a9491e40c869c097dcd
  prosemirror-view@1.41.4:
    hash: 7874eb3c5889534f00df2fd08dcd39bf56100a7f7eb1798cb3123409c03f4fc6
    path: patches/prosemirror-view@1.41.4.patch

importers:

  .:
    dependencies:
      shared:
        specifier: workspace:*
        version: link:packages/shared
      tw-v3:
        specifier: npm:tailwindcss@3.4.18
        version: tailwindcss@3.4.18(tsx@4.23.12)
      typescript:
        specifier: npm:@typescript/typescript6@6.0.2
        version: '@typescript/typescript6@6.0.2'
      react-dom:
        specifier: 19.2.4
        version: 19.2.4(react@19.2.4)
    devDependencies:
      lodash:
        specifier: 4.18.1
        version: 4.18.1

  packages/nested:
    dependencies:
      up-one:
        specifier: workspace:*
        version: link:../other

packages:

  '@dub/analytics@0.0.32':
    resolution: {integrity: sha512-injected==}
    peerDependencies:
      react: 19.2.4

  esbuild@0.27.3:
    resolution: {integrity: sha512-old==}

  esbuild@0.28.1:
    resolution: {integrity: sha512-new==}

  lodash@4.18.1:
    resolution: {integrity: sha512-lodash==}

  react@19.2.4:
    resolution: {integrity: sha512-react==}

snapshots:

  '@dub/analytics@0.0.32(react@19.2.4)':
    dependencies:
      react: 19.2.4

  esbuild@0.27.3: {}

  esbuild@0.28.1: {}

  lodash@4.18.1: {}

  react@19.2.4: {}
"""

def _catalogs_are_not_importers_test(ctx):
    env = unittest.begin(ctx)
    importers = parse_importers(_LOCKFILE)

    asserts.equals(env, {
        "shared": "packages/shared",
        "up-one": "packages/other",
    }, importers["links"])
    asserts.equals(env, {
        "tw-v3": "tailwindcss@3.4.18",
        "typescript": "@typescript/typescript6@6.0.2",
    }, importers["aliases"])

    # Stated on their own because they are the whole point: these two live in
    # `catalogs:`, and pnpm has already substituted a concrete version at every
    # place they are used.
    asserts.false(env, "catalog-linked" in importers["links"])
    asserts.false(env, "catalog-aliased" in importers["aliases"])

    return unittest.end(env)

catalogs_are_not_importers_test = unittest.make(_catalogs_are_not_importers_test)

def _catalogs_and_overrides_are_not_packages_test(ctx):
    env = unittest.begin(ctx)
    packages = parse_pnpm_lock(_LOCKFILE)["packages"]

    asserts.equals(env, [
        "@dub/analytics@0.0.32",
        "esbuild@0.27.3",
        "esbuild@0.28.1",
        "lodash@4.18.1",
        "react@19.2.4",
    ], sorted(packages.keys()))

    return unittest.end(env)

catalogs_and_overrides_are_not_packages_test = unittest.make(_catalogs_and_overrides_are_not_packages_test)

def _overrides_arrive_pre_resolved_test(ctx):
    env = unittest.begin(ctx)
    packages = parse_pnpm_lock(_LOCKFILE)["packages"]

    # A global override is a version SELECTION, so its outcome is the only
    # version present: nothing has to resolve `lodash@4` -> 4.18.1.
    asserts.equals(env, "4.18.1", packages["lodash@4.18.1"]["version"])

    # A package-scoped override (`@lovable/workflow-sdk>esbuild`) materialises as
    # a second version rather than a second resolution of one version, so the
    # name@version keyed map represents it with no parent>child machinery.
    asserts.equals(env, "0.27.3", packages["esbuild@0.27.3"]["version"])
    asserts.equals(env, "0.28.1", packages["esbuild@0.28.1"]["version"])

    return unittest.end(env)

overrides_arrive_pre_resolved_test = unittest.make(_overrides_arrive_pre_resolved_test)

def _package_extensions_arrive_as_edges_test(ctx):
    env = unittest.begin(ctx)
    packages = parse_pnpm_lock(_LOCKFILE)["packages"]

    # @dub/analytics declares no peer upstream; the react peer exists only
    # because of a packageExtension. pnpm writes it into packages: as a peer
    # range and into snapshots: as a resolved edge, and the resolved edge is the
    # one that makes it a dependency here.
    asserts.equals(env, {"react": "19.2.4"}, packages["@dub/analytics@0.0.32"]["peerDependencies"])
    asserts.equals(env, {"react": "19.2.4"}, packages["@dub/analytics@0.0.32"]["dependencies"])

    return unittest.end(env)

package_extensions_arrive_as_edges_test = unittest.make(_package_extensions_arrive_as_edges_test)

def _patched_dependencies_test(ctx):
    env = unittest.begin(ctx)

    asserts.equals(env, {
        "@pierre/diffs@1.3.1": "08b7159491e7c1a510536ff7395f8fc02e744b7ea3413a9491e40c869c097dcd",
        "prosemirror-view@1.41.4": "7874eb3c5889534f00df2fd08dcd39bf56100a7f7eb1798cb3123409c03f4fc6",
    }, parse_patched_dependencies(_LOCKFILE))

    asserts.equals(env, {}, parse_patched_dependencies("lockfileVersion: '9.0'\n"))

    return unittest.end(env)

patched_dependencies_test = unittest.make(_patched_dependencies_test)

def parser_test_suite(name):
    unittest.suite(
        name,
        catalogs_are_not_importers_test,
        catalogs_and_overrides_are_not_packages_test,
        overrides_arrive_pre_resolved_test,
        package_extensions_arrive_as_edges_test,
        patched_dependencies_test,
    )
