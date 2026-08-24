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

Windows does not work, and the gap is not a matter of a few rough edges:

- **No tsgo binary is published for Windows.** `TSGO_PLATFORMS` in
  `ts/private/toolchain.bzl` lists four platforms, none of them Windows, so
  toolchain resolution finds nothing. `ts_compile`'s default
  (`declarations = "tsgo"`) has no emitter, which means no `.d.ts` and no
  type-checking.
- **Every runner is a bash script.** `ts_test`, `ts_binary`, `ts_dev_server`,
  `ts_codegen`, `npm_bin`, `next_build` and the Vite bundler each write a
  `#!/usr/bin/env bash` launcher. `bazel test` and `bazel run` on those targets
  need a bash environment (Git Bash, WSL) even where the underlying tool is
  cross-platform.
- **Hermetic pnpm has no Windows binary.** `_PNPM_PLATFORMS` in
  `ts/private/pnpm.bzl` covers Linux and macOS only, so `bazel run //:pnpm`
  cannot resolve.

What does exist on Windows: a registered Node.js toolchain, a `windows_amd64`
entry in `//platforms`, and a `node_modules` tree action that runs through a
cross-platform Node script rather than shell. That is enough to build a
`node_modules` directory and nothing else. If you need TypeScript on Bazel on
Windows today, use [aspect-build/rules_ts](https://github.com/aspect-build/rules_ts).

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

- `ts_compile`, `ts_test`, `ts_binary`, `ts_bundle`, `ts_config` rules and their
  documented attributes
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
