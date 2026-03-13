# rules_typescript

Bazel rules for the modern **TypeScript + Vite** ecosystem. [Oxc](https://oxc.rs/) compiles, [tsgo](https://github.com/nicholasgasior/TypeScript-7) type-checks, [Vite](https://vite.dev/) bundles.

**TypeScript on Bazel should feel like Go on Bazel.** Write `.ts` files, run Gazelle, get hermetic cached builds with sub-second incremental rebuilds. No `node_modules/` directory. No system Node.js. Just Bazelisk.

## Built for the Vite Ecosystem

This ruleset is designed around **Vite** as the bundler and dev server. Any framework that ships a Vite plugin works:

| Framework | Support | How |
|---|---|---|
| **React + Vite** | Full | SPA bundling, React Fast Refresh HMR, CSS modules |
| **Remix** | Full | Client bundle with route-based code splitting |
| **TanStack Start** | Full | Client + SSR server bundles |
| **SvelteKit** | Config defined | Via `@sveltejs/kit/vite` plugin |
| **Solid Start** | Config defined | Via `@solidjs/start/vite` plugin |

Frameworks that don't use Vite (e.g., Next.js with webpack/turbopack) are not a priority.

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` + `.d.ts` per file. Hundreds of files in milliseconds.
- **tsgo type-checks** — Go port of TypeScript runs as a validation action. Type errors fail `bazel build`.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **Isolated declarations** — explicit return types on exports make `.d.ts` a per-file syntactic transform. Change implementation without changing API → no downstream recompilation.
- **Gazelle generates everything** — BUILD files, bundler targets, dev server targets, framework detection, codegen auto-detection.
- **Zero prerequisites** — only Bazelisk needed. Node.js, pnpm, Go, Rust all fetched hermetically. No `node_modules/` in the source tree.

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
