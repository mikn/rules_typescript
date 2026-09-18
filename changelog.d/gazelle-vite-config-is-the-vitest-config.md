### Fixed

- **Gazelle takes a `vite.config.*` as a `ts_test`'s config when the package
  has no `vitest.config.*`, as `vitest` does.** The generator looked for
  `vitest.config.*` alone, so a package that configures vitest through its
  `vite.config.ts` -- a `define` the tests read, a plugin, a `resolve.alias` --
  got a `ts_test` with no `config`, and `pnpm vitest` passed where the target
  failed with `ReferenceError: __X__ is not defined`. The search now runs over
  `vitest.config.{ts,mts,cts,js,mjs,cjs}` and then
  `vite.config.{ts,mts,cts,js,mjs,cjs}`, vitest's own order; the file is loaded
  as any `config` is, and its imports are the test's deps.
