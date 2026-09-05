# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain. For a stack of TypeScript and Vite, it replaces `tsc` and the dev server with a single hermetic build. For `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts) ([comparison](getting-started/migration.md)).

Rust and Go do the work: [Oxc](https://oxc.rs/) compiles, [tsgo](https://github.com/microsoft/typescript-go) type-checks. The dev server runs one generated [Vite](https://vite.dev/) config. [Gazelle](https://github.com/bazelbuild/bazel-gazelle) writes the BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

Coming from an existing TypeScript monorepo, the
[Quick Start](getting-started/quickstart.md) is the whole path: four root files,
then `bazel run //:gazelle`. [Install](#install) and
[Quick Example](#quick-example) below are the short version.

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo emits declarations and type-checks** — Go port of TypeScript, and the default emitter. Unmodified TypeScript compiles: no export annotations required, and the `.d.ts` are what `tsc` would produce. tsgo runs as a build action, not a separate `tsc --noEmit` job, so type errors fail `bazel build`; the declarations are real outputs, and a package type-checks against what its dependency emits.
- **The dev server is swappable** — `ts_dev_server(server = ...)` takes any target providing `DevServerInfo`. Vite is the default. Each server declares the config fields it does not read, so a target depending on one fails at analysis time naming the field and the server. `ts_dev_server` hands the source tree to the server; HMR is the server's, not a rebuild. See [Bringing your own server](guides/dev-server.md#bringing-your-own-server).
- **Isolated declarations** — annotate a package's exports, set `declarations = "oxc"`, and Oxc emits the `.d.ts` syntactically. Type-checking leaves the critical path, which shortens a deep dependency chain substantially. Opt-in, per package. See [Cost of each mode](rules/ts-compile.md#cost-of-each-mode).
- **Gazelle generates the BUILD files** — targets inferred from the directory tree, imports resolved to labels, lint targets generated, codegen auto-detected. Fifteen `# gazelle:ts_*` directives configure it.
- **Direct dependencies** — a source may import only what a direct dep provides. A declaration arriving through another dep's own deps does not satisfy an import; the build names the file, the specifier and the label to add, and Gazelle writes it.
- **How npm packages are fetched** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub. A target's npm cost is its own closure, not the whole lockfile, and the rules read no `node_modules/` from the source tree. A materialised tree carries one entry per resolution (name, version and peer set), not one directory per name.
- **The editor reads a generated `tsconfig.json`** — `ts_refresh_tsconfig` writes a checked-in `tsconfig.json` out of the build graph, so tsserver, a plain `tsc` run and a coding agent's language server resolve what Bazel resolves. See [IDE Setup](getting-started/ide-setup.md).
- **Only Bazelisk required** — Node.js, Go and Rust are fetched hermetically, and [pnpm too](guides/npm.md#hermetic-pnpm) if you want it. pnpm is needed only to edit the lockfile, never to build. The first build compiles `oxc-bazel` from Rust source, which dominates the wall time.

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

Pre-1.0, any commit may break the API with no deprecation window. Every break
is listed in the [changelog](changelog.md) with the edit it requires; the
[versioning policy](compatibility.md#versioning-policy) has the rest.

`bazel_dep` keeps its `version` attribute: bzlmod requires it and ignores the
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

Your repository root also needs a `BUILD.bazel` (empty is fine): `rules_rust`
resolves `//:MODULE.bazel` while fetching crates, which requires the root to be
a package.

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
- [IDE Setup](getting-started/ide-setup.md) — the generated `tsconfig.json`, the tsserver plugin, and what a coding agent's language server needs
- [npm Dependencies](guides/npm.md) — pnpm lockfile integration
- [Testing with vitest](guides/testing.md) — `ts_test`, the vitest config layers, coverage
- [Bundling](guides/bundling.md) — `ts_binary` with a `BundlerInfo` bundler
- [Dev Server](guides/dev-server.md) — Vite behind one generated config
- [Tailwind v4](guides/tailwind.md) — through `vite_config`, under the dev server
- [Monorepo Layout](guides/monorepo.md) — package boundaries and cross-package deps
- [Troubleshooting](guides/troubleshooting.md) — the error messages, by message text
- [Gazelle Reference](gazelle/overview.md) — directives, package boundaries, codegen detection
- [Rules Reference](rules/ts-compile.md) — all rule attributes and providers
- [Migrating from rules_ts](getting-started/migration.md) — where the other ruleset is the better choice
- [Compatibility](compatibility.md) — Bazel and platform support, the Vite/vitest versions the tests exercise, and the pre-1.0 policy

## License

MIT
