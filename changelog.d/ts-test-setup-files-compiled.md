### Fixed

- **A `config`'s `test.setupFiles` or `test.globalSetup` entry naming a
  TypeScript source runs its compiled sibling.** vitest resolves the entry
  against the root and loads that path as written:
  `setupFiles: ["./test/vitest.setup.ts"]` failed with `Cannot find module
  '.../test/vitest.setup.ts'`. Once the layers have merged, an entry ending in
  `.ts`, `.tsx`, `.mts` or `.cts` whose `.js`, `.mjs` or `.cjs` sibling is in
  the runfiles is rewritten to that sibling. The `ts_compile` over the source
  has to be among the test's `deps`.
