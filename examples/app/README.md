# examples/app

A standard TypeScript library/service workflow with npm deps, vitest testing, and bundling.

## What This Demonstrates

- npm dependency management (`zod`) via pnpm lockfile
- `ts_test` with vitest, in the importer chain the root `node_modules` names
- Cross-package dependencies via `.d.ts` compilation boundary
- `ts_binary` bundling to a single ESM file
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle writing the BUILD files, one package per `tsconfig.json` program

## Structure

```
examples/app/
  MODULE.bazel        # Workspace definition with npm extension
  .bazelrc            # Enables validation (--output_groups=+_validation)
  .bazelignore        # node_modules: the installed tree Gazelle lists through
  pnpm-lock.yaml      # Locked npm deps (zod + vitest)
  tsconfig.json       # The compiler options every package extends; no program
  BUILD.bazel         # ts_binary bundle, gazelle + pnpm targets, root importer
  src/
    schema/
      tsconfig.json   # Extends the root's: the package's program
      user.ts         # Zod schema with explicit type annotations
      user.test.ts    # vitest test suite
      index.ts        # Barrel re-export
      BUILD.bazel     # ts_compile + ts_test + ts_config, written by Gazelle
    app/
      tsconfig.json   # Extends the root's
      index.ts        # Uses schema package
      BUILD.bazel     # ts_compile with cross-package dep, written by Gazelle
```

## Quick Start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # run vitest tests
bazel run //:pnpm -- install --frozen-lockfile  # the tree Gazelle lists through
bazel run //:gazelle # write the BUILD files from the tsconfig.json programs
```

## How It Works

The `//src/schema` package uses `zod` as an npm dependency for runtime
validation. The `ts_compile` target lists `@npm//:zod` in `deps`, which provides
`.d.ts` files at compile time. The `ts_test` target runs vitest against the
schema logic in the importer chain its `node_modules` names -- the root
`node_modules` target, the root importer's links into the store -- and its deps
carry the root `package.json`'s dependencies beside the imports tsgo lists.

Gazelle writes one package per `tsconfig.json` program:
`src/schema/tsconfig.json` and `src/app/tsconfig.json` extend the root
`tsconfig.json`, which sets the compiler options and lists no files, so it is no
program of its own. A run lists each program through the installed
`node_modules`, so `bazel run //:pnpm -- install --frozen-lockfile` precedes it;
`bazel run //:gazelle -- -mode=diff` prints nothing on this tree.

The `//src/app` package depends on `//src/schema` via the `.d.ts` boundary. The root `ts_binary` bundles everything into a single ESM file. Zod's runtime code is resolved from the npm tree during bundling.

Exported zod schemas need explicit type annotations for oxc's isolated declarations mode. For example, `export const UserSchema: z.ZodObject<{...}> = z.object({...})` rather than relying on type inference.

## Using as a Template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in -- run `pnpm install` to update it when adding new npm dependencies.
