# rules_typescript: Project Plan

## Thesis

TypeScript on Bazel should feel like Go on Bazel. Developers write `.ts` files, never touch BUILD files, and get hermetic cached builds with sub-second incremental feedback. The architectural keystone is **isolated declarations**: by enforcing explicit return types on exports, `.d.ts` emit becomes a per-file syntactic transform (no type checker), which makes the compilation model structurally identical to Go's per-package model and unlocks fine-grained Bazel targets.

---

## Phase 0: Foundation — Oxc transform as a Bazel action

**Goal:** A minimal Bazel rule that compiles a single `ts_compile` target from `.ts` sources to `.js` + `.d.ts` using Oxc, with isolated declarations.

### 0.1 — Oxc CLI wrapper binary

Build a small Rust CLI (`oxc-bazel`) that:
- Accepts: a list of `.ts`/`.tsx` input paths, compiler options (target, jsx mode, isolated declarations), output directory
- Produces: `.js`, `.js.map`, `.d.ts` per input file
- Uses `oxc_transformer` crate directly (not the npm package)
- Exits after processing — no worker protocol

**Verification:**
- [ ] `oxc-bazel --files src/a.ts src/b.tsx --out-dir out/ --target es2022 --jsx react-jsx --isolated-declarations` produces correct `.js` and `.d.ts` for each input
- [ ] Output `.d.ts` files are identical to `tsc --isolatedDeclarations --emitDeclarationOnly` output on a 50-file test corpus
- [ ] Process start-to-exit on 100 files is under 50ms on a modern machine

### 0.2 — Bazel rule: `ts_compile`

A Starlark rule that wraps `oxc-bazel` as a regular Bazel action.

```python
ts_compile(
    name = "button",
    srcs = ["Button.tsx", "Button.module.css.d.ts"],
    deps = ["//components/icon"],
)
```

Providers:
- `JsInfo`: `.js` files + transitive `.js` from deps
- `TsDeclarationInfo`: `.d.ts` files + transitive `.d.ts` from deps
- `DefaultInfo`: all outputs

Deps are consumed via their `TsDeclarationInfo` — the rule passes dep `.d.ts` files to `oxc-bazel` so that import resolution works (Oxc needs to see declarations to resolve types for isolated declarations emit).

**Verification:**
- [ ] `bazel build //test:a` where `a` depends on `b` produces correct `.js` and `.d.ts`
- [ ] Changing a file in `b` that doesn't change its `.d.ts` output does NOT trigger recompilation of `a` (Bazel's content-based caching via `.d.ts` as the interface artifact)
- [ ] `bazel build //...` on a synthetic 200-target diamond dependency graph completes in under 10s clean, under 1s on single-leaf change
- [ ] Action count equals number of targets (no persistent workers, no extra actions)

### 0.3 — Toolchain registration

Register `oxc-bazel` as a Bazel toolchain with platform-specific binaries (linux-x64, darwin-arm64, darwin-x64).

**Verification:**
- [ ] `bazel build //... --platforms=@platforms//os:linux` selects the linux binary
- [ ] Toolchain is resolved from a `MODULE.bazel` dependency, not a local path

---

## Phase 1: Type-checking — tsgo as a validation action

**Goal:** Type-checking runs in parallel with compilation, does not block downstream targets, and uses tsgo (TypeScript 7 native binary).

### 1.1 — tsconfig generation

A Starlark rule (`ts_config_gen`) that generates a `tsconfig.json` from the Bazel target graph:
- `composite: true`
- `noEmit: true`
- `strict: true`
- `isolatedDeclarations: true`
- `references` populated from `deps` labels mapped to their output directories in `bazel-bin/`
- `include` populated from `srcs`

The generated tsconfig is a Bazel action output, never a source file.

**Verification:**
- [ ] Generated `tsconfig.json` for a target with 3 deps contains correct `references` paths
- [ ] Running `tsgo --build` manually on the generated tsconfig produces no spurious errors on a known-good codebase
- [ ] No hand-written `tsconfig.json` exists anywhere in the test corpus

### 1.2 — Bazel rule: `ts_check`

A validation action attached to each `ts_compile` target. Runs `tsgo --build` against the generated tsconfig. Uses the Validations Output Group so it runs unconditionally on `bazel build` but does NOT block downstream compilation.

**Verification:**
- [ ] `bazel build //app:bundle` succeeds even if a leaf target has a type error (compilation completes, validation fails, build fails at the end)
- [ ] `bazel build //lib:button` with a type error reports the error in output
- [ ] Type-check action takes `.d.ts` files from deps as inputs, not source `.ts` files from deps (proving the compilation boundary is `.d.ts`)
- [ ] `--build` mode incremental: changing a comment in a source file does NOT re-run type-check for dependent targets (`.tsbuildinfo` cache hit)

