# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts) ([comparison](getting-started/migration.md)).

[Oxc](https://oxc.rs/) compiles. [tsgo](https://github.com/microsoft/typescript-go) type-checks. [Vite](https://vite.dev/) bundles. [Gazelle](https://github.com/bazelbuild/bazel-gazelle) generates BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

**Have a TypeScript monorepo and want a target building?** Go straight to the
[Quick Start](getting-started/quickstart.md) — five root files, then
`bazel run //:gazelle`. Nothing on this page or in the rules reference is needed
first. [Install](#install) and [Quick Example](#quick-example) below are the same
path in miniature.

## Built for the Vite Ecosystem

Vite is the bundler and the dev server. A framework that ships a Vite plugin is
expressible: `vite_config` names the config file, `vite_config_srcs` the local
modules it imports, and the plugins it exports run before Bazel's
([bundling](guides/bundling.md#framework-plugins-via-vite_config)). What that
config may *not* be is a program — only the keys the generated config reads reach
the build, and any other key fails the build naming itself.

| Framework | Gazelle generates a bundle target? | Evidence in this repo |
|---|---|---|
| **React + Vite** | n/a — plain Vite, no framework plugin | `examples/react-app`: SPA bundling, React Fast Refresh HMR, CSS modules |
| **TanStack Start** | yes | `examples/tanstack-app` (SPA target), `//tests/integration:vite_bundle_test`. The app-mode target that runs `tanstackStart()` is excluded from CI |
| **Remix** | yes | `//tests/integration:remix_test` — Gazelle over a fresh workspace, then a build of what it wrote, asserting one chunk per route. Plus `examples/remix-app` |
| **SvelteKit** | **no**, by decision | Gazelle names the framework and the reason instead |
| **Solid Start** | **no**, by decision | Gazelle names the framework and the reason instead |

For the one that gets no target, that is the whole support statement: a
`ts_bundle` nothing can build is worse than none, and silence is worse than
both, so Gazelle logs which framework it saw and why bundling it is
unsupported. The rest of the workspace still compiles and tests. The reason,
and what a client-only build would take instead, is in
[Framework detection](gazelle/overview.md#framework-detection).

`examples/` is in `.bazelignore`, so the example workspaces are separate Bazel
invocations; CI builds all six of them in full.

Frameworks that don't use Vite are not a priority. Next.js is the exception with
a rule of its own — `next_build` runs the framework's own build, and
`next_dev_server` and `next_serve` run the app from source or from that build
(`examples/nextjs-app`, `//tests/integration:nextjs_test`).

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, hundreds of files in milliseconds, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo emits declarations and type-checks** — Go port of TypeScript. Unmodified TypeScript compiles: no explicit export annotations required, and the `.d.ts` are what `tsc` would produce. Type errors fail `bazel build` because the declarations are real outputs.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **Isolated declarations, when you want them** — annotate a package's exports and set `declarations = "oxc"` to have Oxc emit its `.d.ts` syntactically. Type-checking then moves off the critical path, which on a deep dependency chain shortens it substantially ([measured](rules/ts-compile.md#cost-of-each-mode)). Opt-in, per package.
- **Gazelle generates the BUILD files** — targets inferred from the directory tree, imports resolved to labels, lint / bundler / dev-server targets generated, frameworks and codegen auto-detected. Ten `# gazelle:ts_*` directives configure it.
- **Deps are what you declared** — a source may import only what a *direct* dep provides. A declaration arriving through another dep's own deps no longer satisfies an import; the build fails naming the file, the specifier and the label to add, and Gazelle writes it.
- **npm without a store** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub. A target's npm cost is its own dependency closure, not the whole lockfile, and no `node_modules/` exists in the source tree. A `node_modules` tree places every *resolution* a closure made — name, version and peer set — not one directory per name.
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

Pin a commit, and expect to move it deliberately: pre-1.0 any commit may break
the API with no deprecation window, and every break is listed in the
[changelog](changelog.md) with the edit it requires
([versioning policy](compatibility.md#versioning-policy)).

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

Write TypeScript. Export annotations are optional — the default emitter is tsgo,
which infers them from the full type program:

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

Gazelle produces `src/BUILD.bazel`. The target is named after the **directory**,
not the file — one `ts_compile` per directory, the way one Go package is one
directory ([naming](gazelle/overview.md#generated-target-names)):

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

Windows is not supported right now. It may be considered in the future. What runs there today:
[Compatibility](compatibility.md#windows).

## Documentation

- [Quick Start](getting-started/quickstart.md) — new project or migrating an existing one
- [Isolated Declarations](getting-started/isolated-declarations.md) — the opt-in throughput mode
- [IDE Setup](getting-started/ide-setup.md) — the live tsserver resolution hook, for any editor that runs tsserver
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
- [Compatibility](compatibility.md) — Bazel and platform support, the Vite/vitest versions the tests exercise, and what "pre-1.0" means here

## License

MIT
