# ts_compile

Compiles TypeScript source files using Oxc. Optionally type-checks with tsgo.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    deps = ["//other/package", "@npm//:zod"],
    target = "es2022",
    jsx_mode = "react-jsx",
    isolated_declarations = True,
    enable_check = True,
    visibility = ["//visibility:public"],
)
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`, `.tsx`, or `.d.ts` files |
| `deps` | `label_list` | `[]` | `ts_compile` or `ts_npm_package` targets |
| `target` | `string` | `"es2022"` | ECMAScript target version |
| `jsx_mode` | `string` | `"react-jsx"` | JSX transform: `react-jsx`, `react`, `preserve` |
| `isolated_declarations` | `bool` | `True` | Enable isolated declarations for fast `.d.ts` emit |
| `enable_check` | `bool` | `True` | Run tsgo type-checking (requires tsgo toolchain) |

## Outputs

For each source file `foo.ts`, `ts_compile` produces:

| Output | Description |
|--------|-------------|
| `foo.js` | Compiled JavaScript |
| `foo.js.map` | Source map |
| `foo.d.ts` | TypeScript declaration file (compilation boundary) |

## Providers

`ts_compile` returns:

- **`JsInfo`** — transitive depset of `.js` files, used by `ts_binary` and `ts_bundle`
- **`TsDeclarationInfo`** — depset of `.d.ts` files, used by downstream `ts_compile` targets for type resolution
- **`OutputGroupInfo(_validation=...)`** — tsgo type-checking output, enabled by `--output_groups=+_validation`

## Type Checking

Type-checking runs as a Bazel validation action in the `_validation` output group. It executes during `bazel build` but does not block downstream compilation — if package A's type check fails, package B (which depends on A) still compiles using A's `.d.ts`.

To surface type errors on every build, add to `.bazelrc`:

```
build --output_groups=+_validation
```

## Isolated Declarations

When `isolated_declarations = True`, each `.d.ts` is generated from its source file alone with no cross-file inference. This requires all exported functions and variables to have explicit type annotations.

When `isolated_declarations = False`, Oxc falls back to emitting `.d.ts` with inferred types. Builds still work, but the `.d.ts` boundary is less precise — an implementation change that doesn't affect public types may still trigger downstream recompilation.

See [Isolated Declarations](../getting-started/isolated-declarations.md) for the full explanation.

## Architecture

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations) — before transform
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

tsgo type-checking runs as a separate Bazel action with a generated `tsconfig.json` using `rootDirs` bridging the source tree and output tree, `moduleResolution: "Bundler"`, and `--noEmit`.
