### Fixed

- **A `@types/*` package whose entry only forwards to another package now
  supplies that package's declarations.** `@types/bun/index.d.ts` is exactly
  `/// <reference types="bun-types" />`. The rule put that file in the
  generated tsconfig's `files` and stopped. TypeScript resolves a
  type-reference directive through `typeRoots` and a `node_modules` walk from
  the referencing file, never through `paths`: `--traceResolution` shows tsgo
  probing every ancestor `node_modules/@types` and the shim's own
  `node_modules`, reporting `Type reference directive 'bun-types' was not
  resolved`, with `paths["bun-types"]` pointing at the right file the whole
  time. `skipLibCheck` hid the `TS2688`, so the errors landed on the source:
  `TS2868: Cannot find name 'Bun'` and `TS2339: Property 'main' does not exist
  on type 'ImportMeta'`. Naming `@npm//:bun-types` in `deps` worked around it,
  since a types-only package's own entry goes in `files`.

  `npm_import` now reads the triple-slash header of each declaration a package
  designates, its entry and its `exports` subpaths, through the
  `/// <reference path=...>` siblings they pull in, and writes the packages it
  names as `type_references` on the generated target. When a consumer puts one
  of those files in `files`, each name is resolved against the referencing
  package's own dependencies, `@types/x` first as TypeScript's `typeRoots`
  lookup goes, and the answer is listed too, chain included: `@types/bun`
  reaches `bun-types`, whose entry references `node`, so `@types/node` arrives
  with it. The tsconfig `bazel run //:refresh_tsconfig` writes follows the same
  chain. `untyped_packages` still holds: a package named there answers no
  directive. A directive in a project's own `.d.ts` is unchanged; see
  `vite_types` and the troubleshooting guide.
