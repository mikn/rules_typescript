# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [0.1.0] - 2026-03-12

### Added

- Core TypeScript compilation with oxc-bazel (Rust-based, per-file transform)
- Type-checking with tsgo (Go port of TypeScript) as Bazel validation action
- Isolated declarations support (.d.ts compilation boundary for incremental builds)
- npm dependency management via pnpm-lock.yaml parser (v6 and v9 formats)
- Multiple npm package version support with semver-correct alias resolution
- npm bin script generation (`@npm//:vitest_bin`, `@npm//:esbuild_bin`, etc.)
- Conditional exports (`package.json` `"exports"` field) resolution
- pnpm workspace support (`workspace:*` protocol)
- Dependency cycle detection and breaking (Kosaraju's SCC algorithm)
- Gazelle extension for BUILD file auto-generation
- Gazelle every-dir default (every directory with .ts files is a package)
- Gazelle directives: `ts_package_boundary`, `ts_isolated_declarations`, `ts_path_alias`, `ts_runtime_dep`, `ts_exclude`, `ts_warn_unresolved`, `ts_codegen`
- Gazelle auto-detection of TanStack Router, Prisma, GraphQL codegen, OpenAPI generators
- Gazelle reads `tsconfig.json` `compilerOptions.paths` automatically
- `ts_test` with vitest (auto node_modules from deps, DOM testing, coverage, snapshots, custom config, environment selection)
- `ts_binary` (runnable, entry_file convention, index.js default)
- `ts_bundle` with real Vite integration (ESM/CJS, tree-shaking, minification, chunk splitting, source maps, app mode, env_vars)
- `ts_dev_server` with ibazel HMR support and React Fast Refresh
- Vite plugin injection via `vite_config` attr (unlocks Remix, SvelteKit, TanStack Start)
- `ts_codegen` rule for code generation (TanStack routes, Prisma, GraphQL, OpenAPI, custom)
- `css_library`, `css_module` (typed .d.ts from regex extraction), `asset_library`, `json_library` (fully typed .d.ts)
- `ts_lint` rule (ESLint/oxlint as validation action)
- `ts_npm_publish` rule (tarball with auto-filled main/types/exports)
- ESLint plugin for isolated declarations migration (`require-explicit-types` rule, 31 test cases)
- `ts_compile_legacy` macro for gradual isolated declarations adoption
- `vite_types` attr on `ts_compile` for `import.meta.env` and asset URL types
- `path_aliases` attr on `ts_compile` for tsgo type-checking with path aliases
- JS runtime toolchain (Node.js via rules_nodejs, pluggable for Deno/Bun/workerd)
- ARM toolchain_utils (v1.3.0) integration for resolved toolchain targets
- Linux ARM64 platform support (oxc built from source, tsgo from npm)
- Windows Node.js toolchain registered (node_modules action cross-platform)
- IDE support via `bazel run //:refresh_tsconfig` (generates tsconfig.json with paths)
- GitHub Actions CI workflow
- Bootstrap integration tests (8 tests covering full user journeys)
- Examples: basic, app (zod), react-app (React + testing-library), tanstack-app (TanStack Router SPA)
- BCR presubmit configuration
- Release automation scripts
