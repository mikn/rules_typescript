# Migrating from rules_ts

`rules_typescript` is a fresh implementation, not a fork of `rules_ts` from [aspect-build](https://github.com/aspect-build/rules_ts). This page covers the differences honestly — including where `rules_ts` is the better choice.

## When to use which

**Choose `rules_ts` (Aspect) if:**
- You need full `tsc` compatibility for every TypeScript edge case, including decorator metadata
- You need Windows support today
- You want a battle-tested, BCR-published ruleset used in production by many companies
- You're already invested in `rules_js` and the Aspect ecosystem

**Choose `rules_typescript` (this) if:**
- You use Vite for bundling and dev serving
- You want Gazelle to generate all BUILD files (zero manual maintenance)
- You want the `.d.ts` compilation boundary: a body-only change recompiles nothing downstream
- You want type errors to fail the build without extra flags
- You use Remix, TanStack Start, or other Vite-based frameworks
- You want no system prerequisite but Bazelisk (no system Node, no `pnpm install`)

## Comparison

| | rules_ts (Aspect) | rules_typescript (this) |
|---|---|---|
| **Compiler** | tsc (JavaScript) | Oxc (Rust) |
| **Type-checker** | tsc | tsgo (Go port of TypeScript) |
| **Compilation boundary** | tsc project references | `.d.ts` per target |
| **Bundler** | Bring your own | Vite (first-class, built-in) |
| **Dev server** | None built-in | Vite with HMR + React Fast Refresh |
| **npm management** | rules_js (pnpm virtual store, symlinks) | Own pnpm lockfile reader; one Bazel repository per package, fetched on demand |
| **BUILD generation** | Aspect CLI (proprietary) | Gazelle (open-source, directives) |
| **Framework support** | None built-in | Remix and TanStack Start bundle through a Vite-plugin hook; Next.js has its own rule. SvelteKit and Solid Start are detected and deliberately unsupported ([why](../gazelle/overview.md#framework-detection)) |
| **Bazel deps** | rules_js + rules_nodejs | rules_nodejs, rules_rust, rules_go + gazelle, rules_shell, bazel_skylib, platforms, toolchain_utils |
| **Isolated declarations** | Not required | Not required; opt-in per package for throughput |
| **pnpm** | System install required | Hermetic, opt-in ([two lines](../guides/npm.md#hermetic-pnpm)); Linux and macOS only |
| **BCR** | Published, stable | Not published; no tag or release either — consumers pin a commit |
| **Production users** | Many companies | None yet |
| **Windows** | Supported | Not supported |

## Trade-offs: where rules_ts is better

### tsc edge-case compatibility

Oxc is not tsc. Decorator metadata (`emitDecoratorMetadata`) may behave
differently, very new TypeScript syntax can lag tsc by a few weeks, and exotic
`tsconfig.json` options may not be handled identically. Note this applies to
the JavaScript transform only — declarations come from tsgo by default, so the
`.d.ts` are what TypeScript itself would emit.

### Mature ecosystem

`rules_ts` is published on BCR, used in production by real companies, and
battle-tested at scale. `rules_typescript` has no release at all — no tag, no
BCR entry, no production users — and its API broke repeatedly in the last two
rounds of work (see the [changelog](../changelog.md)). Expect rough edges and
expect to read a changelog when you move a pin.

### npm handling

`rules_js`'s pnpm virtual store with symlinks handles more edge cases than our lockfile parser:
- Nested `node_modules` patterns
- Complex peer dependency resolution
- Hoisting edge cases

Our parser handles the common cases (scoped packages, `@types` pairing, multiple versions, npm aliases, pnpm workspaces, dependency cycles) but exotic lockfile patterns may break.

### Windows

`rules_ts` + `rules_js` work on Windows. This ruleset does not, for two upstream
reasons: no tsgo binary is published for Windows, so the default declaration
emitter has no toolchain, and hermetic pnpm has no Windows build. Runners are a
Go launcher now, but a few build-action wrappers (the Vite bundler,
`next_build`) still need a POSIX shell. See
[COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#windows).

## Trade-offs: where rules_typescript is better

### Compilation speed

Oxc is a Rust transformer with no type program, so the per-file transform is far
cheaper than tsc's. No like-for-like comparison against `rules_ts` has been run
here, so take no multiplier from this page. What has been measured is this
ruleset against itself: a rebuild of 1,000 files across 20 packages after
touching every source takes 6.3s with tsgo emitting declarations and 2.7s with
oxc emitting them and nothing type-checking
([method and caveats](../rules/ts-compile.md#cost-of-each-mode)).

### Deps that mean something

An import has to be satisfied by a direct dep: a declaration arriving through
another dep's own deps does not count, and the build says so with the label to
add. `rules_ts` passes the whole transitive closure to `tsc`, so a target can
compile against a dependency it never declared and break when an unrelated
package drops one. The cost here is that BUILD files must be accurate, which is
why Gazelle generates them from the same specifier scanner the check uses.

### Incremental boundary

Each target's `.d.ts` is a real Bazel artifact, so changing a function body
without changing its exported types leaves that artifact byte-identical and no
downstream target recompiles. This holds under either declaration emitter. It is
architecturally impossible with tsc project references, which always re-check
the dependency graph.

### Vite-native

Bundling, dev server, HMR, React Fast Refresh, framework Vite plugins — all built-in. `rules_ts` has no bundler or dev server; you wire those yourself.

### No JS-ruleset layer

There is no `rules_js` and no virtual store: the ruleset reads `pnpm-lock.yaml`
itself and declares one Bazel repository per package behind a `@npm` alias hub.
Fewer moving parts *in the JS layer* — but not a smaller dependency chain
overall. Oxc is Rust, so `rules_rust` and a Rust toolchain come along; Gazelle
is Go, so `rules_go`, `gazelle` and a Go SDK do too. `rules_ts` needs neither.
The first build pays for both toolchains.

### Gazelle

Open-source BUILD file generation with nine `# gazelle:ts_*` directives,
framework auto-detection, codegen auto-detection, and automatic
lint/dev-server/bundler target generation. `rules_ts` relies on the proprietary
Aspect CLI.

### No system prerequisites but Bazelisk

Node.js, Go and Rust are downloaded hermetically. pnpm can be too — it is
opt-in, [two lines of setup](../guides/npm.md#hermetic-pnpm) — and is needed
only to edit the lockfile, never to build or test. `rules_ts` requires a system
Node.js and pnpm.

## Migration steps

If you decide to migrate from `rules_ts`:

1. Replace `ts_project` targets with `ts_compile`
2. Replace `js_library` / `npm_link_all_packages` with the `npm` module
   extension in `MODULE.bazel`:

   ```python
   npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
   npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
   use_repo(npm, "npm")
   ```

3. Keep your `tsconfig.json` out of BUILD `deps`. Either drop it entirely and
   take the zero-config baseline, or pass it as `ts_compile(tsconfig = ...)` to
   have the generated config extend it — see
   [where compiler options come from](../rules/ts-compile.md#where-compiler-options-come-from)
4. Run `bazel run //:gazelle` to regenerate BUILD files
5. Nothing else. Missing explicit return types are fine — the default emitter
   infers them

### Key conceptual differences

**The tsconfig is generated, but yours can be the baseline.** `ts_compile`
generates a tsconfig per target — it owns `rootDirs`, `paths`, the `@types`
`files` list and the emit shape, which a user file cannot supply — and `extends` yours in place
when you pass `tsconfig`. Attributes (`lib`, `types`, `jsx_import_source`,
`compiler_options`) sit between the two.

**One Bazel repository per npm package.** `rules_ts` with `rules_js` builds a
pnpm virtual store of symlinks. Here each package is its own external
repository, fetched when something needs it, with `@npm` holding nothing but
aliases into them. Labels are unchanged from a consumer's point of view:
`@npm//:react`, `@npm//:types_react`, `@npm//:vitest_bin`.

**Isolated declarations are opt-in.** Every target starts on `declarations = "tsgo"`, which needs no annotations. Add `# gazelle:ts_declarations oxc` to a package once its exports are annotated, to move type-checking off the critical path.

**`node_modules` is automatic.** `ts_test` builds its `node_modules` tree from deps automatically. No manual `node_modules` target needed (unless overriding for specific cases). It is not pnpm's virtual store: a name's primary resolution sits flat at the top level and every other resolution of that name — another version, or the same version resolved against different peers — gets its own store directory plus a link from the dependent that resolved to it, which is the part of pnpm's layout that Node's resolution actually needs.
