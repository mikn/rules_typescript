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

A `libc: [musl]` tarball in `pnpm-lock.yaml` matches no platform, and the `npm`
extension drops it without declaring a repository for it, the same path a
tarball with `cpu: [ppc64]` or `os: [aix]` takes. It is never fetched, extracted
or staged into an action.

On a musl host the Node the ruleset downloads is still the glibc build.

### Windows

Windows is not supported right now. It may be considered in the future.

What exists there today: a registered Node.js toolchain, a `windows_amd64` entry
in `//platforms`, and a `node_modules` tree action driven by a cross-platform
Node script with no shell dependency. That builds a `node_modules` directory and
nothing else.

Support would take a Windows entry in `TSGO_PLATFORMS`
(`ts/private/toolchain.bzl`) and `_PNPM_PLATFORMS` (`ts/private/pnpm.bzl`), and
a replacement for the one build-action wrapper that still needs a POSIX shell,
the `node_modules` fallback taken when no JS runtime toolchain is registered.
oxc needs no entry: `oxc-bazel` is built from source by
rules_rust for whichever exec platform the build runs on, so one toolchain
covers every platform. None of this has been run on Windows, so any estimate of
the remaining work is untested.

If you need TypeScript on Bazel on Windows today, use
[aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

## Vite and vitest

Neither is a dependency of this ruleset. Both come from your `pnpm-lock.yaml`,
and `ts_dev_server` and `ts_test` generate configuration for whatever version
that resolves to. "Supported" here means a test in this
repository exercises that version; nothing constrains what you pin.

There is one lane: one Vite version and one vitest version. The workspace
translates six lockfiles; four resolve one or both tools, all at one version, so
no test runs a generated config against a second major:

| Hub | Lockfile | Vite | vitest | Coverage |
|---|---|---|---|---|
| `@npm` | `tests/npm/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | `ts_test` (the whole `tests/vitest` suite), `ts_dev_server` (six servers started and interrogated over HTTP), and `vite-plugin-bazel`'s own tests |
| `@npm_tailwind` | `tests/tailwind/lock/pnpm-lock.yaml` | 8.2.2 | — | Tailwind v4 through `vite_config`, under the dev server |
| `@npm_workers` | `tests/workers/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | `ts_test` with the Workers pool (vitest inside workerd), and the `wrangler types` generator `//tools/codegen:wrangler_types` under `ts_codegen` (`tests/worker_types`) |
| `@npm_eslint` | `tests/eslint/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | the ESLint plugin's own `ts_test` target, against `@typescript-eslint`'s rule tester |
| `@npm_features` | `tests/npm/features/pnpm-lock.yaml` | — | — | pnpm's patched dependencies, npm aliases, peer-dependency variants, per-importer resolution; resolves neither tool |
| `@npm_esbuild` | `vite/esbuild/pnpm-lock.yaml` | — | — | the esbuild that bundles `vite-plugin-bazel`. The one hub that is not a fixture; the bundle ships to consumers as API |

The `examples/` modules and the integration workspaces under `tests/integration/`
are separate Bazel modules with their own lockfiles, outside the table above.
`examples/app`, `examples/react-app`, `e2e/basic`,
`tests/integration/gazelle_roundtrip` and `tests/integration/npm_deps` resolve
Vite 8.2.2 and vitest 4.1.11; `tests/integration/lsp` resolves neither tool,
`tests/integration/store_cache` resolves one package with one dependency, and
`examples/basic` has no npm dependencies.

To re-derive the table from the repository:

```bash
grep -rnE '^  (vite|vitest)@' --include=pnpm-lock.yaml .
bazel query 'filter("behaviour_test$", tests(//tests/dev_server/...))'
```

No hub carries a second major, and the grep prints no other.

The Vite that `vite-plugin-bazel` declares a peer range for and the Vite the
ruleset installs are held together by `//vite/tests:peer_version_test`, which
reads `peerDependencies.vite` out of `vite/package.json` and asserts the
installed major is one that range names.

The one place a generated config is known to be version-sensitive:

- **`ts_test`** reads a `config` file that default-exports an array as a list of
  vitest projects and emits `test.projects`. That option is vitest 3.2 and
  later. `test.workspace`, the name it replaced, was removed in vitest 4, which
  throws on it.

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

### Load-Bearing

Breaks get a changelog entry with the required edit.

- `ts_compile`, `ts_test`, `ts_binary`, `ts_config`,
  `node_modules` and `ts_refresh_tsconfig` rules and their documented
  attributes
- The `ts_pnpm` and `ts_add_package` macros, written by hand into the root
  `BUILD.bazel` beside a lockfile; Gazelle writes neither
- `TsInfo`, `BundlerInfo`, `TsTestRunnerInfo` providers
- The `npm` module extension (`npm.translate_lock`, `npm.pnpm`) and the `@npm`
  label surface (`@npm//:zod`, `@npm//:types_react`, `@npm//:vitest_bin`)
- The `ts` module extension (`ts.tsgo`, `ts.lint`) and the `//ts:lint` label
  flag it sets
- `//ts/toolchain:all` as the registration target, and the four toolchain types
  it registers (`oxc_toolchain_type`, `tsgo_toolchain_type`, `js_runtime_type`,
  `js_tool_type`)
- Gazelle `ts_compile`, `ts_test` and `ts_config` generation. The extension
  declares no directive of its own; `# keep`, `# gazelle:exclude` and
  `# gazelle:resolve` are core Gazelle's

### Volatile

May change in any commit, without a changelog entry.

- `ts_dev_server` and `ts_codegen` rules
- The `ts_codegen` generators under `//tools/codegen` (`tanstack_routes`,
  `wrangler_types`)
- `npm_bin` as a rule loaded by hand; the generated `@npm//:<pkg>_bin` labels
  are load-bearing above
- `DevServerInfo` and its implementation, `//vite:dev_server`
- The Vite plugin (`vite/src/`)
- Anything under `ts/private/` or `npm/private/`
