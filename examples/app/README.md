# examples/app

A standard TypeScript library/service workflow with npm deps, vitest testing, and bundling.

## What this demonstrates

- npm dependency management (`zod`) via pnpm lockfile
- `ts_test` with vitest (auto-generates `node_modules` from `@npm` deps)
- Cross-package dependencies via `.d.ts` compilation boundary
- `ts_binary` bundling to a single ESM file
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle auto-generating BUILD files from TypeScript sources

## Structure

```
examples/app/
  MODULE.bazel        # Workspace definition with npm extension
  .bazelrc            # Enables validation (--output_groups=+_validation)
  pnpm-lock.yaml      # Locked npm deps (zod + vitest)
  BUILD.bazel         # ts_binary bundle + gazelle target
  src/
    schema/
      user.ts         # Zod schema with explicit type annotations
      user.test.ts    # vitest test suite
      index.ts        # Barrel re-export
      BUILD.bazel     # ts_compile + ts_test
    app/
      index.ts        # Uses schema package
      BUILD.bazel     # ts_compile with cross-package dep
```

## Quick start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # run vitest tests
bazel run //:gazelle # regenerate BUILD files from source
```

## How it works

The `//src/schema` package uses `zod` as an npm dependency for runtime validation. The `ts_compile` target lists `@npm//:zod` in `deps`, which provides `.d.ts` files at compile time. The `ts_test` target runs vitest against the schema logic -- it lists its `@npm` deps directly and `ts_test` auto-generates the `node_modules` tree needed at runtime. No manual `node_modules` target is required.

The `//src/app` package depends on `//src/schema` via the `.d.ts` boundary. The root `ts_binary` bundles everything into a single ESM file. Zod's runtime code is resolved from the npm tree during bundling.

Exported zod schemas need explicit type annotations for oxc's isolated declarations mode. For example, `export const UserSchema: z.ZodObject<{...}> = z.object({...})` rather than relying on type inference.

## Using as a template

Copy this directory. Remove the `local_path_override` block in `MODULE.bazel` and set the `rules_typescript` version to the published BCR version. Keep `pnpm-lock.yaml` checked in -- run `pnpm install` to update it when adding new npm dependencies.
