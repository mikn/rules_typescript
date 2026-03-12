# AGENTS.md — rules_typescript

Instructions for AI agents and contributors working on this codebase.

## Quality Standard

This ruleset targets **rules_go ergonomic parity**. The bar: a TypeScript developer writes `.ts` files, runs `bazel run //:gazelle`, then `bazel build //...` and `bazel test //...` — everything works with zero manual BUILD file editing.

## Development Workflow

```bash
bazel test //...                           # 28 unit/integration tests
bazel build //... --output_groups=+_validation  # redundant if .bazelrc has it
cd e2e/basic && bazel test //...           # e2e workspace
cd examples/react-app && bazel test //...  # example workspace

# Bootstrap tests (slow, spawn nested Bazel — run explicitly)
RULES_TYPESCRIPT_ROOT=$(pwd) bazel test //tests/bootstrap:test_new_project
```

## Three-Stage Development Cycle

For any non-trivial change:

1. **Implement** — write code, build, test, iterate until green
2. **Adversarial review** — separate agent finds bugs, design flaws, shell injection, depset violations
3. **Fix** — address all CRITICAL and HIGH findings, verify

Do not skip the review stage. It has caught real bugs in every round.

## Architecture (terse)

```
ts_compile → OxcCompile action (.js + .d.ts + .js.map)
           → TsgoCheck validation action (.tscheck stamp in _validation)

.d.ts = compilation boundary. Downstream sees only .d.ts, not .ts source.
Change implementation without changing .d.ts → no downstream recompilation.
```

**Key files:**
- `ts/defs.bzl` — public API (all rules, providers, macros)
- `ts/private/ts_compile.bzl` — core compilation rule
- `ts/private/providers.bzl` — JsInfo, TsDeclarationInfo, BundlerInfo, CssInfo, AssetInfo, NpmPackageInfo
- `ts/private/npm_translate_lock.bzl` — pnpm lockfile parser + @npm repo generator
- `ts/private/ts_test.bzl` — vitest test macro (auto node_modules)
- `ts/private/ts_bundle.bzl` — Vite production bundler
- `ts/private/ts_dev_server.bzl` — dev server with HMR
- `ts/private/ts_codegen.bzl` — general code generation
- `gazelle/generate.go` — BUILD file generation
- `gazelle/resolve.go` — import → label resolution
- `gazelle/config.go` — directives, framework detection, codegen detection
- `oxc_cli/src/main.rs` — Rust CLI (parse → isolated_declarations → transform → codegen)
- `vite/bundler.bzl` — Vite bundler wrapper

## Rules (never break these)

**Starlark:**
- Never materialize depsets at analysis time (no `.to_list()` in rule impls unless unavoidable + commented)
- `depset(order = "postorder")` for all transitive file sets
- `ctx.actions.run` over `ctx.actions.run_shell` when possible
- Shell strings: always use `_shell_escape()` for any interpolated path
- All `fail()` calls must have actionable messages with "Did you mean...?" suggestions

**Bazel:**
- bzlmod only. No WORKSPACE.
- Never reference `bazel-out/` directly. Use `ctx.bin_dir.path`, `File.path`.
- Optional toolchains: `config_common.toolchain_type(TYPE, mandatory = False)`
- Validation actions in `OutputGroupInfo(_validation = ...)`, not separate targets
- No `bazel clean`. Iterate. Trust the cache.

**Gazelle (Go):**
- All config via `# gazelle:ts_*` directives (not `gazelle_ts.json`, which is deprecated)
- Default: every-dir (every directory with .ts files is a package)
- `ts_test` auto-generates node_modules from npm deps in the `deps` list
- Register all new directives in `KnownDirectives()`, all new rules in `Kinds()` + `Loads()`

**Testing:**
- Every feature needs a test that ASSERTS correctness (not just "builds without errors")
- Bootstrap tests (`tests/bootstrap/`) test full user journeys — create project, gazelle, build, test
- Bootstrap tests found 5 real bugs on first run. They are not optional.

## Gazelle Directives (complete list)

| Directive | Effect |
|---|---|
| `ts_package_boundary every-dir\|index-only\|true` | Package boundary mode |
| `ts_isolated_declarations false` | Emit `isolated_declarations = False` |
| `ts_path_alias @/ src/` | Path alias (merges with parent) |
| `ts_runtime_dep @npm//:happy-dom` | Always-included test dep |
| `ts_exclude *.generated.ts` | Exclude pattern |
| `ts_warn_unresolved true` | Warn on unresolved imports |
| `ts_ignore` | Skip this directory |
| `ts_target_name <name>` | Override target name |
| `ts_codegen <name> <generator> <outs> [args]` | Custom codegen rule |

## Provider Contract

Every `ts_compile` target provides: `JsInfo` + `TsDeclarationInfo` + `OutputGroupInfo(_validation)`.
Every `ts_npm_package` provides: `JsInfo` + `TsDeclarationInfo` + `NpmPackageInfo`.
`css_library`/`css_module`/`asset_library`/`json_library` provide `TsDeclarationInfo` (for .d.ts stubs).

## npm Internals

`npm_translate_lock` (repository rule):
- Parses pnpm-lock.yaml (v6 + v9)
- Downloads tarballs, extracts to `@npm` repo
- Generates BUILD.bazel with `ts_npm_package` per package
- Handles: scoped packages, @types pairing, multiple versions, bin scripts, conditional exports, pnpm workspaces, dependency cycles (Kosaraju's SCC)

## What NOT to do

- Don't add Python dependencies. All codegen uses awk or Starlark `json.decode()`.
- Don't generate bash scripts for Windows compatibility paths. Use Node.js via the runtime toolchain.
- Don't add `gazelle_ts.json` features. Use directives.
- Don't create separate `_check` targets. Use `_validation` output group on the compile target.
- Don't assume `@npm` is the only repo name. Support custom names via the npm extension.
