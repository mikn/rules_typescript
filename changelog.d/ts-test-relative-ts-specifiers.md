### Fixed

- **A relative `.ts` specifier resolves to its compiled sibling under vitest.**
  The emit keeps an `allowImportingTsExtensions` specifier as written and the
  runfiles hold only the `.js`, so a test importing `./util.ts` failed with
  `Cannot find module './util.ts'`. The generated config resolves a relative
  specifier ending in `.ts`, `.tsx`, `.mts` or `.cts` whose file is absent
  while the compiled sibling exists to that sibling, from the importing file's
  directory -- the rule a `setupFiles` entry is rewritten by.
