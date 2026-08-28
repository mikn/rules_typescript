# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts) ([comparison](getting-started/migration.md)).

Rust and Go do the work: [Oxc](https://oxc.rs/) compiles, [tsgo](https://github.com/microsoft/typescript-go) type-checks. Bundling and dev serving speak one generated [Vite](https://vite.dev/) config, run by Vite or by [oj](https://github.com/raphamorim/oj). [Gazelle](https://github.com/bazelbuild/bazel-gazelle) writes the BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

Coming from an existing TypeScript monorepo, the
[Quick Start](getting-started/quickstart.md) is the whole path: five root files,
then `bazel run //:gazelle`. [Install](#install) and
[Quick Example](#quick-example) below are the short version.

## Why Bazel for TypeScript

A TypeScript monorepo tends to reach a point where the build is the least
predictable thing in it. `tsc -b` invalidates whole projects rather than the
files that changed, `node_modules/` is one mutable directory that every tool
reads slightly differently, and CI repeats work someone already did locally.
Bazel answers those with a graph it can prune precisely. The usual objection is
what adoption costs: BUILD files to maintain, and an editor, bundler and dev
server that all have to keep working.

What this ruleset does about each:

- **A type error fails `bazel build`.** tsgo runs as a build action rather than
  a separate `tsc --noEmit` job. Declarations are real outputs, so a package
  type-checks against what its dependency emits.
- **A package rebuilds when its own inputs change.** Direct dependencies are
  enforced: a source may import only what a direct dep provides, so the graph
  Bazel prunes is the graph the code has. The build names the file, the
  specifier and the label to add, and Gazelle writes it.
- **npm cost is per target.** One Bazel repository per package, fetched on
  demand. A target pays for its own closure instead of the whole lockfile, and
  the source tree holds no `node_modules/`.
- **The editor keeps working.** `ts_refresh_tsconfig` writes a checked-in
  `tsconfig.json` out of the build graph, so tsserver, a plain `tsc` run and a
  coding agent's language server all resolve what Bazel resolves. See
  [IDE Setup](getting-started/ide-setup.md).
- **Bazel is out of the inner loop.** `ts_dev_server` hands the source tree to
  Vite or oj and steps back; HMR is the dev server's, not a rebuild.
- **Bazelisk is the only prerequisite.** Node.js, Go and Rust are fetched
  hermetically, and pnpm too if you want it.

The costs are real and worth naming. BUILD files live in your tree even though
Gazelle generates them. The first build compiles `oxc-bazel` from Rust source,
which dominates the wall time. The stack is Vite-shaped.
[Migrating from rules_ts](getting-started/migration.md) is the comparison to
read when `tsc` compatibility or a non-Vite bundler matters more than any of the
above.

## Built for the Vite Ecosystem

Vite bundles, and Vite or oj serves. A framework that ships a Vite plugin fits
either, because both read the same generated config: `vite_config` names the
config file, `vite_config_srcs` the local modules it imports, and the plugins it
exports run before Bazel's. Only the keys the generated config reads reach the
build; any other key fails the build, naming itself. See
[Framework plugins via `vite_config`](guides/bundling.md#framework-plugins-via-vite_config).

| Framework | Gazelle generates a bundle target? | What you get |
|---|---|---|
| **React + Vite** | n/a — plain Vite, no framework plugin | SPA bundle, CSS modules, Fast Refresh HMR under `react_refresh = True` |
| **TanStack Start** | yes | `client/` and `server/server.mjs`, no dev server; server functions reach the client through a generated handler id |
| **Remix** | yes | SPA bundle with per-route chunks, and SSR via [`remix_build`](rules/remix-build.md) |
| **SvelteKit** | yes | `client/` and `server/manifest.js` via [`sveltekit_build`](rules/sveltekit-build.md); `.svelte` components via [`svelte_library`](rules/svelte-library.md) |
| **Solid Start** | no | Gazelle logs the framework and the reason |

Solid Start gets no bundle target: `ts_bundle`'s `vite_config` contract is a
default export with a `plugins` array, `@solidjs/start` ships no Vite plugin,
and its `defineConfig()` returns a vinxi app. TanStack Start gets no dev
server: its SSR module runner inlines `react/jsx-runtime` instead of
externalising it against a `node_modules` tree that is a build output. Gazelle
logs the framework and the reason in both cases, and the workspace still
compiles and tests. See
[Framework detection](gazelle/overview.md#framework-detection).

`examples/` is in `.bazelignore`, so the example workspaces are separate Bazel
invocations; CI builds all six.

Frameworks that don't use Vite are not a priority. Next.js is the exception with
a rule of its own: [`next_build`](rules/next-build.md) runs the framework's own
build, and [`next_dev_server` and `next_serve`](rules/next-run.md) run the app
from source or from that build (`examples/nextjs-app`).

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo emits declarations and type-checks** — Go port of TypeScript, and the default emitter. Unmodified TypeScript compiles: no export annotations required, and the `.d.ts` are what `tsc` would produce. Type errors fail `bazel build`; the declarations are real outputs.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **The dev server is swappable** — `ts_dev_server(server = ...)` takes any target providing `DevServerInfo`. Vite is the default; `@rules_typescript//oj:dev_server` selects oj, which adopts the same generated config and needs no `@npm//:vite` in the tree. Each server declares the config fields it does not read, so a target depending on one fails at analysis time naming the field and the server. See [Choosing the server](guides/dev-server.md#choosing-the-server).
- **Isolated declarations** — annotate a package's exports, set `declarations = "oxc"`, and Oxc emits the `.d.ts` syntactically. Type-checking leaves the critical path, which shortens a deep dependency chain substantially. Opt-in, per package. See [Cost of each mode](rules/ts-compile.md#cost-of-each-mode).
- **Gazelle generates the BUILD files** — targets inferred from the directory tree, imports resolved to labels, lint / bundler / dev-server targets generated, frameworks and codegen auto-detected. Eleven `# gazelle:ts_*` directives configure it.
- **Direct dependencies** — a source may import only what a direct dep provides. A declaration arriving through another dep's own deps does not satisfy an import; the build names the file, the specifier and the label to add, and Gazelle writes it.
- **How npm packages are fetched** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub. A target's npm cost is its own closure, not the whole lockfile, and the source tree holds no `node_modules/`. A materialised tree carries one entry per resolution — name, version and peer set — rather than one directory per name.
- **Only Bazelisk required** — Node.js, Go and Rust are fetched hermetically, and [pnpm too](guides/npm.md#hermetic-pnpm) if you want it. pnpm is needed only to edit the lockfile, never to build.

## Install

`rules_typescript` is not on the Bazel Central Registry yet, so pin it from git
with `git_override`. Add to `MODULE.bazel`:

```python
bazel_dep(name = "rules_typescript", version = "0.2.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)

register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

Move the pin deliberately: pre-1.0, any commit may break the API with no
deprecation window. Every break is listed in the [changelog](changelog.md) with
the edit it requires, and the
[versioning policy](compatibility.md#versioning-policy) has the rest.

`bazel_dep` keeps its `version` attribute — bzlmod requires it and ignores the
value while an override is in place. The quickstart covers the
[`archive_override` and `local_path_override` alternatives](getting-started/quickstart.md#depending-on-rules_typescript);
the plain `bazel_dep` line resolves on its own once a version reaches the BCR.

Add to `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

No `@rules_rust` flag belongs here. `rules_rust` is a transitive dependency of
`rules_typescript`, so Bazel cannot resolve the label from your repository and
fails the invocation. See
[Troubleshooting](guides/troubleshooting.md#no-repository-visible-as-rules_rust).

Your repository root also needs a `BUILD.bazel` (empty is fine) — `rules_rust`
resolves `//:MODULE.bazel` while fetching crates, and that requires the root to
be a package.

## Quick Example

Write TypeScript. Export annotations are optional under tsgo:

```typescript
// src/math.ts
export function add(a: number, b: number) {
  return a + b;
}
```

Generate BUILD files and build:

```bash
bazel run //:gazelle
bazel build //...
```

Gazelle produces `src/BUILD.bazel`, one `ts_compile` per directory, named after
the directory. See
[Generated target names](gazelle/overview.md#generated-target-names).

```python
ts_compile(
    name = "src",
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

Windows is not supported right now; it may be considered in the future. What
runs there today: [Compatibility](compatibility.md#windows).

## Documentation

- [Quick Start](getting-started/quickstart.md) — new project or migrating an existing one
- [Isolated Declarations](getting-started/isolated-declarations.md) — the opt-in throughput mode
- [IDE Setup](getting-started/ide-setup.md) — the generated `tsconfig.json`, plus the tsserver hook for any editor that runs tsserver
- [npm Dependencies](guides/npm.md) — pnpm lockfile integration
- [Testing with vitest](guides/testing.md) — `ts_test`, the vitest config layers, coverage
- [Bundling](guides/bundling.md) — `ts_bundle` with Vite or custom bundlers
- [Dev Server](guides/dev-server.md) — Vite or oj behind one generated config
- [Tailwind v4](guides/tailwind.md) — through `vite_config`, in both bundle modes and under both dev servers
- [Monorepo Layout](guides/monorepo.md) — package boundaries and cross-package deps
- [Publishing Packages](guides/publishing.md) — `ts_npm_publish` and the `package.json` template
- [Troubleshooting](guides/troubleshooting.md) — the error messages, by message text
- [Gazelle Reference](gazelle/overview.md) — directives, package boundaries, framework detection
- [Rules Reference](rules/ts-compile.md) — all rule attributes and providers
- [Migrating from rules_ts](getting-started/migration.md) — where the other ruleset is the better choice
- [Compatibility](compatibility.md) — Bazel and platform support, the Vite/vitest versions the tests exercise, and the pre-1.0 policy

## License

MIT
