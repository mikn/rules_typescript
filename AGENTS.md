# AGENTS.md — rules_typescript

## Project

Bazel ruleset for TypeScript. Oxc (Rust) compiles, tsgo (Go) type-checks. Modeled after rules_go: developers write `.ts`, never touch BUILD files, get hermetic cached builds. Isolated declarations is the architectural keystone — `.d.ts` emit is per-file syntactic (no type-checker needed), making TypeScript structurally identical to Go's per-package compilation model.

## Bazel Conventions (Modern/Idiomatic)

- **bzlmod only.** No WORKSPACE. All external deps via `MODULE.bazel` + `bazel_dep()`.
- **Never reference `bazel-out/` directly.** Use `ctx.bin_dir.path`, `File.path`, `File.dirname`. The config segment (`k8-fastbuild`, etc.) is unstable.
- **Pass deps individually.** Each dep's files are action inputs via `File.path`. Don't stage files into temp directories. Don't glob `bazel-out/`.
- **Optional toolchains.** Use `config_common.toolchain_type(TYPE, mandatory = False)` for optional features (tsgo type-checking, JS runtime). Check `ctx.toolchains[TYPE]` for `None`.
- **Validation actions.** Place type-checking in `OutputGroupInfo(_validation = ...)` on the target users build. Don't create separate `_check` targets — Bazel only runs `_validation` from the target being built.
- **Providers are the API.** `JsInfo`, `TsDeclarationInfo` are the interop contract. Every compilable target must provide both.
- **`dev_dependency = True`** for anything consumers don't need (gazelle, rules_go, test-only deps).
- **No `bazel clean`.** Iterate. Trust the cache.
- **`bzl_library`** for every `.bzl` file. Check visibility across packages.

## Starlark Style

- `ctx.actions.run` over `ctx.actions.run_shell` when possible. Shell only for tsgo (needs `touch` for stamp).
- `depset(order = "postorder")` for transitive file sets.
- `args.add_all()` for file lists — never materialize depsets at analysis time.
- Private attrs prefixed with `_`. Public rule attrs are the API surface.
- Macros in `defs.bzl`, raw rules in `private/`. Users load from `defs.bzl`.

## Architecture

```
ts_compile (one rule, one target)
  ├── OxcCompile action     → .js + .js.map + .d.ts
  └── TsgoCheck validation  → .tscheck stamp (in _validation output group)
       └── generates tsconfig.json inline (rootDirs bridges source + bin trees)
```

- **Compilation boundary:** `.d.ts` files. Downstream targets only see `.d.ts` from deps, not source `.ts`. Changing implementation without changing public API = no recompilation downstream.
- **tsconfig generation:** `module: "Preserve"`, `moduleResolution: "Bundler"` (not NodeNext — that requires `.js` extensions). `rootDirs` contains execroot root and `ctx.bin_dir.path` so tsgo can find dep `.d.ts` files in the output tree via the same relative paths used for source imports.
- **Ambient `.d.ts` in srcs:** Included in tsconfig `include` array. Used for type shims (e.g., JSX namespace without `@types/react`).

## npm Support

`npm_translate_lock` in `ts/private/npm_translate_lock.bzl` reads `pnpm-lock.yaml` and creates a self-contained `@npm` external repository:

- Parses both pnpm lockfile v6 and v9 (handles snapshots section, peer-dep suffixes, scoped packages).
- Downloads every package tarball via `repository_ctx.download_and_extract`. Verifies integrity with the SRI hash from the lockfile when present.
- Platform-specific packages (with `os`/`cpu` constraints) are filtered to host platform only.
- Generates a single `BUILD.bazel` with `ts_npm_package` targets. Label naming: `@types/react` → `types_react`, `react-dom` → `react-dom`.
- Types packages (`@types/*`) are auto-paired with their runtime package via `types_dep` attr.
- Exposed via `npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")`.

`ts_npm_package` in `ts/private/ts_npm_package.bzl` provides `NpmPackageInfo` with:
- `package_name`, `package_version`: package identity
- `package_dir`: the `package.json` File (used for tsconfig `paths` resolution)
- `all_files`: full file depset for runtime use by `node_modules` rule
- `js_files`, `declaration_files`: filtered subsets for compilation
- `transitive_deps`: depset of `NpmPackageInfo` for transitive closure
- `transitive_package_dirs`: depset of `package.json` Files (inputs to tsgo for Bundler resolution)

`node_modules` rule in `ts/private/node_modules.bzl` builds a hermetic `node_modules/` tree from `NpmPackageInfo` targets via a shell action that copies files using a tab-delimited manifest.

## Gazelle Extension

Go implementation in `gazelle/`. Generates `ts_compile` and `ts_test` BUILD targets.

**Package boundary heuristic (every-dir, default):** Every directory with `.ts`/`.tsx` source files gets a `ts_compile` target. This matches Go's behaviour.

**Package boundary heuristic (index-only, opt-in):** Only directories with `index.ts`/`index.tsx`, an explicit `# gazelle:ts_package_boundary true` directive, or the repo root become boundaries. Enable via `# gazelle:ts_package_boundary index-only` in the root BUILD.bazel.

BREAKING CHANGE (post-0.1.0): The default was switched from index-only to every-dir. Existing workspaces that relied on index-only behaviour should add `# gazelle:ts_package_boundary index-only` to their root BUILD.bazel to restore it.

