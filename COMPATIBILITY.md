# Compatibility

## Bazel Versions

| Bazel Version | Support Level |
|---------------|--------------|
| 9.x | Fully supported (the only version CI runs) |
| 8.x | Untested (bzlmod is available; nothing verifies it) |
| 7.x | Untested |
| < 7.0 | Not supported (no bzlmod) |

rules_typescript requires bzlmod (MODULE.bazel). WORKSPACE-based setups are not
supported, and no workspace here carries a `WORKSPACE.bazel`: Bazel 8 made the
file optional and Bazel 9 stopped reading it, so `MODULE.bazel` alone marks a
repository root. `.bazelversion` in this repository pins 9.2.0 and CI installs
Bazelisk against it.

## Platforms

| Platform | Status |
|----------|--------|
| Linux x86_64 | Supported |
| Linux ARM64 | Supported |
| macOS x86_64 | Supported |
| macOS ARM64 | Supported |
| Windows x86_64 | **Not supported** |

CI runs `ubuntu-latest` and `macos-latest`. Linux ARM64 and macOS x86_64 have
toolchains for every tool but no CI coverage.

### musl

Only glibc linux is supported. `NODE_PLATFORMS` (`ts/private/runtime.bzl`),
`TSGO_PLATFORMS` (`ts/private/toolchain.bzl`) and `_PNPM_PLATFORMS`
(`ts/private/pnpm.bzl`) enumerate the platform vocabulary, all glibc, and
`//platforms` has no musl key. Node.js publishes no official musl tarball, so
there is nothing to register.

A `libc: [musl]` tarball in `pnpm-lock.yaml` therefore matches no platform, and
the `npm` extension drops it without declaring a repository for it — the same
path a tarball with `cpu: [ppc64]` or `os: [aix]` already takes. It is never
fetched, never extracted, and never staged into an action.

On a musl host the Node the ruleset downloads is still the glibc build.

### Windows

Windows is not supported right now. It may be considered in the future.

What exists there today: a registered Node.js toolchain, a `windows_amd64` entry
in `//platforms`, and a `node_modules` tree action driven by a cross-platform
Node script with no shell dependency. That builds a `node_modules` directory and
nothing else.

Support would take a Windows entry in `TSGO_PLATFORMS`
(`ts/private/toolchain.bzl`) and `_PNPM_PLATFORMS` (`ts/private/pnpm.bzl`), and
replacements for the build-action wrappers that still need a POSIX shell: the
Vite bundler, the framework build rules (`next_build`, `remix_build`,
`sveltekit_build`), and the `node_modules` fallback taken when no JS runtime
toolchain is registered. oxc needs no entry: `oxc-bazel` is built from source by
rules_rust for whichever exec platform the build runs on, so one toolchain
covers every platform. None of this has been run on Windows, so any estimate of
the remaining work is untested.

