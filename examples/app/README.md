# examples/app — Real Application Example

A small but realistic TypeScript application showing the full `rules_typescript` workflow.

## What this example demonstrates

- **npm deps** — `zod` for schema validation, `vitest` for testing
- **Cross-package dependencies** — `//src/app` depends on `//src/schema` via `.d.ts` boundary
- **vitest testing** — `ts_test` with real assertions against zod schema validation
- **ts_binary bundling** — bundles the application entry point to a single ESM file
- **Type-checking** — tsgo validation via `--output_groups=+_validation`
- **Gazelle** — `bazel run //:gazelle` auto-generates BUILD files from TypeScript sources
- **`.d.ts` compilation boundary** — schema changes only recompile dependents if the API changes

## Quick start

```bash
# Build everything (compile + bundle)
bazel build //...

# Run the vitest test suite
bazel test //...

# Type-check with tsgo
bazel build //... --output_groups=+_validation

# Regenerate BUILD files from TypeScript sources
bazel run //:gazelle
```

## Structure

```
examples/app/
  MODULE.bazel          # Consumer MODULE.bazel with local_path_override
  pnpm-lock.yaml        # Locked npm deps (zod + vitest)
  BUILD.bazel           # ts_binary bundle + gazelle target
  src/
    schema/
      user.ts           # Zod schema with explicit type annotations
      index.ts          # Barrel re-export
      user.test.ts      # vitest test suite
      BUILD.bazel       # ts_compile + node_modules + ts_test
    app/
      index.ts          # Uses schema package
      BUILD.bazel       # ts_compile with cross-package dep
```

## Key design decisions

**Explicit type annotations on zod schemas** — `rules_typescript` uses oxc's isolated
declarations mode for fast parallel `.d.ts` emit. This requires exported variables to
carry explicit type annotations. Zod schemas can be annotated as:

```typescript
export const UserSchema: z.ZodObject<{
  id: z.ZodString;
  name: z.ZodString;
}> = z.object({ id: z.string(), name: z.string() });
```

**`node_modules` target for tests** — `ts_test` needs a hermetic `node_modules` tree
at runtime. The `node_modules()` rule builds this tree as a Bazel action — no npm/pnpm
runs during the build.

**Using this example from BCR** — remove the `local_path_override` block in
`MODULE.bazel` and set the `rules_typescript` version to the published BCR version.
