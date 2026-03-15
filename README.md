# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

[Oxc](https://oxc.rs/) compiles. [tsgo](https://github.com/nicholasgasior/TypeScript-7) type-checks. [Vite](https://vite.dev/) bundles. [Gazelle](https://github.com/bazelbuild/bazel-gazelle) generates BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

**Full documentation: [mikn.github.io/rules_typescript](https://mikn.github.io/rules_typescript)**

## Built for the Vite Ecosystem

This ruleset is designed around **Vite** as the bundler and dev server. Vite-based frameworks work out of the box:

- **React + Vite** — SPA bundling, React Fast Refresh HMR, CSS modules
- **Remix** — full client bundle with route-based code splitting via `@remix-run/dev` Vite plugin
- **TanStack Start** — client + SSR server bundles via `@tanstack/react-start` Vite plugin
- **SvelteKit, Solid Start** — any framework that ships a Vite plugin

Frameworks that don't use Vite (e.g., Next.js with webpack/turbopack) are not a priority.

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js`, `.js.map`, and `.d.ts` per file. Hundreds of files in milliseconds.
- **tsgo type-checks** — Go port of TypeScript runs as a validation action. Type errors fail `bazel build`.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **Isolated declarations** — explicit return types on exports make `.d.ts` a per-file syntactic transform. Change implementation without changing API → no downstream recompilation.
- **Gazelle generates BUILD files** — auto-infers targets, resolves imports, detects frameworks, generates bundler + dev server targets.
- **Zero prerequisites** — only Bazelisk needed. Node.js, pnpm, Go, Rust all fetched hermetically.

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

### Adding npm dependencies

```bash
pnpm add zod --lockfile-only   # updates pnpm-lock.yaml, no node_modules created
bazel run //:gazelle           # picks up new package, updates BUILD files
bazel build //...              # downloads package hermetically, builds
```

Add the npm extension to `MODULE.bazel` (one-time setup):

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
```

No `node_modules/` directory ever exists in the source tree. The lockfile is the only npm artifact checked into git. `pnpm` is only needed to manage the lockfile — Bazel downloads all packages hermetically at build time.

## IDE Integration

A tsserver hook resolves modules live from Bazel's build graph — npm types, internal packages, path aliases. No manual `tsconfig.json` paths to maintain. Works with VS Code, Neovim, Emacs, any editor with tsserver.

```bash
bazel run //:refresh_tsconfig  # one-time: generates hook + tsconfig
```

Then add to VS Code settings: `"typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"`

See **[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)** for all editors.

## Feature Highlights

- **[Quick Start](https://mikn.github.io/rules_typescript/getting-started/quickstart/)** — new project or migrating an existing codebase
- **[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)** — live tsserver resolution from Bazel's build graph (TypeScript's GOPACKAGESDRIVER)
- **[Isolated Declarations](https://mikn.github.io/rules_typescript/getting-started/isolated-declarations/)** — the architectural keystone for fast incremental builds
- **[npm Dependencies](https://mikn.github.io/rules_typescript/guides/npm/)** — pnpm lockfile integration, platform-specific packages, bin scripts
- **[Testing with vitest](https://mikn.github.io/rules_typescript/guides/testing/)** — `ts_test`, snapshots, sharding, watch mode with ibazel
- **[Bundling](https://mikn.github.io/rules_typescript/guides/bundling/)** — `ts_bundle` with Vite or any `BundlerInfo`-compatible bundler
- **[Dev Server](https://mikn.github.io/rules_typescript/guides/dev-server/)** — Vite dev server with ibazel HMR
- **[Monorepo Layout](https://mikn.github.io/rules_typescript/guides/monorepo/)** — package boundaries, cross-package `.d.ts` caching
- **[Gazelle Reference](https://mikn.github.io/rules_typescript/gazelle/overview/)** — directives, `gazelle_ts.json`, auto-detected lint targets
- **[Rules Reference](https://mikn.github.io/rules_typescript/rules/ts-compile/)** — all attributes, providers, and outputs
- **[Migration from rules_ts](https://mikn.github.io/rules_typescript/getting-started/migration/)** — differences from aspect-build/rules_ts

## License

MIT
