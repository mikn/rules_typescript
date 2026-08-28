# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

Rust and Go do the work: [Oxc](https://oxc.rs/) compiles, [tsgo](https://github.com/microsoft/typescript-go) type-checks. Bundling and dev serving speak one generated [Vite](https://vite.dev/) config, run by Vite or by [oj](https://github.com/raphamorim/oj). [Gazelle](https://github.com/bazelbuild/bazel-gazelle) writes the BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

Coming from an existing TypeScript repository: [Install](#install) is the short
path, and the
[Quick Start](https://mikn.github.io/rules_typescript/getting-started/quickstart/)
covers the migration questions.

**Full documentation: [mikn.github.io/rules_typescript](https://mikn.github.io/rules_typescript)**

## Built for the Vite Ecosystem

Vite bundles, and Vite or oj serves. Frameworks that ship a Vite plugin fit
either, because both read the same generated config.

- **React + Vite** — plain Vite: SPA bundle, CSS modules, and Fast Refresh HMR under `react_refresh = True`.
- **Remix** — SPA bundle **and** SSR via [`remix_build`](https://mikn.github.io/rules_typescript/rules/remix-build/). Routes get their own chunks.
- **SvelteKit** — SSR via [`sveltekit_build`](https://mikn.github.io/rules_typescript/rules/sveltekit-build/), components via [`svelte_library`](https://mikn.github.io/rules_typescript/rules/svelte-library/). Both Vite passes run: hashed chunks in `client/`, and a `server/manifest.js` route id per route directory. `svelte_library` emits either the compiler's browser or its SSR output, picked by `generate` (`"client"` by default).
- **TanStack Start** — bundle, and server functions that reach the client through a generated handler id. No dev server: its SSR module runner inlines `react/jsx-runtime` instead of externalising it against a `node_modules` tree that is a build output.
- **Solid Start** — no bundle target. `@solidjs/start` ships no Vite plugin: `defineConfig()` returns a vinxi app, which `ts_bundle`'s `vite_config` contract (a default export with a `plugins` array) cannot consume.

Where a target cannot be built, Gazelle writes none and reports why.

Non-Vite frameworks are not a priority. Next.js is the exception. `next_build`
runs the framework's own build from declared inputs;
[`next_dev_server` and `next_serve`](https://mikn.github.io/rules_typescript/rules/next-run/)
run the app from source or from that build. Both routers work, both API-route
flavours, `"use client"`/`"use server"`, middleware, CSS and static image
imports. The build action runs with the network blocked, so `next/font/google`
fails with a diagnostic naming the download; `allow_network = True` is the
opt-out. See
[next_build](https://mikn.github.io/rules_typescript/rules/next-build/).

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo type-checks** — Go port of TypeScript, and it emits the declarations too, so unmodified TypeScript compiles: no export annotations required, and the `.d.ts` are what `tsc` would produce. Type errors fail `bazel build`.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **The dev server is swappable** — `ts_dev_server(server = ...)` takes any target providing `DevServerInfo`. Vite is the default; `@rules_typescript//oj:dev_server` selects [oj](https://github.com/raphamorim/oj), a Rust-native server that adopts the same generated Vite config and needs no `@npm//:vite` in the tree. What each server does not read is declared in its provider, so a target depending on a field its server ignores fails at analysis time naming both.
- **Isolated declarations** — annotate a package's exports and set `declarations = "oxc"`, and Oxc emits its `.d.ts` syntactically, which moves type-checking off the critical path and shortens a deep dependency chain substantially. Opt-in, per package — see [Cost of each mode](https://mikn.github.io/rules_typescript/rules/ts-compile/#cost-of-each-mode).
- **Gazelle generates BUILD files** — infers targets from the directory tree, resolves imports to labels, generates lint, bundler and dev-server targets, and takes eleven `# gazelle:ts_*` directives. It regenerates the attributes it owns on every run and names every value it drops, so a value it cannot derive needs `# keep` — see [Attributes Gazelle owns](https://mikn.github.io/rules_typescript/gazelle/directives/#attributes-gazelle-owns).
- **CSS modules** — `css_module` runs postcss-modules once, generates the `.d.ts` and the scoped-name map from that result, and hands the map to Vite. `styles.button` type-checks against the keys the stylesheet exports, and the class name in a test is the one in the bundle — see [CSS and assets](https://mikn.github.io/rules_typescript/rules/css-and-assets/).
- **Direct dependencies** — a source may import only what a direct dep provides. A declaration arriving through another dep's own deps does not satisfy an import: the build fails naming the file, the specifier and the label to add, and `bazel run //:gazelle` writes it.
- **How npm packages are fetched** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub, so a target fetches only its own dependency closure. A generated `node_modules` tree holds every resolution that closure made — name, version and peer set — flat where a name resolved once, keyed by resolution where it did not.
- **Zero prerequisites** — only Bazelisk needed; Node.js, Go and Rust are fetched hermetically, and [pnpm too](https://mikn.github.io/rules_typescript/guides/npm/#hermetic-pnpm) if you want it. pnpm edits the lockfile; a build never needs it.

## Requirements

The only prerequisite is **Bazelisk** (or Bazel 9+). Everything else — the Rust
toolchain, Go toolchain, Node.js runtime, and the npm packages your targets
actually reach — is fetched hermetically. The first build compiles `oxc-bazel`
from Rust source, the slow part; everything after that is cached.

Supported platforms: Linux x86_64, Linux ARM64, macOS x86_64, macOS ARM64.
**Windows is not supported right now. It may be considered in the future.** See
[COMPATIBILITY.md](COMPATIBILITY.md#windows).

**Nothing has shipped yet.** There is no tag, no release, no Bazel Central
Registry entry and no production users. Pre-1.0, any commit may break the API
with no deprecation window. Every break is listed in
[CHANGELOG.md](CHANGELOG.md) with the edit it requires; read it before moving a
pin. Full policy:
[COMPATIBILITY.md](COMPATIBILITY.md#versioning-policy).

Vite and vitest are your dependencies, not the ruleset's: they come from your
own lockfile, and the rules generate configuration for whichever version it
resolves to. The versions the tests exercise, and the places a generated config
is version-sensitive, are in
[COMPATIBILITY.md](COMPATIBILITY.md#vite-and-vitest).

## Install

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create an empty `WORKSPACE.bazel` (optional — `MODULE.bazel`
marks the root in Bazel 9; every workspace in this repo carries one).

**Step 3.** Add to `MODULE.bazel`. The ruleset is not on the Bazel Central
Registry yet, so `bazel_dep` alone has nothing to resolve against; pin it from
git:

```python
module(name = "my_project", version = "0.0.0")

bazel_dep(name = "rules_typescript", version = "0.2.0")
git_override(
    module_name = "rules_typescript",
    remote = "https://github.com/mikn/rules_typescript.git",
    commit = "REPLACE_WITH_A_COMMIT_SHA_FROM_MAIN",
)
register_toolchains("@rules_typescript//ts/toolchain:all")

bazel_dep(name = "gazelle", version = "0.47.0")
```

Pin a full commit SHA, not a branch. bzlmod still requires `version` on
`bazel_dep` and ignores its value while the override is active. The
`archive_override` (smaller fetch) and `local_path_override` forms are in
[Depending on rules_typescript](https://mikn.github.io/rules_typescript/getting-started/quickstart/#depending-on-rules_typescript).

**Step 4.** Add to `.bazelrc`:

```
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
```

Those three lines are the whole file. Do not add an `@rules_rust` flag:
`rules_rust` is a transitive dependency of `rules_typescript`, not of your
module, so Bazel cannot resolve the label and rejects the invocation with
`No repository visible as '@rules_rust' from main repository`.

**Step 5.** Add to `BUILD.bazel` at the repository root. The file has to exist
even if empty: `rules_rust` resolves `//:MODULE.bazel` while fetching crates,
which requires the root to be a Bazel package:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write TypeScript. Export annotations are optional: tsgo emits the
declarations from the full type program, so an inferred return type is fine:

```typescript
export function add(a: number, b: number) {
  return a + b;
}
```

**Step 7.** Generate BUILD files, build, and test:

```bash
bazel run //:gazelle
bazel build //...
bazel test //...
```

### Adding npm Dependencies

One-time setup in `MODULE.bazel`:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm", "pnpm")
```

Take `"pnpm"` even if you never run pnpm through Bazel: Gazelle writes `ts_pnpm`
and `ts_add_package` targets into your root `BUILD.bazel` as soon as a lockfile
exists, and without that repo `bazel build //...` aborts with
`No repository visible as '@pnpm' from main repository`.

Then, per package:

```bash
pnpm add zod --lockfile-only   # updates pnpm-lock.yaml, no node_modules created
bazel run //:gazelle           # picks up new package, updates BUILD files
bazel build //...              # fetches just that package's closure, builds
```

Bazel fetches a package the first time a target needs it. No `node_modules/`
directory ever exists in the source tree; the lockfile is the only npm artifact
in git.

`bazel run //:pnpm -- add zod --lockfile-only` uses a hermetic pnpm —
[two lines of setup](https://mikn.github.io/rules_typescript/guides/npm/#hermetic-pnpm).

## IDE Integration

`ts_refresh_tsconfig` writes the workspace-root `tsconfig.json` from Bazel's
build graph: source roots, path aliases, and one `compilerOptions.paths` entry
per npm package your targets reach that ships declarations, pointing at the
copies it installs under `.bazel/npm`. The file is meant to be checked in, and
`test = True` adds a test that fails once it goes stale. A tsserver hook is installed alongside it for
editors that resolve live, and works with anything running tsserver.

```python
# BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_refresh_tsconfig")

ts_refresh_tsconfig(
    name = "refresh_tsconfig",
    test = True,
    deps = [
        "//apps/web",
        "//packages/ui",
    ],
)
```

`deps` is the whole input. An aspect walks it, so listing a target covers
everything it depends on; the default, `deps = []`, writes an empty `paths`. It
obeys visibility, so a package-private target cannot be listed.

```bash
bazel run //:refresh_tsconfig        # writes tsconfig.json, .bazel/npm/, and the hook
bazel test //:refresh_tsconfig_test  # fails when the checked-in tsconfig is stale
```

Then add to VS Code settings: `"typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"`

`nested_tsconfigs` lists the packages that need their own editor program, as
workspace-relative paths to the `tsconfig.json` each one gets. A package belongs
there when its targets set `compilerOptions` the root block cannot also be set
to. The list is declared, not discovered, and the rule **fails at analysis time
when it disagrees with the graph in either direction** — so a repository with one
such package fails the snippet above until the list is filled in. That attribute,
`extra_exclude`, `npm_dir` and the other editors are in
**[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)**.

## Documentation

- **[Quick Start](https://mikn.github.io/rules_typescript/getting-started/quickstart/)** — new project or migrating an existing codebase
- **[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)** — a generated `tsconfig.json` plus live tsserver resolution from Bazel's build graph (TypeScript's GOPACKAGESDRIVER)
- **[Isolated Declarations](https://mikn.github.io/rules_typescript/getting-started/isolated-declarations/)** — the opt-in throughput mode
- **[npm Dependencies](https://mikn.github.io/rules_typescript/guides/npm/)** — pnpm lockfile integration, platform-specific packages, bin scripts
- **[Testing with vitest](https://mikn.github.io/rules_typescript/guides/testing/)** — `ts_test`, snapshots, sharding, watch mode with ibazel
- **[Bundling](https://mikn.github.io/rules_typescript/guides/bundling/)** — `ts_bundle` with Vite or any `BundlerInfo`-compatible bundler
- **[Dev Server](https://mikn.github.io/rules_typescript/guides/dev-server/)** — a pluggable dev server with ibazel HMR: Vite by default, oj through `server = "@rules_typescript//oj:dev_server"`, one generated config driving either
- **[Monorepo Layout](https://mikn.github.io/rules_typescript/guides/monorepo/)** — package boundaries, cross-package `.d.ts` caching
- **[Gazelle Reference](https://mikn.github.io/rules_typescript/gazelle/overview/)** — directives, framework detection, auto-detected lint and codegen targets
- **[Rules Reference](https://mikn.github.io/rules_typescript/rules/ts-compile/)** — all attributes, providers, and outputs
- **[Migration from rules_ts](https://mikn.github.io/rules_typescript/getting-started/migration/)** — differences from aspect-build/rules_ts
- **[Troubleshooting](https://mikn.github.io/rules_typescript/guides/troubleshooting/)** — the error messages, by message text
- **[Compatibility](https://mikn.github.io/rules_typescript/compatibility/)** — Bazel and platform support, the Vite/vitest versions the tests exercise, and what "pre-1.0" means here

## License

MIT
