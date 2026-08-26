# rules_typescript

An opinionated Bazel ruleset for TypeScript, optimised for the **Oxc + Vite** toolchain rather than broad compatibility with every JS build tool. If your stack is TypeScript, Vite, and a Vite-based framework — this replaces `tsc`, your bundler, and your dev server with a single hermetic build. If you need `tsc` compatibility or non-Vite toolchains, see [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

[Oxc](https://oxc.rs/) compiles. [tsgo](https://github.com/microsoft/typescript-go) type-checks. [Vite](https://vite.dev/) bundles. [Gazelle](https://github.com/bazelbuild/bazel-gazelle) generates BUILD files. Write `.ts`, run Gazelle, `bazel build //...`. No `node_modules/`. No system Node. Just Bazelisk.

**Full documentation: [mikn.github.io/rules_typescript](https://mikn.github.io/rules_typescript)**

## Built for the Vite Ecosystem

This ruleset is designed around **Vite** as the bundler and dev server. A
framework that ships a Vite plugin fits, with the amount of proof varying:

- **React + Vite** — SPA bundling, React Fast Refresh HMR, CSS modules (`examples/react-app`)
- **TanStack Start** — the SPA bundle builds in CI (`examples/tanstack-app`, plus an integration test); the app-mode bundle that runs `tanstackStart()` through `vite_config` is excluded from CI with the blocker named in the workflow ([why](https://mikn.github.io/rules_typescript/guides/bundling/#framework-plugins-via-vite_config))
- **Remix** — client bundle with route-based code splitting via the `@remix-run/dev` Vite plugin. An integration test runs Gazelle over a fresh Remix workspace, builds what it wrote, and asserts a chunk per route (`//tests/integration:remix_test`, plus `examples/remix-app`)
- **SvelteKit, Solid Start** — detected, and deliberately given **no** bundle target. Gazelle names the framework and says bundling it is unsupported, instead of writing a `ts_bundle` that cannot build; your TypeScript still compiles and tests ([the reason for each](https://mikn.github.io/rules_typescript/gazelle/overview/#framework-detection))

Frameworks that don't use Vite are not a priority. Next.js is the exception with
a rule of its own — `next_build` runs the framework's own build
(`examples/nextjs-app`, `//tests/integration:nextjs_test`).

## Key Ideas

- **Oxc compiles** — Rust-based TypeScript/JSX transformer. `.js` + `.js.map` per file, hundreds of files in milliseconds, and `.d.ts` too under `declarations = "oxc"`.
- **tsgo emits declarations and type-checks** — Go port of TypeScript. Unmodified TypeScript compiles: no explicit export annotations required, and the `.d.ts` are what `tsc` would produce. Type errors fail `bazel build` because the declarations are real outputs.
- **Vite bundles** — production bundles with tree-shaking, code splitting, minification. App mode (HTML + hashed assets) and lib mode.
- **Isolated declarations, when you want them** — annotate a package's exports and set `declarations = "oxc"` to have Oxc emit its `.d.ts` syntactically. Type-checking then moves off the critical path, which on a deep dependency chain shortens it substantially ([measured](https://mikn.github.io/rules_typescript/rules/ts-compile/#cost-of-each-mode)). Opt-in, per package.
- **Gazelle generates BUILD files** — infers targets from the directory tree, resolves imports to labels, and generates lint, bundler and dev-server targets. Nine `# gazelle:ts_*` directives configure it.
- **Deps are what you declared** — a source may import only what a *direct* dep provides. A declaration that reaches a target through another dep's own deps no longer satisfies an import: the build fails naming the file, the specifier and the label to add, and `bazel run //:gazelle` writes it.
- **npm without a store** — one Bazel repository per package, fetched on demand, behind a `@npm` alias hub. A target's npm cost is its own dependency closure, not the whole lockfile. A `node_modules` tree places every *resolution* a closure made — name, version and peer set — not one directory per name.
- **Only Bazelisk required** — Node.js, Go and Rust are fetched hermetically, and [pnpm too](https://mikn.github.io/rules_typescript/guides/npm/#hermetic-pnpm) if you want it. pnpm is needed only to edit the lockfile, never to build.

## Requirements

The only prerequisite is **Bazelisk** (or Bazel 9+). Everything else — the Rust
toolchain, Go toolchain, Node.js runtime, and the npm packages your targets
actually reach — is fetched hermetically. The first build also compiles
`oxc-bazel` from Rust source, which is the slow part; everything after it is
cached.

Supported platforms: Linux x86_64, Linux ARM64, macOS x86_64, macOS ARM64.
**Windows is not supported**, and the reason is upstream: no tsgo binary and no
hermetic pnpm binary are published for it. A few of our own build actions still
run through a bash wrapper too. See
[COMPATIBILITY.md](COMPATIBILITY.md#windows).

Vite and vitest are your dependencies, not the ruleset's — they come from your
own lockfile, and the rules generate configuration for whichever version that
resolves to. Which versions the repository's own tests exercise, and where a
generated config is version-sensitive, is in
[COMPATIBILITY.md](COMPATIBILITY.md#vite-and-vitest).

## Install

**Step 1.** Create `.bazelversion`:

```
9.0.0
```

**Step 2.** Create `WORKSPACE.bazel` (empty — required by Bazel 9).

**Step 3.** Add to `MODULE.bazel`. The ruleset is not on the Bazel Central
Registry yet, so pin it from git — `bazel_dep` alone has nothing to resolve
against:

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
`bazel_dep` and ignores its value while the override is active. See
[Depending on rules_typescript](https://mikn.github.io/rules_typescript/getting-started/quickstart/#depending-on-rules_typescript)
for the `archive_override` (smaller fetch) and `local_path_override` forms.

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
even if empty — `rules_rust` resolves `//:MODULE.bazel` while fetching crates,
which requires the root to be a Bazel package:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
```

**Step 6.** Write TypeScript. Export annotations are optional — tsgo emits the
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

### Adding npm dependencies

One-time setup in `MODULE.bazel`:

```python
npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
```

Then, per package:

```bash
pnpm add zod --lockfile-only   # updates pnpm-lock.yaml, no node_modules created
bazel run //:gazelle           # picks up new package, updates BUILD files
bazel build //...              # fetches just that package's closure, builds
```

The extension declares one Bazel repository per package in the lockfile and a
`@npm` hub of aliases pointing at them, so Bazel fetches a package the first
time a target needs it rather than downloading the lockfile up front. No
`node_modules/` directory ever exists in the source tree; the lockfile is the
only npm artifact in git.

Don't want pnpm installed? `bazel run //:pnpm -- add zod --lockfile-only` uses a
hermetic one — [two lines of setup](https://mikn.github.io/rules_typescript/guides/npm/#hermetic-pnpm).

## IDE Integration

`ts_refresh_tsconfig` writes the workspace-root `tsconfig.json` from Bazel's
build graph: source roots, path aliases, and one `compilerOptions.paths` entry
per npm package your targets reach, pointing at declarations it installs under
`.bazel/npm`. Those `paths` entries are the mechanism — you don't maintain them,
but the file is meant to be checked in, and `test = True` adds a test that fails
once it goes stale. A tsserver hook is installed alongside it for editors that would
rather resolve live than reload a tsconfig; it works with VS Code, Neovim,
Emacs, anything running tsserver.

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

`deps` is the whole input. An aspect walks `deps` from each entry, so listing a
target covers everything it depends on — and the attribute default, `deps = []`,
reaches nothing and writes a `tsconfig.json` with an empty `paths`. `deps` also
obeys visibility, so a package-private `ts_compile` target cannot be listed
(Gazelle writes `//visibility:public` on the targets it generates, and so does
`ts_test` for the targets it generates from your sources). A target's
`module_name` gets its own `paths` entry, so a first-party package imported by
bare specifier resolves in the editor too.

`extra_exclude` adds globs to the generated `exclude`. Reach for it when the
repository holds TypeScript that is not in this module's build graph — a nested
Bazel module, a workspace in `.bazelignore` — since nothing in `deps` names
those files and `tsc` would otherwise walk them under the wrong
`compilerOptions`.

```bash
bazel run //:refresh_tsconfig        # writes tsconfig.json, .bazel/npm/, and the hook
bazel test //:refresh_tsconfig_test  # fails when the checked-in tsconfig is stale
```

Then add to VS Code settings: `"typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"`

See **[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)** for all editors, and for `npm_dir`.

## Feature Highlights

- **[Quick Start](https://mikn.github.io/rules_typescript/getting-started/quickstart/)** — new project or migrating an existing codebase
- **[IDE Setup](https://mikn.github.io/rules_typescript/getting-started/ide-setup/)** — a generated `tsconfig.json` plus live tsserver resolution from Bazel's build graph (TypeScript's GOPACKAGESDRIVER)
- **[Isolated Declarations](https://mikn.github.io/rules_typescript/getting-started/isolated-declarations/)** — the opt-in throughput mode, and what it does and does not buy
- **[npm Dependencies](https://mikn.github.io/rules_typescript/guides/npm/)** — pnpm lockfile integration, platform-specific packages, bin scripts
- **[Testing with vitest](https://mikn.github.io/rules_typescript/guides/testing/)** — `ts_test`, snapshots, sharding, watch mode with ibazel
- **[Bundling](https://mikn.github.io/rules_typescript/guides/bundling/)** — `ts_bundle` with Vite or any `BundlerInfo`-compatible bundler
- **[Dev Server](https://mikn.github.io/rules_typescript/guides/dev-server/)** — Vite dev server with ibazel HMR
- **[Monorepo Layout](https://mikn.github.io/rules_typescript/guides/monorepo/)** — package boundaries, cross-package `.d.ts` caching
- **[Gazelle Reference](https://mikn.github.io/rules_typescript/gazelle/overview/)** — directives, framework detection, auto-detected lint and codegen targets
- **[Rules Reference](https://mikn.github.io/rules_typescript/rules/ts-compile/)** — all attributes, providers, and outputs
- **[Migration from rules_ts](https://mikn.github.io/rules_typescript/getting-started/migration/)** — differences from aspect-build/rules_ts

## License

MIT
