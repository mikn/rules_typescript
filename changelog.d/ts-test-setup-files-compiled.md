### Fixed

- **A `config`'s `test.setupFiles` or `test.globalSetup` entry naming a
  TypeScript source runs its compiled sibling.** vitest resolves the entry
  against the root and loads that path, and the runfiles hold no source:
  `setupFiles: ["./test/vitest.setup.ts"]` failed with `Cannot find module
  '.../test/vitest.setup.ts'`. Once the layers have merged, an entry ending in
  `.ts`, `.tsx`, `.mts` or `.cts` whose file is absent while the `.js`,
  `.mjs` or `.cjs` beside it exists is rewritten to that sibling. The
  `ts_compile` over the source has to be among the test's `deps`.
