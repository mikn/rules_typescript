# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts) ([comparison](getting-started/migration.md)).

[Oxc](https://oxc.rs/) compiles. [tsgo](https://github.com/microsoft/typescript-go) type-checks. [Vite](https://vite.dev/) bundles. [Gazelle](https://github.com/bazelbuild/bazel-gazelle) generates BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

## Built for the Vite Ecosystem

This ruleset is designed around **Vite** as the bundler and dev server. A
framework that ships a Vite plugin is expressible, with the amount of proof
varying — and with one known limit worth reading before you plan a migration:
`vite_config` takes a single `.mjs`/`.js` file, so a configuration that imports
local plugin modules of its own cannot be expressed as one staged file
([bundling](guides/bundling.md#framework-plugins-via-vite_config)).

| Framework | Evidence in this repo | How |
|---|---|---|
| **React + Vite** | `examples/react-app` | SPA bundling, React Fast Refresh HMR, CSS modules |
| **TanStack Start** | `examples/tanstack-app` (SPA target), `//tests/integration:vite_bundle_test` | Client bundle; the app-mode target using `tanstackStart()` is excluded from CI |
| **Remix** | `examples/remix-app` | Client bundle with route-based code splitting |
| **SvelteKit** | Gazelle target generation only | `@sveltejs/vite-plugin-svelte` via `vite_config` |
| **Solid Start** | Gazelle target generation only | `@solidjs/start` via `vite_config` |

"Gazelle target generation only" means Gazelle knows how to emit the
`node_modules` / `vite_bundler` / `ts_bundle` targets for that framework, and
nothing in the repository builds one. `examples/` is in `.bazelignore`, so the
example workspaces are separate Bazel invocations; CI builds all six of them,
with one target excluded and the blocker named in the workflow
(`examples/tanstack-app`'s app-mode bundle, whose `vite_config` resolves
`@tanstack/react-start` from the source tree — see
[bundling](guides/bundling.md#framework-plugins-via-vite_config)).

Frameworks that don't use Vite (e.g., Next.js with webpack/turbopack) are not a priority.

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, hundreds of files in milliseconds, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo emits declarations and type-checks** — Go port of TypeScript. Unmodified TypeScript compiles: no explicit export annotations required, and the `.d.ts` are what `tsc` would produce. Type errors fail `bazel build` because the declarations are real outputs.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **Isolated declarations, when you want them** — annotate a package's exports and set `declarations = "oxc"` to have Oxc emit its `.d.ts` syntactically. Type-checking then moves off the critical path, which on a deep dependency chain shortens it substantially ([measured](rules/ts-compile.md#cost-of-each-mode)). Opt-in, per package.
- **Gazelle generates the BUILD files** — targets inferred from the directory tree, imports resolved to labels, lint / bundler / dev-server targets generated, frameworks and codegen auto-detected. Nine `# gazelle:ts_*` directives configure it.
- **Deps are what you declared** — a source may import only what a *direct* dep provides. A declaration arriving through another dep's own deps no longer satisfies an import; the build fails naming the file, the specifier and the label to add, and Gazelle writes it.
- **npm without a store** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub. A target's npm cost is its own dependency closure, not the whole lockfile, and no `node_modules/` exists in the source tree. A `node_modules` tree places every version a closure resolved, not one per name.
- **Only Bazelisk required** — Node.js, Go and Rust are fetched hermetically, and [pnpm too](guides/npm.md#hermetic-pnpm) if you want it. pnpm is needed only to edit the lockfile, never to build.

## Install

`rules_typescript` is not on the Bazel Central Registry yet, so pin it from git
with `git_override`. Add to `MODULE.bazel`:

```python
bazel_dep(name = "rules_typescript", version = "0.1.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)

register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

`bazel_dep` keeps its `version` attribute — bzlmod requires it and ignores the
value while an override is in place. The quickstart covers the
[`archive_override` and `local_path_override` alternatives](getting-started/quickstart.md#depending-on-rules_typescript);
the plain `bazel_dep` line starts resolving on its own once a version reaches
the BCR.

Add to `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

That is all of it. No `@rules_rust` flag belongs here — `rules_rust` is a
transitive dependency of `rules_typescript`, so Bazel cannot resolve the label
from your repository and fails the invocation
([troubleshooting](guides/troubleshooting.md#no-repository-visible-as-rules_rust)).

Your repository root also needs a `BUILD.bazel` (empty is fine) — `rules_rust`
resolves `//:MODULE.bazel` while fetching crates, and that requires the root to
be a package.

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
| Windows x86_64 | **Not supported** |

No tsgo binary and no hermetic pnpm binary are published for Windows — both
upstream gaps — and a few build actions still run through a bash wrapper.
Details and what does work:
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## Documentation

- [Quick Start](getting-started/quickstart.md) — new project or migrating an existing one
- [Isolated Declarations](getting-started/isolated-declarations.md) — the opt-in throughput mode
- [IDE Setup](getting-started/ide-setup.md) — the live tsserver resolution hook, for any editor that runs tsserver
- [npm Dependencies](guides/npm.md) — pnpm lockfile integration
- [Testing with vitest](guides/testing.md) — `ts_test`, the vitest config layers, coverage
- [Bundling](guides/bundling.md) — `ts_bundle` with Vite or custom bundlers
- [Monorepo Layout](guides/monorepo.md) — package boundaries and cross-package deps
- [Gazelle Reference](gazelle/overview.md) — directives, package boundaries, framework detection
- [Rules Reference](rules/ts-compile.md) — all rule attributes and providers

## License

MIT
