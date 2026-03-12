# rules_typescript

Bazel rules for TypeScript using [Oxc](https://oxc.rs/) for compilation and [tsgo](https://github.com/nicholasgasior/TypeScript-7) for type-checking.

**TypeScript on Bazel should feel like Go on Bazel.** Write `.ts` files, let Gazelle generate BUILD files, get hermetic cached builds with sub-second incremental feedback.

**Full documentation: [nicholasgasior.github.io/rules_typescript](https://nicholasgasior.github.io/rules_typescript)**

## Key Ideas

- **Oxc compiles** — A Rust-based TypeScript/JSX transformer produces `.js`, `.js.map`, and `.d.ts` per source file. No `tsc`. Hundreds of files in milliseconds.
- **tsgo type-checks** — The Go port of TypeScript runs as a Bazel validation action. Type errors surface on `bazel build` but don't block downstream compilation.
- **Isolated declarations** — Explicit return types on exports make `.d.ts` emit a per-file syntactic transform. Changing an implementation without changing the public API doesn't recompile dependents.
- **Gazelle generates BUILD files** — Auto-infers `ts_compile` targets, resolves imports to Bazel labels, handles npm packages.

## Requirements

The only prerequisite is **Bazelisk** (or Bazel 9+). Everything else — the Rust toolchain, Go toolchain, Node.js runtime, and all npm packages — is fetched hermetically on first build.

Supported platforms: Linux x86_64, Linux ARM64, macOS x86_64, macOS ARM64.

## Install

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create `WORKSPACE.bazel` (empty — required by Bazel 9).

**Step 3.** Add to `MODULE.bazel`:

```python
module(name = "my_project", version = "0.0.0")

bazel_dep(name = "rules_typescript", version = "0.1.0")
register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

**Step 4.** Add to `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

**Step 5.** Add to `BUILD.bazel`:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write TypeScript with explicit return types on exports:

```typescript
export function add(a: number, b: number): number {
  return a + b;
}
```

**Step 7.** Generate BUILD files, build, and test:

```bash
bazel run //:gazelle
bazel build //...
bazel test //...
```

## Feature Highlights

- **[Quick Start](https://nicholasgasior.github.io/rules_typescript/getting-started/quickstart/)** — new project or migrating an existing codebase
- **[Isolated Declarations](https://nicholasgasior.github.io/rules_typescript/getting-started/isolated-declarations/)** — the architectural keystone for fast incremental builds
- **[npm Dependencies](https://nicholasgasior.github.io/rules_typescript/guides/npm/)** — pnpm lockfile integration, platform-specific packages, bin scripts
- **[Testing with vitest](https://nicholasgasior.github.io/rules_typescript/guides/testing/)** — `ts_test`, snapshots, sharding, watch mode with ibazel
- **[Bundling](https://nicholasgasior.github.io/rules_typescript/guides/bundling/)** — `ts_bundle` with Vite or any `BundlerInfo`-compatible bundler
- **[Dev Server](https://nicholasgasior.github.io/rules_typescript/guides/dev-server/)** — Vite dev server with ibazel HMR
- **[Monorepo Layout](https://nicholasgasior.github.io/rules_typescript/guides/monorepo/)** — package boundaries, cross-package `.d.ts` caching
- **[Gazelle Reference](https://nicholasgasior.github.io/rules_typescript/gazelle/overview/)** — directives, `gazelle_ts.json`, auto-detected lint targets
- **[Rules Reference](https://nicholasgasior.github.io/rules_typescript/rules/ts-compile/)** — all attributes, providers, and outputs
- **[Migration from rules_ts](https://nicholasgasior.github.io/rules_typescript/getting-started/migration/)** — differences from aspect-build/rules_ts

## License

MIT
