# Compatibility

## Bazel Versions

| Bazel Version | Support Level |
|---------------|--------------|
| 9.x | Fully supported (the only version CI runs) |
| 8.x | Untested (bzlmod is available; nothing verifies it) |
| 7.x | Untested |
| < 7.0 | Not supported (no bzlmod) |

rules_typescript requires bzlmod (MODULE.bazel). WORKSPACE-based setups are not
supported. `.bazelversion` in this repository pins 9.0.0 and CI installs
Bazelisk against it, so 9.x is the only version with evidence behind it.

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

### Windows

Windows does not work. The two blocking reasons are upstream, not ours:

- **No tsgo binary is published for Windows.** `TSGO_PLATFORMS` in
  `ts/private/toolchain.bzl` lists four platforms, none of them Windows, so
  toolchain resolution finds nothing. `ts_compile`'s default
  (`declarations = "tsgo"`) has no emitter, which means no `.d.ts` and no
  type-checking.
- **Hermetic pnpm has no Windows binary.** `_PNPM_PLATFORMS` in
  `ts/private/pnpm.bzl` covers Linux and macOS only, so `bazel run //:pnpm`
  cannot resolve. The `ts_pnpm` / `ts_add_package` wrappers are bash scripts
  besides.

Our own remaining shell dependency is much smaller than it was, and is no
longer about runners: `ts_binary`, `ts_test`, `ts_dev_server` and `npm_bin` run
through one checked-in Go launcher that reads a per-target JSON config. What
still needs a POSIX shell on the exec platform is a handful of build-action
wrappers — the Vite bundler and `next_build` — plus the `node_modules` tree's
fallback path, which is only taken when no JS runtime toolchain is registered.

What does exist on Windows: a registered Node.js toolchain, a `windows_amd64`
entry in `//platforms`, and a `node_modules` tree action that runs through a
cross-platform Node script rather than shell. That is enough to build a
`node_modules` directory and nothing else. If you need TypeScript on Bazel on
Windows today, use [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

## Vite and vitest

Neither is a dependency of this ruleset. Both come from your `pnpm-lock.yaml`,
and `ts_bundle`, `ts_dev_server` and `ts_test` generate configuration for
whatever version that resolves to. So "supported" here means *exercised by a
test in this repository*, and nothing constrains what you pin.

Two lockfiles are exercised, and only one of them is a *lane* — a Vite and a
vitest that generated configs actually run against. Everything runs on that one:

| Hub | Lockfile | Vite | vitest | What it exercises |
|---|---|---|---|---|
| `@npm` | `tests/npm/pnpm-lock.yaml` | 8.2.2 | 4.1.11 | `ts_test` (the whole `tests/vitest` suite), `ts_dev_server` (five servers started for real and interrogated over HTTP), `ts_bundle` output (`tests/vite_bundle`), `vite-plugin-bazel`'s own tests, the nested-Bazel integration workspaces, `examples/` |
| `@npm_features` | `tests/npm/pnpm-lock-features.yaml` | — | — | pnpm's patched dependencies, npm aliases, peer-dependency variants and per-importer resolution. It resolves neither tool, so it is a second lockfile rather than a second lane |

To re-derive all of that rather than trusting the table:

```bash
grep -nE '^  (vite|vitest)@' tests/npm/pnpm-lock.yaml tests/npm/pnpm-lock-features.yaml
bazel query 'filter("behaviour_test$", tests(//tests/dev_server/...))'
```

A second lane on a second major is not extra coverage; it is a second lockfile to
keep in step. The one thing it was there to hold together — the Vite that
`vite-plugin-bazel` declares a peer range for, and the Vite the ruleset installs —
is now held by a test instead: `//vite/tests:peer_version_test` reads
`peerDependencies.vite` out of `vite/package.json` and asserts the installed major
is one that range names.

The two places a generated config is known to be version-sensitive:

- **`ts_bundle`** emits `build.rollupOptions.output.manualChunks` for
  `split_chunks`, and `minify = True` emits `true` rather than naming a
  minifier. Both are spellings every generation from 6 onward honours; the
  vendor-splitting plugin `split_chunks` used to emit was removed in Vite 7,
  and naming `esbuild` picks a minifier that is an optional peer and so absent
  from a tree built from `deps = ["@npm//:vite"]`. `minify = False` also pins
  `output.minify: false`, because the dead-code pass otherwise re-emits each
  chunk from its AST and discards what a plugin's `renderChunk` returned.
- **`ts_test`** reads a `config` file that default-exports an array as a list of
  vitest projects and emits `test.projects`. That option is vitest 3.2 and
  later: `test.workspace`, the name it replaced, was removed in vitest 4, which
  throws on it rather than ignoring it.

## Versioning Policy

This project follows [Semantic Versioning 2.0.0](https://semver.org/) from 1.0
onward. It has not reached 0.1.0 yet: there is no tag, no release and no Bazel
Central Registry entry, and consumers pin a commit.

**Pre-1.0 (current):** any commit may break the API, with no deprecation
window and no compatibility shim. Breaks are listed in
[CHANGELOG.md](CHANGELOG.md) with the edit each one requires, which is the
migration path. The last two rounds of work broke `ts_compile`, `ts_test`, the
npm extension and the toolchain API; read the changelog before bumping a pin.

**Post-1.0 (future):** major versions for breaking changes, minor for features,
patch for fixes.

## Public API Surface

Everything is unstable pre-1.0. The distinction below is about how likely a
thing is to move, not a guarantee.

### Load-bearing — breaks get a changelog entry with the required edit

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

### Volatile — may change in any commit, without a changelog entry

- `ts_dev_server`, `ts_codegen`, `ts_lint`, `ts_npm_publish`, `next_build` rules
- `vite_bundler` and the Vite plugin (`vite/src/`)
- Gazelle codegen auto-detection and framework bundle generation
- `gazelle_ts.json` — deprecated; Gazelle prints a warning and reads
  `tsconfig.json` plus directives instead
- Anything under `ts/private/` or `npm/private/`
