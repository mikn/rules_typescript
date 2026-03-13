# Migrating from rules_ts

`rules_typescript` is a fresh implementation, not a fork of `rules_ts` from [aspect-build](https://github.com/aspect-build/rules_ts). This page covers the differences honestly — including where `rules_ts` is the better choice.

## When to use which

**Choose `rules_ts` (Aspect) if:**
- You have an existing large codebase that can't adopt isolated declarations
- You need full `tsc` compatibility for every TypeScript edge case
- You need Windows support today
- You want a battle-tested, BCR-published ruleset used in production by many companies
- You're already invested in `rules_js` and the Aspect ecosystem

**Choose `rules_typescript` (this) if:**
- You use Vite for bundling and dev serving
- You want Gazelle to generate all BUILD files (zero manual maintenance)
- You want sub-second incremental rebuilds via the `.d.ts` compilation boundary
- You're starting a new project or can adopt isolated declarations
- You use Remix, TanStack Start, or other Vite-based frameworks
- You want zero system prerequisites (no Node, no pnpm install needed)

## Comparison

| | rules_ts (Aspect) | rules_typescript (this) |
|---|---|---|
| **Compiler** | tsc (JavaScript) | Oxc (Rust) — 10-100x faster per file |
| **Type-checker** | tsc | tsgo (Go port of TypeScript) |
| **Compilation boundary** | tsc project references | `.d.ts` via isolated declarations |
| **Bundler** | Bring your own | Vite (first-class, built-in) |
| **Dev server** | None built-in | Vite with HMR + React Fast Refresh |
| **npm management** | rules_js (pnpm virtual store, symlinks) | Own pnpm lockfile parser (simpler) |
| **BUILD generation** | Aspect CLI (proprietary) | Gazelle (open-source, directives) |
| **Framework support** | None built-in | Remix, TanStack Start, SvelteKit, Solid Start |
| **Dependencies** | rules_js + rules_nodejs | rules_nodejs only |
| **Isolated declarations** | Not required | Required (or opt-out per package) |
| **pnpm** | System install required | Hermetic (`bazel run //:pnpm`) |
| **BCR** | Published, stable | Pre-release (v0.1.0) |
| **Production users** | Many companies | None yet |
| **Windows** | Supported | Partial (node_modules action only) |

## Trade-offs: where rules_ts is better

### Full tsc compatibility

`tsc` IS TypeScript. Every compiler flag, every `tsconfig.json` option, every edge case works exactly as documented. Our Oxc + tsgo stack covers the vast majority of TypeScript, but:

- Decorator metadata (`emitDecoratorMetadata`) may behave differently in oxc
- Very new TypeScript syntax may lag behind tsc by a few weeks until oxc implements it
- Some exotic `tsconfig.json` options (e.g., `verbatimModuleSyntax` edge cases) may not be handled identically

For most projects this doesn't matter. For projects with complex decorator patterns or bleeding-edge TypeScript features, `tsc` is safer.

### No isolated declarations requirement

With `rules_ts`, existing codebases work immediately — no code changes needed. With `rules_typescript`, you either:

1. Add explicit return types to all exports (the intended path — enables the fast `.d.ts` boundary)
2. Use `# gazelle:ts_isolated_declarations false` (escape hatch — everything compiles but you lose the incremental boundary benefit)

Option 2 gets you running immediately, but without the speed advantage that justifies the ruleset. The migration from option 2 to option 1 is gradual (package by package, aided by the ESLint plugin) but non-trivial for large codebases.

### Mature ecosystem

`rules_ts` is published on BCR, used in production by real companies, and battle-tested at scale. `rules_typescript` is v0.1.0. Expect rough edges.

### npm handling

`rules_js`'s pnpm virtual store with symlinks handles more edge cases than our lockfile parser:
- Nested `node_modules` patterns
- Complex peer dependency resolution
- Hoisting edge cases

Our parser handles the common cases (scoped packages, `@types` pairing, multiple versions, npm aliases, pnpm workspaces, dependency cycles) but exotic lockfile patterns may break.

### Windows

`rules_ts` + `rules_js` work on Windows. Our `node_modules` build action is cross-platform (Node.js script), but test runners, dev server, and binary runners are still bash scripts.

## Trade-offs: where rules_typescript is better

### Compilation speed

Oxc (Rust) compiles TypeScript 10-100x faster per file than tsc. For a 500-file project, clean compilation is seconds, not minutes. Incremental rebuilds (touching one file) are sub-second.

### Incremental boundary

The `.d.ts` isolated declarations boundary means changing a function body without changing its return type does NOT recompile any downstream package. This is architecturally impossible with tsc project references (which always re-check the dependency graph).

### Vite-native

Bundling, dev server, HMR, React Fast Refresh, framework Vite plugins — all built-in. `rules_ts` has no bundler or dev server; you wire those yourself.

### Simpler dependency chain

No `rules_js`. No complex virtual store. One `pnpm-lock.yaml` → one `@npm` repository.

### Gazelle

Open-source BUILD file generation with 10 directives, framework auto-detection, codegen auto-detection, and automatic lint/dev-server/bundler target generation. `rules_ts` relies on the proprietary Aspect CLI.

### Zero system prerequisites

Only Bazelisk needed. Node.js, pnpm, Go, Rust are all downloaded hermetically. `rules_ts` requires system Node.js and pnpm.

## Migration steps

If you decide to migrate from `rules_ts`:

1. Replace `ts_project` targets with `ts_compile`
2. Replace `js_library` / `npm_link_all_packages` with `npm_translate_lock`
3. Remove `tsconfig.json` from BUILD deps — `ts_compile` generates its tsconfig internally
4. Run `bazel run //:gazelle` to regenerate BUILD files
5. If your codebase lacks explicit return types, add `# gazelle:ts_isolated_declarations false` to your root BUILD.bazel first (see [Path B](quickstart.md#path-b-existing-project)), then migrate package by package

### Key conceptual differences

**No `tsconfig.json` in BUILD files.** `ts_compile` generates a tsconfig per target internally using `rootDirs`, `moduleResolution: "Bundler"`, and `paths` entries from deps.

**One `@npm` repository.** `rules_ts` with `rules_js` creates per-package virtual stores. We create a single `@npm` repo. Labels: `@npm//:react`, `@npm//:types_react`.

**Isolated declarations are opt-out.** New projects start with `isolated_declarations = True`. Existing projects opt out with `# gazelle:ts_isolated_declarations false`, then gradually opt packages back in.

**`node_modules` is automatic.** `ts_test` builds its `node_modules` tree from deps automatically. No manual `node_modules` target needed (unless overriding for specific cases).
