### Fixed

- **A relative import through a chain link is owned by the tree the link
  enters.** tsgo realpaths the file a bare specifier reaches, so the listing
  names its store tree, `node_modules/.pnpm/<key>/node_modules/<name>/...`; a
  relative specifier through the link, `../node_modules/<name>/dist/
  internal.js` -- the path to a module the package's `exports` do not expose
  -- is listed at the link, and `TsgoCheck` took the file for one nothing
  owns: `tsaction: tests/npm/multi_version/link_internal.test.ts imports
  "./node_modules/minimatch/dist/esm/escape.js" resolves to
  tests/npm/multi_version/node_modules/minimatch/dist/esm/escape.d.ts, which
  no src, dep or npm package of this target owns`, with `@npm//:minimatch_9_0_9`
  in `deps`. tsaction now reads the link to its store tree and the check runs
  as for a bare specifier: declared when a link of `deps` enters the tree, the
  hub label to add when only the closure holds it, an error when the closure
  does not hold it. `//tests/npm/multi_version:link_internal_test` pins it, at
  compile time and at run time through the link in the runfiles.