**File classification:**
- `*.test.ts`, `*.spec.ts`, `*.test.tsx`, `*.spec.tsx` → `ts_test`
- `*.gen.ts`, `*.gen.tsx` → excluded (generated files, e.g. TanStack Router routeTree)
- All other `.ts`/`.tsx` → `ts_compile srcs`

**Import resolution** (in `resolve.go`): relative imports resolve via the rule index. Bare specifiers (npm packages) resolve via `npmPackages` map from `gazelle_ts.json`. Path aliases from `pathAliases` in `gazelle_ts.json` or `# gazelle:ts_path_alias` directives resolve to workspace-relative labels. Sub-path alias resolution (`@/utils/helpers`) falls back to the parent directory (`src/utils`) when the exact path is not in the index.

**Directives** (in `config.go`):
- `ts_package_boundary every-dir` / `index-only` / `true`: boundary mode control
- `ts_ignore` / `ts_ignore false`: suppress/re-enable generation
- `ts_target_name <name>`: override primary target name
- `ts_isolated_declarations false`: emit `isolated_declarations = False` on ts_compile and ts_test
- `ts_path_alias <alias> <dir>`: add/override a path alias mapping (merges with inherited, not replaces)
- `ts_runtime_dep <label>`: append a label to all ts_test deps in the subtree
- `ts_exclude <pattern>`: exclude files matching pattern from source targets
- `ts_warn_unresolved true`: warn on unresolved imports

**`gazelle_ts.json` (deprecated):** per-directory config file with `pathAliases`, `npmMappingFile`, `excludePatterns`, `excludeDirs`, `runtimeDeps.test`. Migrate to `# gazelle:` directives in BUILD.bazel instead.

## Runtime Toolchain

`JS_RUNTIME_TOOLCHAIN_TYPE` in `ts/private/runtime.bzl`. Provides `JsRuntimeInfo` with:
- `runtime_binary`: Node/Deno/Bun executable
- `runtime_name`: human-readable name
- `args_prefix`: arguments prepended before the script (e.g., `--experimental-vm-modules`)

`node_runtime_repo` downloads Node.js tarballs from nodejs.org per platform. `declare_node_runtime_toolchains` macro generates one `js_runtime_toolchain` + `toolchain()` per platform from `PLATFORM_CONSTRAINTS`.

The JS runtime is optional (`mandatory = False`). `ts_test` falls back to system `node` when no toolchain is registered.

Node.js is provisioned in `MODULE.bazel` via the `rules_nodejs` extension (`node.toolchain(name = "nodejs", node_version = ...)`). The `ts` extension in `ts/extensions.bzl` only exposes `oxc_toolchain` and `tsgo_toolchain` tags — there is no `ts.node_runtime` tag.

## Bundler Interface

`BundlerInfo` provider in `ts/private/providers.bzl`:
- `bundler_binary`: the CLI executable
- `config_file`: optional config passed via `--config`
- `runtime_deps`: depset of additional files the bundler needs at runtime

The bundler CLI contract (documented in `ts/private/ts_bundle.bzl`):
```
<bundler> --entry <path.js> --out-dir <dir> --format esm|cjs|iife
          [--external <pkg>]... [--sourcemap] [--config <file>]
```

`ts_bundle_impl` is shared between `ts_bundle` and `ts_binary` (both rules, identical attrs). Fallback mode (no bundler): concatenates all `.js` files from the depset using a shell action with a multiline manifest.

`vite_bundler` rule in `vite/bundler.bzl` returns `BundlerInfo` wiring a pre-built Vite CLI into the interface.

## Rust (oxc_cli)

- Edition 2024, min Rust 1.92.0.
- Built via `rules_rust` + `crate_universe`. `crate.from_cargo()` in MODULE.bazel.
- `clap` for CLI, `rayon` for parallel file processing, `miette` for diagnostics.
- `--files` takes 1..N paths. `--out-dir` for output. `--strip-dir-prefix` for package-relative output paths.
- Each file: parse → semantic → isolated_declarations → transform → codegen. Order matters: isolated_declarations runs BEFORE transform (which strips type info).

## Testing

- `tests/smoke/` — minimal .ts and .tsx compilation + validation
- `tests/multi/` — cross-package deps, compilation boundary verification
- `tests/vitest/` — real vitest test run via `ts_test` + `node_modules`
- `tests/bundle/` — `ts_binary` bundling with placeholder mode
- `tests/npm/` — npm package targets from `pnpm-lock.yaml`
- `e2e/basic/` — separate workspace with `local_path_override`, multi-package project + ts_binary bundle + Gazelle
- Always verify with `--output_groups=+_validation` to trigger type-checking

## Phase 6 Verification Results

All phases verified working end-to-end:

- `bazel build //tests/... --output_groups=+_validation` — passes (oxc compile + tsgo type-check)
- `bazel test //tests/vitest:math_test` — passes (vitest runs inside sandbox)
- `cd e2e/basic && bazel build //...` — passes (cross-package ts_binary bundle)
- npm support: `pnpm-lock.yaml` parsing, tarball download, `node_modules` tree generation
- Gazelle: extension builds and generates correct BUILD files

## Key Gaps (remaining)

- **tsgo checksums:** Populated for version 7.0.0-dev.20260311.1. Must update when tsgo version changes.
- **No Windows support.** Linux + macOS only.
- **Vite bundler:** Skeleton implementation. The `vite_binary` must be a pre-built CLI wrapper — no Bazel-hermetic Vite build yet.
- **Gazelle npm dep resolution:** Relies on `gazelle_ts.json` + `npmMappingFile` for non-standard npm layouts. Standard `@npm//:package` convention works out of the box.