### 1.3 — tsgo toolchain

Register tsgo as a Bazel toolchain. Download platform-specific binaries from `@typescript/native-preview` npm package (extract the Go binary from the package).

**Verification:**
- [ ] `bazel build //...` on a 500-target corpus: total type-check wall time is under 15s (parallelized across cores)
- [ ] tsgo binary is fetched by Bazel's repository rules, not checked into the repo

---

## Phase 2: npm dependencies — lockfile-derived repository

**Goal:** npm packages are Bazel targets with `JsInfo` and `TsDeclarationInfo` providers, derived from pnpm lockfile.

### 2.1 — Repository rule: `npm_translate_lock`

Parses `pnpm-lock.yaml` and generates:
- One external repository per npm package
- A BUILD file per package exposing `ts_npm_package` targets
- Correct `deps` edges from the lockfile's dependency graph
- `.d.ts` files from `@types/*` packages automatically paired with their untyped counterparts

Uses Bazel's downloader for tarball fetches (content-addressed cache).

**Verification:**
- [ ] `bazel query @npm//react:react` resolves to a valid target
- [ ] `@npm//react:react` provides `JsInfo` (runtime `.js`) and `TsDeclarationInfo` (from `@types/react`)
- [ ] Adding a new dep to `package.json` + `pnpm install` + `bazel build //...` works without manual intervention
- [ ] `bazel fetch //...` downloads only packages reachable from requested targets (lazy fetching)

### 2.2 — Runtime `node_modules` generation

A Bazel action that produces a pnpm-style symlink `node_modules` tree from the resolved npm targets. Used only at runtime (test execution, dev server), not during compilation.

**Verification:**
- [ ] A `ts_test` target that `import("react")` at runtime resolves correctly via the generated `node_modules`
- [ ] The `node_modules` tree is a Bazel action output (cacheable, hermetic), not produced by running `pnpm install` inside the sandbox

---

## Phase 3: Gazelle extension — BUILD file inference

**Goal:** `bazel run //:gazelle` generates all BUILD files from source, with zero manual authoring.

### 3.1 — Import extraction

A Go library that calls `oxc-parser` (via C FFI from the Rust crate) to extract import specifiers from `.ts`/`.tsx` files. Returns a list of `(file, import_specifier)` pairs.

**Verification:**
- [ ] Correctly extracts: relative imports, bare specifiers, dynamic imports, type-only imports, re-exports
- [ ] Processes 10,000 files in under 2s
- [ ] Does NOT extract imports from comments or strings

### 3.2 — Import resolution

A Go library that calls `oxc_resolver` (via C FFI) to resolve import specifiers to file paths, then maps file paths to Bazel labels using workspace-relative path conventions.

Resolution strategy:
- Relative imports → file in same or adjacent directory → Bazel label in same or sibling package
- Bare specifiers → lookup in lockfile-derived mapping → `@npm//` label
- Path aliases → parsed from a `gazelle_ts.json` config file (NOT tsconfig) → Bazel labels

**Verification:**
- [ ] `import "./utils"` in `src/app/page.tsx` resolves to `//src/app:app` or `//src/app/utils:utils` depending on directory structure
- [ ] `import "react"` resolves to `@npm//react`
- [ ] `import "@/components/Button"` with alias `@/ → src/` resolves to `//src/components/button`
- [ ] Resolution handles `.ts`, `.tsx`, `.js`, `/index.ts` suffixes correctly

### 3.3 — Package boundary heuristic

Default rules for inferring target boundaries:
- A directory with `index.ts` or `index.tsx` → one `ts_compile` target, `index` defines the public API
- Files matching `*.test.ts` or `*.spec.ts` → `ts_test` target
- Files without a parent `index.ts` → rolled into parent directory's target
- `# gazelle:ts_package_boundary` directive for manual override

**Verification:**
- [ ] On a 50-directory TanStack Start app, `gazelle` produces BUILD files that result in a passing `bazel build //...` with zero manual edits
- [ ] Adding a new `.ts` file and re-running `gazelle` correctly adds it to an existing target or creates a new one
- [ ] Deleting a file and re-running `gazelle` removes it

### 3.4 — Framework plugin: TanStack Start

A Gazelle plugin that recognizes TanStack Start conventions:
- `routes/` directory tree → route targets
- `__root.tsx` → root layout
- `routeTree.gen.ts` → generated, excluded from source targets

