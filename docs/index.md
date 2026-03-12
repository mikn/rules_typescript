# rules_typescript

Bazel rules for TypeScript using [Oxc](https://oxc.rs/) for compilation and [tsgo](https://github.com/nicholasgasior/TypeScript-7) for type-checking.

**TypeScript on Bazel should feel like Go on Bazel.** Write `.ts` files, let Gazelle generate BUILD files, get hermetic cached builds with sub-second incremental feedback.

## Key Ideas

- **Oxc compiles** — A Rust-based TypeScript/JSX transformer produces `.js`, `.js.map`, and `.d.ts` per source file. No `tsc`. Hundreds of files in milliseconds.
- **tsgo type-checks** — The Go port of TypeScript runs as a Bazel validation action. Type errors surface on `bazel build` but don't block downstream compilation.
- **Isolated declarations** — Explicit return types on exports make `.d.ts` emit a per-file syntactic transform. Changing an implementation without changing the public API doesn't recompile dependents.
- **Gazelle generates BUILD files** — Gazelle infers `ts_compile` targets from your source tree, resolves imports to Bazel labels, and handles npm packages automatically.

## Install

Add to `MODULE.bazel`:

```python
bazel_dep(name = "rules_typescript", version = "0.1.0")

register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

Add to `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

## Quick Example

Write TypeScript with explicit return types on exports:

```typescript
// src/math.ts
export function add(a: number, b: number): number {
  return a + b;
}
```

Generate BUILD files and build:

```bash
bazel run //:gazelle
bazel build //...
```

Gazelle produces:

```python
ts_compile(
    name = "math",
    srcs = ["math.ts"],
    visibility = ["//visibility:public"],
)
```

## Supported Platforms

| Platform | Status |
|----------|--------|
| Linux x86_64 | Supported |
| Linux ARM64 | Supported |
| macOS x86_64 | Supported |
| macOS ARM64 | Supported |

## Documentation

- [Quick Start](getting-started/quickstart.md) — new project or migrating an existing one
- [Isolated Declarations](getting-started/isolated-declarations.md) — why this makes builds fast
- [IDE Setup](getting-started/ide-setup.md) — VS Code and WebStorm integration
- [npm Dependencies](guides/npm.md) — pnpm lockfile integration
- [Testing with vitest](guides/testing.md) — `ts_test`, snapshots, watch mode
- [Bundling](guides/bundling.md) — `ts_bundle` with Vite or custom bundlers
- [Monorepo Layout](guides/monorepo.md) — package boundaries and cross-package deps
- [Gazelle Reference](gazelle/overview.md) — directives, `gazelle_ts.json`, framework detection
- [Rules Reference](rules/ts-compile.md) — all rule attributes and providers

## License

MIT
