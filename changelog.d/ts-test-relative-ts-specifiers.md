### Fixed

- **A relative `.ts` specifier resolves to its compiled sibling under vitest.**
  The emit keeps an `allowImportingTsExtensions` specifier as written, so a
  test importing `./util.ts` failed with `Cannot find module './util.ts'`. The
  generated config resolves a relative specifier ending in `.ts`, `.tsx`,
  `.mts` or `.cts` to the compiled sibling beside it whenever that sibling
  exists, from the importing file's directory, whether or not the source is
  staged there -- the rule a `setupFiles` entry is rewritten by.
