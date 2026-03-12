# examples/basic

The simplest possible rules_typescript project: two packages, cross-package deps, and a bundled binary.

## What this demonstrates

- `ts_compile` compiling TypeScript to `.js` + `.d.ts`
- Cross-package dependencies via `.d.ts` compilation boundary
- `ts_binary` bundling an entry point to a single ESM file
- tsgo type-checking (enabled by default in `.bazelrc`)
- Gazelle auto-generating BUILD files from TypeScript sources

## Structure

```
examples/basic/
  MODULE.bazel        # Workspace definition with rules_typescript dep
  .bazelrc            # Enables validation (--output_groups=+_validation)
  BUILD.bazel         # ts_binary bundle + gazelle target
  src/
    lib/
      math.ts         # Pure arithmetic helpers (explicit return types)
      index.ts        # Barrel re-export
      BUILD.bazel     # ts_compile
    app/
      index.ts        # Imports from //src/lib
      BUILD.bazel     # ts_compile with cross-package dep
```

## Quick start

```bash
bazel build //...    # compile + type-check (validation is on by default via .bazelrc)
bazel test //...     # no tests in this example (see examples/app)
bazel run //:gazelle # regenerate BUILD files from source
```

## How it works

The `//src/lib` package compiles `math.ts` and re-exports it through `index.ts`. The `//src/app` package depends on `//src/lib` and sees only its `.d.ts` outputs at compile time. This is the `.d.ts` compilation boundary: changing `math.ts` internals without changing the public type signature does not recompile `//src/app`.

The root `BUILD.bazel` defines a `ts_binary` target (`app_bundle`) that bundles `//src/app` and its transitive deps into a single ESM file. No npm dependencies, no tests, no framework code -- just the core compilation and bundling pipeline.

## Using as a template

Copy this directory and remove the `local_path_override` block in `MODULE.bazel`. Set the `rules_typescript` version to the published BCR version. No npm lockfile or test infrastructure is needed for this minimal setup.