If you need TypeScript on Bazel on Windows today, use
[aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

## Vite and vitest

Neither is a dependency of this ruleset. Both come from your `pnpm-lock.yaml`,
and `ts_bundle`, `ts_dev_server` and `ts_test` generate configuration for
whatever version that resolves to. "Supported" here means a test in this
repository exercises that version; nothing constrains what you pin.

There is **one lane** — one Vite version and one vitest version. The workspace
translates several lockfiles; four of them resolve one or both tools, and they
agree on the version, so no test runs a generated config against a second major:

| Hub | Lockfile | Vite | vitest | Coverage |
|---|---|---|---|---|
| `@npm` | `tests/npm/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | `ts_test` (the whole `tests/vitest` suite), `ts_dev_server` (seven servers started for real and interrogated over HTTP, six under Vite and one under oj), `ts_bundle` output (`tests/vite_bundle`), `vite-plugin-bazel`'s own tests, and the `lsp`, `npm_deps` and `vite_bundle` integration workspaces |
| `@npm_tailwind` | `tests/tailwind/pnpm-lock.yaml` | 8.2.2 | — | Tailwind v4 through `vite_config`: app mode, lib mode, and the dev server under both implementations |
| `@npm_workers` | `tests/workers/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | `ts_test` with the Workers pool (vitest inside workerd), `ts_worker_dry_run_test`, `ts_worker_deploy` |
| `@npm_eslint` | `tests/eslint/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | the ESLint plugin's own `ts_test` target, against `@typescript-eslint`'s rule tester |
| `@npm_features` | `tests/npm/pnpm-lock-features.yaml` | — | — | pnpm's patched dependencies, npm aliases, peer-dependency variants, per-importer resolution; resolves neither tool |
| `@npm_css` | `ts/private/css/pnpm-lock.yaml` | — | — | the packages the ruleset's own build actions run: postcss 8.5.26 and postcss-modules 9.0.1 for `css_module`'s compiler, and the esbuild that bundles `vite-plugin-bazel`. The one hub here that is **not** a fixture — both bundles ship to consumers as API |

The `examples/` modules and the `svelte` and `sveltekit` integration workspaces
are separate Bazel modules with their own lockfiles, outside the table above.
Five of the six examples resolve Vite 8.2.2 and vitest 4.1.11; `examples/basic`
has no npm dependencies. Four integration workspaces copy an example's lockfile
in at test time: `nextjs` from `examples/nextjs-app`, `remix` and `remix_ssr`
from `examples/remix-app`, `tanstack` from `examples/tanstack-app`.

`@npm_css` is a compatibility surface of its own. `css_module` derives the class
names and the `.d.ts` with its own postcss-modules; the bundler reproduces them
with the CSS-modules implementation built into your Vite. The naming function is
handed to Vite, so only a divergence in what counts as a local name can split
the two, and the plugin then errors with both sides named.

To re-derive the table from the repository:

```bash
grep -rnE '^  (vite|vitest)@' --include=pnpm-lock.yaml .
bazel query 'filter("behaviour_test$", tests(//tests/dev_server/...))'
```

No hub carries a second major. One importer in the tree resolves Vite 7: the
SvelteKit integration workspace (`tests/integration/sveltekit/workspace`, Vite
7.1.5). It builds through a hand-written config, so no generated config runs
against a Vite 7. The grep prints two further majors, `vite@7.3.1` and
`vite@5.4.21` in `examples/remix-app/pnpm-lock.yaml`. Both arrive transitively
under `@remix-run/dev`, through `vite-node` and `@vanilla-extract/integration`.
That example's own `vite` is 8.2.2, and its generated configs get that one.

The Vite that `vite-plugin-bazel` declares a peer range for and the Vite the
ruleset installs are held together by `//vite/tests:peer_version_test`, which
reads `peerDependencies.vite` out of `vite/package.json` and asserts the
installed major is one that range names.

The two places a generated config is known to be version-sensitive:

- **`ts_bundle`** emits `build.rollupOptions.output.manualChunks` for
  `split_chunks`, and `minify = True` emits `true` without naming a minifier.
  Both spellings are honoured by every generation from 6 onward. The
  vendor-splitting plugin `split_chunks` used to emit was removed in Vite 7, and
  naming `esbuild` picks a minifier that is an optional peer, absent from a tree
  built from `deps = ["@npm//:vite"]`. `minify = False` also pins
  `output.minify: false`: the dead-code pass otherwise re-emits each chunk from
  its AST and discards what a plugin's `renderChunk` returned.
- **`ts_test`** reads a `config` file that default-exports an array as a list of
  vitest projects and emits `test.projects`. That option is vitest 3.2 and
  later. `test.workspace`, the name it replaced, was removed in vitest 4, which
  throws on it.

## oj

oj is the second `ts_dev_server` implementation, and unlike Vite it comes from
this ruleset rather than from your lockfile. It publishes no npm package and no
release binary, so cargo is the only channel to pin it from: `MODULE.bazel`'s
`oj_crates` extension pins the crate at `=0.1.6` and rules_rust builds it from
source. The first build of a target selecting oj is a Rust compile.

What tests it:

| Target | What it covers |
|---|---|
| `//tests/dev_server:dev_oj_behaviour_test` | the assertions the six Vite lanes make, against the same generated config |
| `//tests/dev_server:dev_oj_css_module_test` | a served `*.module.css` carrying the class names the `.d.ts` was generated from |
| `//tests/dev_server:dev_oj_hmr_latency_test` | edit-to-HMR, over oj's own `/__ws` socket |
| `//tests/tailwind:tailwind_dev_oj_test` | `@tailwindcss/vite`, a Vite-API plugin, in oj's plugin host |

oj is not a bundler here. Nothing in the ruleset returns `BundlerInfo` for it.
The oj revision this module pins gives `oj build` the `--config` flag `oj dev`
already had, so a generated config can now name itself to a build, but
`oj build`'s CLI still matches neither `BundlerInfo` invocation mode.

## Versioning Policy

This project follows [Semantic Versioning 2.0.0](https://semver.org/) from 1.0
onward. Nothing has shipped yet: `MODULE.bazel` reads 0.2.0, but there is no
tag, no release and no Bazel Central Registry entry, and consumers pin a commit.

**Pre-1.0 (current):** any commit may break the API, with no deprecation
window and no compatibility shim. Breaks are listed in
[CHANGELOG.md](https://github.com/mikn/rules_typescript/blob/main/CHANGELOG.md)
with the edit each one requires. `ts_compile`, `ts_test`, the npm extension
and the toolchain API have all broken pre-1.0; read the changelog before bumping
a pin.

**Post-1.0 (future):** major versions for breaking changes, minor for features,
patch for fixes.

## Public API Surface

Everything is unstable pre-1.0. The split below ranks how likely a thing is to
move.

### Load-bearing

Breaks get a changelog entry with the required edit.

- `ts_compile`, `ts_test`, `ts_binary`, `ts_bundle`, `ts_config`,
  `node_modules` and `ts_refresh_tsconfig` rules and their documented
  attributes
- `JsInfo`, `TsDeclarationInfo`, `TsModuleInfo`, `BundlerInfo`, `CssInfo`,
  `CssModuleInfo`, `AssetInfo`, `NpmPublishInfo`, `TsLintInfo` providers
- The `npm` module extension (`npm.translate_lock`, `npm.pnpm`) and the `@npm`
  label surface (`@npm//:zod`, `@npm//:types_react`, `@npm//:vitest_bin`)
- The `ts` module extension (`ts.tsgo`)
- `//ts/toolchain:all` as the registration target, and the four toolchain types
  it registers (`oxc_toolchain_type`, `tsgo_toolchain_type`, `js_runtime_type`,
  `js_tool_type`)
- Gazelle `ts_compile` / `ts_test` generation and all `# gazelle:ts_*`
  directives

### Volatile

May change in any commit, without a changelog entry.

- `ts_dev_server`, `ts_codegen`, `ts_lint`, `ts_npm_publish`, `next_build` rules
- `DevServerInfo` and the two implementations of it, `//vite:dev_server` and
  `//oj:dev_server`
- `vite_bundler` and the Vite plugin (`vite/src/`)
- Gazelle codegen auto-detection and framework bundle generation
- Anything under `ts/private/` or `npm/private/`
