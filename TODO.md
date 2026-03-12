# rules_typescript — Remaining Work

## Project Vision

**"TypeScript on Bazel should feel like Go on Bazel."**

Today we have: compilation (oxc), type-checking (tsgo), npm deps (pnpm lockfile), Gazelle, vitest testing, and a placeholder bundler. This gets you a **pure TypeScript library monorepo** with hermetic cached builds.

What we don't have: real bundling, dev server with HMR, CSS/asset support, or framework integration. This means **no real Next.js, Remix, TanStack Start, or SvelteKit apps** can be built today.

### Current Readiness

| Area | Status |
|------|--------|
| Compilation (oxc) | Production-ready |
| Type-checking (tsgo) | Production-ready |
| npm deps (pnpm → Bazel) | Production-ready |
| Gazelle BUILD generation | Production-ready (JS/TS, CSS, assets, path aliases from tsconfig.json, ts_dev_server) |
| Testing (vitest) | Solid (DOM, coverage, snapshots, custom config, watch mode, debugging all done) |
| Bundling | Vite bundler (production quality) |
| Dev server + HMR | Rule exists; basic ibazel workflow documented |
| CSS / assets | css_library, css_module, asset_library, json_library rules; CSS module mock in ts_test |
| Framework integration | Gazelle detection + TanStack plugin (foundation) |
| npm publishing | ts_npm_publish with auto-filled main/types/exports |
| CI/CD | Docs: remote caching (BuildBuddy/EngFlow), RBE, GitLab CI, non-determinism |

---

## Sub-Project 1: Real Bundler Integration

**Goal:** `ts_bundle(bundler = "//vite:bundler")` produces production-quality bundles with tree-shaking, code splitting, and minification. Vite is the default shipped implementation.

### 1.1 Vite Bundler — Build & Distribution
- [ ] Build `vite/src/plugin.ts` inside Bazel (currently TypeScript source, never compiled)
- [ ] Create `ts_compile` target for the Vite plugin (or use `tsup` genrule)
- [ ] Package the compiled plugin as an npm-publishable artifact
- [ ] Create `vite_toolchain` — repository rule that downloads Vite binary + our plugin
- [ ] Register Vite as the default bundler toolchain (optional, like tsgo)
- [x] Make `vite_bundler` rule accept @npm//:vite + node_modules() tree and generate a working wrapper script (SP1 partial: uses @npm//:vite_bin infra from SP6)

### 1.2 Vite Build Integration
- [x] Wire `vite build` as the bundler action in `ts_bundle` when Vite bundler is set
- [x] Generate `vite.config.mjs` as a Bazel action (not hand-written)
  - [x] `build.rollupOptions.input` (entry path via VITE_ENTRY_PATH env var)
  - [x] `build.outDir` pointing at declared outputs (via VITE_OUT_DIR env var)
  - [x] `resolve.alias` mapping Bazel package paths to bazel-bin outputs (via EXEC_ROOT env var)
  - [x] `build.lib` mode (lib mode with explicit fileName to control output name)
- [x] Support `format` attr (esm/cjs/iife) → Vite output format
- [x] Support `external` attr → Vite externals
- [x] Support `define` attr → Vite define
- [x] Support `sourcemap` attr → Vite sourcemap config
- [x] Declare output files: `<name>.<fmt>.js`, `<name>.<fmt>.js.map`
- [x] Support chunk splitting via `split_chunks` attr (splitVendorChunkPlugin; output is a directory)

### 1.3 Minification & Tree-Shaking
- [x] Pass through minification options (esbuild via `minify` attr)
- [x] Verify tree-shaking works with `.d.ts` compilation boundary (confirmed: `add` and `PI` are inlined, dead exports dropped)
- [x] Add `minify` attr to `ts_bundle` (bool, default True; maps to Vite `build.minify: "esbuild"`)

### 1.4 Alternative Bundlers
- [x] Document the `BundlerInfo` interface for custom bundler authors (README: Custom bundler section)
- [ ] Create esbuild bundler implementation (for speed-focused users)
- [ ] Create Rolldown bundler implementation (when Rolldown stabilizes)

