# examples/basic — Zero to Working Quickstart

A minimal `rules_typescript` example showing the essential workflow.

## What this example demonstrates

- **`ts_compile`** — compiling TypeScript source files to `.js` + `.d.ts`
- **Cross-package dependencies** — `//src/app` depends on `//src/lib` via `.d.ts` boundary
- **`ts_binary` bundling** — bundling the app entry point to a single ESM file
- **Type-checking** — tsgo validation via `--output_groups=+_validation`
- **Gazelle** — `bazel run //:gazelle` auto-generates BUILD files

## Quick start

```bash
# Build everything
bazel build //...

# Type-check with tsgo
bazel build //... --output_groups=+_validation

# Regenerate BUILD files from TypeScript sources
bazel run //:gazelle
```

## Structure

```
examples/basic/
  MODULE.bazel          # Consumer MODULE.bazel with local_path_override
  BUILD.bazel           # ts_binary bundle + gazelle target
  src/
    lib/
      math.ts           # Pure arithmetic helpers (explicit return types)
      index.ts          # Barrel re-export
      BUILD.bazel       # ts_compile
    app/
      index.ts          # Imports from //src/lib
      BUILD.bazel       # ts_compile with cross-package dep
```

## See also

[examples/app](../app/) — a fuller example with npm deps, vitest testing, and more.