**Verification:**
- [ ] A standard TanStack Start starter project gets correct BUILD files from `gazelle` with zero manual edits
- [ ] Route targets have correct deps on shared components

---

## Phase 4: Test and binary rules

**Goal:** `ts_test` and `ts_binary` rules that consume `ts_compile` outputs.

### 4.1 — `ts_test` rule

Runs vitest in Bazel's test sandbox. Consumes `.js` outputs from `ts_compile` deps + runtime `node_modules`.

```python
ts_test(
    name = "button_test",
    srcs = ["Button.test.tsx"],
    deps = ["//components/button"],
)
```

**Verification:**
- [ ] `bazel test //components/button:button_test` runs vitest, reports pass/fail via Bazel test protocol
- [ ] Test caching works: re-running without changes is instant (cached pass)
- [ ] Test sharding works for targets with multiple test files

### 4.2 — `ts_binary` rule

Produces a bundled output using Rolldown (or Vite build mode). Takes `.js` outputs from the transitive `ts_compile` graph and bundles them.

```python
ts_binary(
    name = "app",
    entry_point = "//src/app:app",
    # Optional: chunk splitting config
)
```

**Verification:**
- [ ] `bazel build //app` produces a working bundled JS application
- [ ] Bundle action is cached: only re-runs if any transitive `.js` input changed
- [ ] Source maps chain correctly through oxc transform → rolldown bundle

---

## Phase 5: Dev server integration

**Goal:** `bazel run //app:dev` starts a Vite dev server with HMR, using Bazel-compiled outputs.

### 5.1 — Vite plugin: `vite-plugin-bazel`

A Vite plugin that:
- Serves `.js` files from `bazel-bin/` output tree
- Watches for ibazel rebuilds and triggers Vite HMR on changed modules
- Uses the generated `node_modules` for npm dep resolution at runtime

**Verification:**
- [ ] `ibazel run //app:dev` starts a working dev server
- [ ] Editing a source `.ts` file triggers: ibazel rebuild (oxc transform) → Vite HMR → browser update, total latency under 500ms
- [ ] No transforms happen inside Vite — all JS is pre-compiled by Bazel

---

## Phase 6: Validation and migration tooling

**Goal:** A codebase can adopt `rules_ts_v2` incrementally.

### 6.1 — Isolated declarations migration lint rule

An oxlint rule (or eslint rule) that reports all exported symbols missing explicit type annotations. Provides auto-fix where the type is inferrable from the AST.

**Verification:**
- [ ] Running on a codebase with 1000 exports, correctly identifies all violations
- [ ] Auto-fix covers > 80% of violations (return types, variable declarations)
- [ ] Remaining manual fixes are flagged with clear error messages

### 6.2 — `gazelle` dry-run diff mode

`bazel run //:gazelle -- --mode=diff` shows what BUILD files would be generated/changed without writing them. For incremental adoption and CI validation.

**Verification:**
- [ ] CI can assert "BUILD files are up to date" by checking that diff mode produces no output

---

## Success criteria (end-to-end)

On a real TanStack Start application with 500+ source files and 100+ npm dependencies:

| Metric | Target |
|---|---|
| `gazelle` generates all BUILD files from source | Zero manual BUILD file edits |
| `bazel build //...` clean build | Under 30s |
| `bazel build //...` single-leaf change | Under 2s |
| `bazel test //...` single test change | Under 3s |
| `ibazel run //app:dev` edit-to-HMR latency | Under 500ms |
| Type error reported on `bazel build` | Yes, via validation action |
| Type error blocks downstream compilation | No |
| Hand-written `tsconfig.json` files in repo | Zero |
| Hand-written `BUILD.bazel` files for TS targets | Zero (gazelle-generated only) |
| npm dependency management | `pnpm install` + `gazelle` only |

---

## Dependency map

```
Phase 0 (oxc action)
  │
  ├──→ Phase 1 (tsgo validation) — depends on Phase 0 providers
  │
  ├──→ Phase 2 (npm deps) — independent of Phase 1
  │
  └──→ Phase 3 (gazelle) — depends on Phase 0 rule API being stable
          │
          ├──→ Phase 4 (test/binary) — depends on Phase 0 + Phase 2
          │
          └──→ Phase 5 (dev server) — depends on Phase 0 + Phase 2 + Phase 4

Phase 6 (migration tooling) — can run in parallel from Phase 0 onward
```

Phases 0-2 are the foundation and should be built sequentially. Phases 3-5 can overlap once the rule API from Phase 0 stabilizes. Phase 6 is continuous.