### 1.5 Source Map Chain
- [x] Verify 3-level source map chain: `.ts` → oxc `.js.map` → Vite bundle `.js.map` → browser (test: //tests/vite_bundle:sourcemap_chain_test)
- [x] Ensure `sourcesContent` is populated for debugging without source files (verified in sourcemap_chain_test)

---

## Sub-Project 2: Dev Server & HMR

**Goal:** `bazel run //app:dev` starts a Vite dev server with HMR. Edit a `.ts` file, see changes in the browser within 500ms.

### 2.1 ts_dev_server Rule
- [x] Create `ts/private/ts_dev_server.bzl` as an executable rule
- [x] Accept `entry_point` (ts_compile target)
- [x] Accept `port`, `host`, `open` attrs
- [x] Accept optional `plugin` attr (compiled vite-plugin-bazel .mjs)
- [x] Generate runner script that starts Vite dev server
- [x] Wire runfiles: compiled .js files, node_modules tree
- [x] Export from `ts/defs.bzl`
- [x] Accept `bundler` attr (BundlerInfo provider, for non-Vite dev servers)

### 2.2 Vite Plugin — Dev Mode
- [x] Build `vite/src/*.ts` inside Bazel (genrule using esbuild — `//vite:vite_plugin_bazel`)
- [x] `bazelPlugin()` in plugin.ts resolves `.ts` imports to compiled `.js` in bazel-bin
- [x] `BazelWatcher` in watcher.ts watches bazel-bin for changes (triggered by ibazel)
- [x] HMR invalidation on file change (`handleRebuild` in plugin.ts)
- [x] Wire compiled plugin into ts_dev_server via `plugin` attr
- [x] Generate Vite config for dev mode that dynamically imports the plugin
- [ ] Handle `rootDirs`-style path mapping (source tree ↔ output tree) — BazelResolver partially handles this
- [x] Support React Fast Refresh (via `@vitejs/plugin-react`): `react_refresh = True` attr on `ts_dev_server`

### 2.3 ibazel Integration
- [x] Document the `ibazel run //app:dev` workflow in ts_dev_server.bzl docstring
- [x] Vite's file watcher monitors bazel-bin for .js changes (no server restart needed)
- [x] Runner script passes BAZEL_BIN_DIR env var to the Vite config
- [ ] Detect ibazel via `IBAZEL_NOTIFY_CHANGES` environment variable (for explicit protocol)
- [ ] Parse ibazel's change notification protocol
- [ ] Trigger Vite HMR update on ibazel rebuild completion (use BazelWatcher from plugin.ts)
- [ ] Handle incremental rebuilds (only changed modules invalidated)
- [ ] Measure and optimize edit-to-HMR latency (target: <500ms)

### 2.4 Gazelle Integration
- [x] Teach Gazelle to generate `ts_dev_server` targets in app packages
- [x] Auto-detect entry points for dev server

---

## Sub-Project 3: CSS & Asset Support

**Goal:** `import "./Button.css"` works in compilation, bundling, and dev server. Assets (images, fonts, SVGs) are handled correctly.

### 3.1 CSS Imports in Compilation
- [x] Define `CssInfo` provider (css_files depset, transitive_css_files depset)
- [ ] Modify `ts_compile` to accept `.css` files in srcs (pass through, not compiled)
- [x] Create `css_library` rule that provides `CssInfo`
- [x] Emit `.css` files alongside `.js` in output tree (transitive_css_files in DefaultInfo)
- [ ] Strip CSS import statements from compiled `.js` — the bundler (Vite) handles this at bundle time; for library targets without a bundler, oxc leaves CSS imports in the .js output which may cause runtime errors if executed directly in Node.js without a bundler

### 3.2 CSS Modules
- [x] Support `import styles from "./Button.module.css"` pattern
- [x] Generate `.d.ts` for CSS modules (mapping class names to strings via regex extraction)
- [ ] Wire CSS module compilation into the build pipeline (PostCSS? Lightning CSS?)

### 3.3 Tailwind CSS
- [ ] Support `@tailwind` directives
- [ ] Wire Tailwind as a PostCSS plugin in the bundler
- [ ] Content scanning for purging unused styles

### 3.4 Asset Handling
- [x] Define `AssetInfo` provider
- [x] Support `import logo from "./logo.svg"` (generates ambient .d.ts returning string)
- [ ] Asset hashing for cache busting in production bundles
- [ ] Asset manifest generation
- [ ] Copy assets to bundle output directory

### 3.5 Gazelle — CSS & Asset Recognition
- [x] Teach Gazelle to extract CSS imports from `.ts`/`.tsx` files
- [x] Generate `css_library` targets for `.css` files
- [x] Handle CSS module imports separately from plain CSS (css_module targets)
- [x] Generate `asset_library` targets for image/font/SVG/JSON asset files
- [x] Resolve `import styles from "./Button.module.css"` to css_module dep
- [x] Resolve `import logo from "./logo.svg"` to asset_library dep

---

## Sub-Project 4: Framework Integration

**Goal:** Real Next.js, TanStack Start, Remix, and SvelteKit apps build, test, and serve via Bazel.

### 4.1 Next.js
- [ ] Create `next_build` rule that wraps `next build` as a Bazel action
- [ ] Inputs: compiled .js from ts_compile, node_modules tree, next.config.js
- [ ] Outputs: .next build directory (or selective outputs)
- [ ] Support App Router (app/ directory convention)
- [ ] Support Pages Router (pages/ directory convention)
- [ ] Support API Routes
- [ ] Support Server Components (`"use client"` / `"use server"` directives)
- [ ] Support `next/image` optimization
- [ ] Support `next/font` loading
- [ ] Support middleware
- [ ] Create `next_dev_server` rule for `next dev` integration
- [ ] Gazelle plugin for Next.js file conventions
- [ ] Example: real Next.js app with pages, API routes, and SSR

### 4.2 TanStack Start
- [ ] Create `tanstack_build` rule wrapping Vinxi/Nitro build
- [x] Support file-based routing (routes/ convention) in Gazelle
- [ ] Support server functions (RPC serialization)
- [ ] Support route validation (zod schemas in route params)
- [x] Extend existing Gazelle TanStack plugin with build metadata (RouteInfo, route comments)
- [x] Support dynamic route params in Gazelle ($userId.tsx → :userId)
- [ ] Create `tanstack_dev_server` rule
- [ ] Example: real TanStack Start app with routes and server functions

### 4.3 Remix
- [ ] Create `remix_build` rule
- [ ] Support route conventions (routes/ with nested layouts)
- [ ] Support loader/action functions
- [ ] Support resource routes
- [ ] Gazelle plugin for Remix conventions

### 4.4 SvelteKit
- [ ] Support `.svelte` file compilation (requires Svelte compiler)
- [ ] Create `sveltekit_build` rule
- [ ] Support +page/+layout conventions
- [ ] Support server-side modules (+page.server.ts)

### 4.5 Framework Detection in Gazelle
- [x] Auto-detect framework from package.json dependencies (@tanstack/react-router, @tanstack/start → TanStack; next → NextJS)
- [x] Load appropriate plugin (TanStack enabled automatically when detected)
- [ ] Generate framework-specific build targets automatically (requires full build rule, deferred)

---

## Sub-Project 5: Testing Maturity

**Goal:** vitest tests work reliably with DOM testing, coverage, snapshots, and custom config.

### 5.0 ts_test Ergonomics (DONE)
- [x] Auto-generate node_modules tree from @npm// deps in ts_test macro
- [x] No more explicit node_modules target or node_modules attr required
- [x] Gazelle no longer generates node_modules rules; emits empty stubs to delete stale ones
- [x] Backwards compatible: explicit node_modules attr still accepted
- [x] `gazelle_ts.json` `runtimeDeps.test` field: Gazelle appends listed labels to every ts_test deps list — eliminates manual happy-dom, react, @vitest/coverage-v8 additions

### 5.1 DOM Testing
- [x] Verify @testing-library/react works with vitest in Bazel sandbox
- [x] Verify happy-dom or jsdom environment works
- [x] Add `environment` attr to `ts_test` (node/happy-dom/jsdom)
- [x] Create example with @testing-library component tests

### 5.2 Coverage
- [x] Pass --coverage flag to vitest CLI
- [x] Collect coverage artifacts and integrate with bazel coverage (`COVERAGE_OUTPUT_FILE` + `_lcov_merger` + `fragments = ["coverage"]`)
- [x] Collect coverage artifacts (lcov) as test outputs (written to `COVERAGE_OUTPUT_FILE`)
- [x] Integrate with Bazel's `--combined_report=lcov` (combined report produced at `bazel-out/_coverage/_coverage_report.dat`)
- [ ] Support `--instrumentation_filter` for selective coverage (InstrumentedFilesInfo traversal not yet wired)

### 5.3 Snapshot Testing
- [x] Solve Bazel read-only sandbox issue for snapshot writes
  - Option C implemented: `update_snapshots = True` attr on `ts_test` produces an
    executable `bazel run` target that passes `--update` to vitest and writes
    snapshots back into the source tree via `BUILD_WORKSPACE_DIRECTORY`.
  - `--sandbox_writable_path` documented as alternative in README.
- [x] Document snapshot workflow in Bazel context (README.md)

### 5.4 Custom vitest Configuration
- [x] Add `config` attr to `ts_test` (label to vitest.config.ts)
- [x] Support custom reporters, setup files, global setup
- [ ] Support `vitest.workspace.ts` for monorepo configurations

### 5.5 Watch Mode
- [x] Document `ibazel test //path:test` as the watch mode workflow (README.md)
- No custom rule needed: ibazel works with standard `ts_test` targets out of the box.

### 5.6 Debugging
- [x] Document how to attach a debugger to vitest in Bazel (README.md)
- [x] `--inspect-brk` documented via `env = {"NODE_OPTIONS": "--inspect-brk=9229"}` pattern
- [x] VS Code launch.json template created (.vscode/launch.json.template)

---

## Sub-Project 6: npm Support Hardening

**Goal:** Handle real-world npm dependency graphs (100+ packages, multiple versions, bin scripts, workspaces).

### 6.1 Bin Scripts
- [x] Extract `bin` field from package.json during npm_translate_lock
- [x] Generate executable targets for each bin script (e.g., `@npm//:vitest_bin`)
- [x] Wire bin scripts into `ts_test` vitest resolution (replace heuristic path)
- [x] Support `npx`-style invocation: `bazel run @npm//:tsx -- script.ts`

### 6.2 Multiple Package Versions
- [x] Support pnpm's package aliasing (`react@18` + `react@19` in same lockfile)
- [x] Generate versioned target names: `@npm//:react_19_1_0` alongside `@npm//:react`
- [x] Primary alias (`@npm//:react`) points to highest semver version (preserved behaviour)
- [x] Dependency edges from dependents correctly reference the versioned label they actually use
- [x] Test: `@vitest/pretty-format` at 3.0.9+3.2.4 is exercised by the existing lockfile (//tests/npm:npm_multi_version_test)
- [ ] Resolve correct version per importer (from lockfile's `importers` section — workspace-level pinning)

### 6.3 pnpm Workspaces
- [ ] Parse `pnpm-workspace.yaml`
- [ ] Resolve `workspace:*` protocol references to local Bazel targets
- [ ] Map workspace packages to ts_compile targets

### 6.4 Conditional Exports
- [x] Parse `exports` field in package.json
- [x] Resolve conditional exports (import/require/types/default) correctly
- [x] Wire resolved entry points into TsDeclarationInfo

### 6.5 Integrity & Security
- [ ] Verify SRI hashes for all downloaded packages (fail if missing, with override)
- [ ] Add `--strict_npm_integrity` flag
- [ ] Support npm audit integration (report vulnerable packages)

### 6.6 Performance
- [ ] Lazy package download (only fetch packages needed for current targets)
- [ ] Parallel tarball downloads in repository rule
- [ ] Cache downloaded tarballs across `bazel clean`

---

## Sub-Project 7: Gazelle Improvements

**Goal:** Gazelle handles real-world TypeScript patterns including CSS, dynamic imports, path aliases, and framework conventions.

### 7.1 CSS Import Recognition
- [ ] Extract CSS imports from `.ts`/`.tsx` files
- [ ] Generate appropriate targets (css_library or filegroup)
- [ ] Handle CSS modules differently from plain CSS imports

### 7.2 Dynamic Import Handling
- [x] Detect `import("./page")` dynamic imports
- [x] Generate deps for dynamically imported modules
- [x] Support template literal dynamic imports: `` import(`./pages/${name}`) `` (skip, don't error)

### 7.3 Path Alias Reading from tsconfig.json
- [x] Read `compilerOptions.paths` from tsconfig.json (not just gazelle_ts.json)
- [x] Support `baseUrl` + `paths` resolution
- [x] Fall back to gazelle_ts.json if both exist (gazelle_ts.json takes priority)

### 7.4 Re-export Handling
- [x] `export * from "./utils"` should resolve to the re-exported module
- [x] `export { foo } from "./bar"` should add dep on `./bar`'s package
- [x] Handle barrel files (index.ts that re-exports everything)

### 7.5 Error Reporting
- [x] Warn when an import cannot be resolved (instead of silently dropping)
- [x] Show which resolution strategies were tried
- [x] `# gazelle:ts_warn_unresolved` directive to control warning level

### 7.6 Generated File Patterns
- [x] Exclude `.next/`, `.nuxt/`, `.svelte-kit/`, `dist/`, `build/` directories
- [x] Configurable exclude patterns via `gazelle_ts.json`
- [x] Handle `*.gen.ts`, `*.generated.ts`, `*.auto.ts` patterns

### 7.7 Framework Plugins
- [ ] Next.js plugin: detect pages/, app/, recognize file conventions
- [ ] Remix plugin: detect routes/, handle nested layouts
- [ ] SvelteKit plugin: detect +page/+layout, handle .svelte files
- [ ] Auto-load plugin based on detected framework

---

## Sub-Project 8: Isolated Declarations Migration

**Goal:** Provide tooling that helps teams add explicit return types to all exports, enabling isolated declarations.

### 8.1 ESLint Plugin
- [x] Implement `@rules_typescript/eslint-plugin-isolated-declarations`
- [x] Rule: `require-explicit-types` — error on exports without explicit return type
- [ ] Auto-fix: infer return type from TypeScript and insert annotation
- [x] Handle edge cases: overloads, generics, conditional types
- [x] Publish to npm (package prepared with dist/ built; not actually published)

### 8.2 Migration CLI
- [ ] Create `isolated-declarations-migrate` CLI tool
- [ ] Scan codebase for exports missing explicit types
- [ ] Report count and locations
- [ ] `--fix` mode: auto-insert inferred types (requires tsc or tsgo for inference)
- [ ] `--check` mode: exit 1 if any violations (for CI)

### 8.3 Gradual Rollout
- [x] Support `isolated_declarations = False` per-target in ts_compile
- [x] Create `ts_compile_legacy` macro that defaults to `isolated_declarations = False`
- [x] Document migration strategy: enable per-package, fix violations, move to next package

---

## Sub-Project 9: Developer Experience

**Goal:** Using rules_typescript feels seamless — IDE integration, clear errors, fast feedback loops.

### 9.1 IDE Integration
- [x] Generate tsconfig.json at workspace root for IDE consumption
  - [x] `bazel run //:refresh_tsconfig` target
  - [x] Maps Bazel package structure to tsconfig `paths` (references require per-package tsconfig.json)
  - [x] Points at bazel-bin for .d.ts resolution via rootDirs
- [x] VS Code settings template (.vscode/settings.json.template)
- [x] IDE setup documented as Quick Start step 5 (right after Gazelle)
- [ ] WebStorm/IntelliJ configuration guide

### 9.2 Error Messages
- [x] Audit all `fail()` calls — ensured each has actionable guidance
- [x] Added `Did you mean...?` suggestions for common mistakes (ts_binary, ts_bundle, node_modules)
- [x] `build --output_groups=+_validation` in all .bazelrc files: type errors now fail `bazel build` by default
- [ ] Improve oxc error output for isolated declarations failures

### 9.3 Build Feedback
- [x] Add `--show_result=N` recommendation to docs (README.md)
- [ ] Create `bazel_ts_info` rule that reports compilation statistics
- [ ] Consider progress messages in actions ("Compiling 5 TypeScript files...")

### 9.4 Linting Integration
- [x] Create `ts_lint` rule wrapping eslint or oxlint
- [x] Wire as a validation action (like type-checking)
- [x] Gazelle generates `ts_lint` targets alongside `ts_compile` when an oxlint.json or .eslintrc.* config is detected

---

## Sub-Project 10: Production & CI/CD

**Goal:** rules_typescript works reliably in CI/CD pipelines with remote caching and execution.

### 10.1 Remote Caching
- [x] Document setup with BuildBuddy, EngFlow, or Bazel Cache
- [x] Verify all actions are hermetic (no network access, no env leaks)
- [ ] Test with `--remote_cache` flag (requires external infra)

### 10.2 Remote Execution
- [x] Document RBE setup (BuildBuddy RBE, EngFlow, custom executor image)
- [x] Verify oxc-bazel, tsgo, and Node binaries work in remote execution (statically linked)
- [x] Platform-specific binary selection via exec platform constraints (documented)
- [ ] Test with `--remote_executor` flag (requires external infra)

### 10.3 CI Examples
- [x] GitHub Actions workflow template
- [x] GitLab CI template
- [x] Generic CI script (scripts/ci.sh)

### 10.4 BCR Publishing
- [x] Finalize `.bcr/metadata.json` with real maintainer info
- [x] Automate `source.json` integrity hash on release
- [x] Create release script that tags, builds tarball, computes hash
- [ ] Submit to BCR

### 10.5 Determinism
- [x] Verify builds are bit-for-bit reproducible
- [x] Create `scripts/verify_determinism.sh` that builds twice and diffs outputs
- [x] Document any known sources of non-determinism (docs/CI_CD.md)

---

## Sub-Project 11: Platform Support

**Goal:** rules_typescript works on all major platforms.

### 11.1 Windows

**Foundation (done):**
- [x] Add `windows_amd64` to `_NODE_PLATFORM_CONSTRAINTS` and `_NODEJS_REPO_PLATFORM` in `ts/private/runtime.bzl`
- [x] Add `nodejs_windows_amd64` to `use_repo` and `register_toolchains` in `MODULE.bazel`
- [x] Replace bash script in `node_modules` rule with cross-platform Node.js builder (`_BUILDER_MJS_CONTENT` in `ts/private/node_modules.bzl`):
  - Node.js `copyFileSync` + `mkdirSync` replace bash `cp` + `mkdir -p`
  - Works on Windows, Linux, and macOS identically
  - Falls back to bash script when JS runtime toolchain is not registered (POSIX only)
  - Comment fixed: paths are exec-root-relative (Bazel cwd = exec root during actions), not absolute
- [x] Eliminate duplicate bash script in `_ts_auto_node_modules` (used by `ts_test` macro):
  - Now delegates to `build_node_modules_action` from `node_modules.bzl`
  - Added `toolchains` attr to `_ts_auto_node_modules` rule
  - Made `mandatory = True`: rule is only used inside `ts_test` where Node is always available; prevents silent fallback on misconfigured setups
- [x] Export `build_node_modules_action` as a public helper so both `node_modules` and `ts_test` share the same cross-platform copy logic
- [x] Document Windows limitation clearly at the top of `ts_test.bzl`: the node_modules tree action is cross-platform, but the bash runner scripts are not

**Remaining for full Windows support (runner scripts):**
- [ ] Replace bash runner scripts in `ts_test.bzl`, `ts_binary.bzl`, `ts_dev_server.bzl`, and `npm_bin.bzl` with platform-independent alternatives; options:
  - Generate `.bat` scripts alongside `.sh` scripts and select via `select()` on `@platforms//os:windows`
  - Use a two-file wrapper: a tiny `.bat` that invokes `node wrapper.mjs`, where `wrapper.mjs` handles all the runner logic (runfiles resolution, shard splitting, etc.)
  - Implement a Bazel-native runfiles library in Node.js and generate a single `.mjs` runner (needs a native Windows launcher or `node.exe` shim on PATH)
- [ ] Build oxc-bazel for Windows (x86_64, arm64) via rules_rust cross-compilation or pre-built binaries
- [ ] Verify tsgo Windows binaries exist in the `@typescript/native-preview` npm packages
- [ ] Add `windows_amd64` to `PLATFORM_CONSTRAINTS` in `ts/private/toolchain.bzl` (needed for oxc and tsgo toolchains)
- [ ] Test on Windows CI (GitHub Actions `windows-latest` runner)
- [ ] Handle Windows path separators in generated runner scripts (backslash vs forward slash)

### 11.2 Linux ARM64
- [x] Build oxc-bazel for linux-aarch64 (built from source via rules_rust — no pre-built binary needed)
- [x] Verify tsgo linux-arm64 npm package exists (@typescript/native-preview-linux-arm64 at 7.0.0-dev.20260311.1)
- [x] Add to `PLATFORM_CONSTRAINTS` (both oxc and tsgo; sha256 checksum verified and added)
- [ ] Test on ARM64 CI (GitHub Actions ARM runner or self-hosted)

### 11.3 Container Builds
- [ ] Provide Dockerfile with all dependencies pre-installed
- [x] Document Bazel-in-Docker workflow (README: Platform Support > Container Builds)
- [x] Verify sandbox works in Docker (no privileged mode needed — documented in README)

---

## Sub-Project 12: Monorepo & Package Publishing

**Goal:** Support large monorepos with internal packages and npm-publishable packages.

### 12.1 Internal Packages
- [x] Document recommended monorepo layout (README: Monorepo Layout section)
- [x] Gazelle auto-detects packages from directory structure (existing feature)
- [x] Support `# gazelle:ts_package_boundary` for explicit boundaries (existing feature)

### 12.2 Publishable Packages
- [x] Create `ts_npm_publish` rule (ts/private/ts_npm_publish.bzl)
- [x] Inputs: ts_compile target + package.json template
- [x] Outputs: tarball ready for `npm publish` (both staging dir and .tar)
- [x] Generate package.json with correct `main`, `types`, `exports` fields (auto-filled from compiled outputs when absent from template)
- [x] Include compiled .js, .d.ts, and .js.map in tarball
- [x] Support scoped packages (@org/name) (no rule changes needed; package.json controls name)

### 12.3 Workspace References
- [ ] Parse `pnpm-workspace.yaml`
- [ ] Map `workspace:*` references to Bazel targets
- [ ] Gazelle resolves workspace package imports to Bazel labels

---

## Sub-Project 13: Path to A-Rating (rules_go parity)

**Goal:** Eliminate every friction point that prevents a TypeScript team from adopting rules_typescript as confidently as a Go team adopts rules_go.

### 13.1 pnpm Workspace Support
- [x] Parse `pnpm-workspace.yaml` in `npm_translate_lock` repository rule
- [x] Detect `workspace:*` protocol version specs in lockfile `importers` section (pnpm v6 inline and v9 block formats)
- [x] Map workspace packages to Bazel labels: `workspace:*` → `//packages/shared:shared`
- [x] Generate `alias` targets in `@npm` repo for workspace packages pointing at local Bazel targets (`@@//path:name` canonical prefix)
- [ ] Gazelle resolves `import { Foo } from "@myorg/shared"` to `//packages/shared` when the package is a workspace member
- [x] Test: `@npm//:shared` alias resolves to `@@//packages/shared:shared` (//tests/npm:workspace_consumer)

### 13.2 Invisible node_modules Naming
- [x] `vite_bundler` wrapper script auto-creates a `node_modules` symlink at the correct location instead of requiring the user to name their target `node_modules`
- [x] Remove the `basename != "node_modules"` validation from `vite/bundler.bzl`
- [x] The wrapper creates a `node_modules` symlink pointing at the actual tree artifact before invoking Vite (when name differs)
- [x] `node_modules()` rule uses `ctx.label.name` as output directory name, enabling multiple targets per package
- [x] Test: vite_bundler works with any node_modules target name (//tests/vite_bundle:vite_bundle_test via entry_vite_alt_nm)

### 13.3 JSON Imports Return Typed Data
- [x] Create `json_library` rule (separate from `asset_library`) that generates a proper `.d.ts` with the JSON structure
- [x] The `.d.ts` is: `declare const data: { readonly key: string; readonly nested: { ... } }; export default data;`
- [x] Parse the JSON file at build time using a Node.js script run via the JS runtime toolchain
- [x] Gazelle: distinguish `.json` data imports from asset imports (`json_library` for `.json`, `asset_library` for images/fonts)
- [x] Update `asset_library` to NOT handle `.json` files (handled by `json_library` instead)
- [x] Test: `import config from "./config.json"` gives typed access to properties (//tests/json:json_output_test)

### 13.4 CSS Module Imports in Node Tests
- [x] Vitest needs a CSS module mock/transform so `import styles from "./Button.module.css"` works at test runtime
- [x] Auto-generate a vitest config stub when `ts_test` has CSS module deps (detects `CssModuleInfo` in deps)
- [x] The stub installs a Vite plugin that mocks `.module.css` imports: returns a `Proxy` that yields the property name as the class name string
- [x] `deps` attr on the runner rule relaxed to accept any labels (no provider constraint), CSS module deps detected at analysis time
- [x] Test: component test that imports CSS modules passes without manual config (//tests/css_module_test:button_test)

### 13.5 import.meta.env.* Support
- [x] Add `env_vars` attr to `ts_bundle` (string_dict) for Vite-style env variable injection
- [x] Generate `define` entries mapping `import.meta.env.KEY` to their double-quoted literal values
- [x] Test: bundled output replaces `import.meta.env.VITE_API_URL` with the literal value (//tests/vite_bundle:env_vars_test)

### 13.6 Vite App-Mode Bundling
- [x] Add `mode` attr to `ts_bundle`: `"lib"` (current default) or `"app"`
- [x] In app mode, generate a Vite config with `build.rollupOptions.input` pointing at an HTML file
- [x] Accept `html` attr (label to index.html) for app mode entry point
- [x] Asset hashing is enabled by default (Vite's default behavior)
- [x] Output is a complete deployable directory (HTML + JS + CSS + assets) declared as a `declare_directory`
- [x] Test: app-mode bundle produces index.html with hashed script/link tags (//tests/vite_bundle:app_mode_test)
- [ ] In app mode, `publicDir` points at a directory containing static assets (future work)

### 13.7 vite/client Types Automatically Available
- [x] Created `ts/vite_env.d.ts` standalone shim (no vite npm dep needed) with:
  - `ImportMetaEnv` interface (MODE, BASE_URL, PROD, DEV, SSR, [key: string])
  - `ImportMeta.env` + `ImportMeta.hot` for HMR
  - Asset URL module declarations (*.svg, *.png, *.jpg, etc.)
  - CSS module declarations (*.module.css, *.module.scss, etc.)
- [x] Added `vite_types` bool attr to `ts_compile` macro that auto-prepends `@rules_typescript//ts:vite_env.d.ts`
- [x] Exported via `exports_files(["vite_env.d.ts"])` in `ts/BUILD.bazel`
- [x] Test: `env_entry.ts` uses `import.meta.env.VITE_API_URL` and `import.meta.env.PROD` with `vite_types = True` and type-checks cleanly (//tests/vite_bundle:env_entry)

### 13.8 Coverage with bazel coverage
- [x] Declare coverage output directory in `ts_test` when `coverage = True`
- [x] Configure vitest to write lcov report to a known path (via `COVERAGE_OUTPUT_FILE` env var set by `bazel coverage`)
- [x] Wire `_lcov_merger` tool for `bazel coverage --combined_report=lcov` (via `_lcov_merger` attr + `fragments = ["coverage"]`)
- [x] The coverage output is collected as a test output and available in `bazel-testlogs`
- [x] Test: `bazel coverage //tests/vitest/coverage:math_coverage_test --combined_report=lcov` produces lcov file at `bazel-out/_coverage/_coverage_report.dat`
- [x] Requires `@vitest/coverage-v8` in npm deps; documented in tests/vitest/coverage/BUILD.bazel
- [x] node_modules symlink created at RUNFILES root so Vite can resolve `@vitest/coverage-v8` in sandbox
- [x] lcov paths normalized (`SF:_main/` prefix stripped via sed) before writing to `COVERAGE_OUTPUT_FILE`

### 13.9 Zero-Prerequisites First Run
- [x] Document EXACT steps from empty directory to passing build (including Bazelisk install) — see README Requirements section
- [x] Ensure first `bazel build` works without any pre-installed tools (no pnpm, no node, no go — all fetched by Bazel)
- [x] The only prerequisite is Bazelisk (or Bazel 9+)
- [x] Test: fresh `MODULE.bazel` + source files → `bazel build //...` succeeds (`scripts/quickstart.sh` passes)
- [x] Created `scripts/quickstart.sh` — builds a minimal workspace from scratch in a temp dir and verifies compilation and type-checking

---

## Priority & Sequencing

### Phase A — Immediately Useful (weeks)
Sub-projects that make the existing system more robust:
1. **SP5: Testing maturity** (5.1 DOM testing, 5.4 custom config)
2. **SP6: npm hardening** (6.1 bin scripts, 6.4 conditional exports)
3. **SP7: Gazelle improvements** (7.1 CSS recognition, 7.5 error reporting)
4. **SP8: Migration tooling** (8.1 ESLint plugin)

### Phase B — Core Value (months)
Sub-projects that unlock real application support:
5. **SP1: Real bundler** (1.1-1.2 Vite integration)
6. **SP3: CSS support** (3.1-3.2 CSS imports and modules)
7. **SP2: Dev server** (2.1-2.3 ts_dev_server with HMR)

### Phase C — Framework Support (months)
Sub-projects that target specific frameworks:
8. **SP4: Frameworks** (4.1 Next.js, 4.2 TanStack Start)
9. **SP9: Developer experience** (9.1 IDE integration)

### Phase D — Scale & Polish (ongoing)
10. **SP10: CI/CD** (10.1-10.4)
11. **SP11: Platform support** (11.1 Windows)
12. **SP12: Monorepo patterns** (12.2 publishable packages)

---

## Honest Assessment

**What works today:**
- Pure TypeScript library monorepo with npm deps, vitest tests, hermetic builds. Good for backend services, shared libraries, CLI tools.
- Vite bundler: production-quality bundles with tree-shaking, code splitting, minification, sourcemaps.
- CSS and asset support: css_library, css_module, asset_library, and json_library rules with Gazelle integration. json_library generates fully-typed .d.ts declarations by parsing JSON at build time. CSS modules are mocked in Node.js tests automatically when ts_test detects CssModuleInfo deps.
- Gazelle: generates ts_compile, ts_test, ts_lint, css_library, css_module, asset_library, and ts_dev_server targets from TypeScript source files. Reads path aliases from tsconfig.json compilerOptions.paths/baseUrl.
- Dev server: ts_dev_server rule generates a Vite dev server runner; ibazel run provides HMR via file watching. bundler attr accepts BundlerInfo for custom dev server implementations. react_refresh = True wires @vitejs/plugin-react for React Fast Refresh (component state preserved across HMR).
- npm publishing: ts_npm_publish assembles publish-ready tarballs; auto-fills main/types/exports fields from compiled outputs.
- CI/CD: documented remote caching (BuildBuddy/EngFlow/self-hosted), remote execution, GitLab CI template, and known sources of non-determinism.

**What doesn't work today:** Any frontend application with framework-specific build pipelines (Next.js, Remix, SvelteKit) at the framework level. The examples/react-app and examples/tanstack-app compile TypeScript and run vitest tests but don't produce deployable web applications via framework-native build tools.

**Effort estimate:** Sub-project 4 (framework integration: Next.js, Remix, TanStack Start, SvelteKit) represents ~3-6 months per framework to reach production quality. Sub-project 2 HMR (ibazel protocol, React Fast Refresh, <500ms latency) is another 1-2 months. Full feature parity with the JavaScript ecosystem is a multi-year effort.
