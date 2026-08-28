# Migrating from rules_ts

`rules_typescript` is a fresh implementation, not a fork of `rules_ts` from
[aspect-build](https://github.com/aspect-build/rules_ts). `rules_ts` has a
release, production users and Windows support. This has none of the three.

## When to Use Which

**Choose `rules_ts` (Aspect) if:**
- You need full `tsc` compatibility for every TypeScript edge case, including decorator metadata
- You need Windows support today
- You want a battle-tested, BCR-published ruleset used in production by many companies
- You're already invested in `rules_js` and the Aspect ecosystem

**Choose `rules_typescript` (this) if:**
- You bundle with Vite, and dev-serve with Vite or oj
- You want Gazelle to generate the BUILD files, and can live with pinning the
  occasional hand-narrowed attribute with `# keep`
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
| **Bundler** | Bring your own | Vite, through `ts_bundle`; any other bundler through `BundlerInfo` |
| **Dev server** | None built-in | Vite or oj, chosen per target, with HMR and React Fast Refresh |
| **npm management** | rules_js (pnpm virtual store, symlinks) | Own pnpm lockfile reader; one Bazel repository per package, fetched on demand |
| **BUILD generation** | Aspect CLI (proprietary) | Gazelle (open-source, directives) |
| **Framework support** | None built-in | TanStack Start bundles through a Vite-plugin hook; Remix, SvelteKit and Next.js each have a rule of their own. Solid Start is detected and deliberately unsupported (see [framework detection](../gazelle/overview.md#framework-detection)) |
| **Bazel deps** | rules_js + rules_nodejs | rules_nodejs, rules_rust, rules_go + gazelle, rules_shell, bazel_skylib, platforms, toolchain_utils |
| **Isolated declarations** | Not required | Not required; opt-in per package for throughput |
| **pnpm** | System install required | Hermetic, opt-in ([hermetic pnpm](../guides/npm.md#hermetic-pnpm)); Linux and macOS only |
| **BCR** | Published, stable | Not published; no tag or release either — consumers pin a commit |
| **Production users** | Many companies | None yet |
| **Windows** | Supported | Not supported |

## Trade-offs: where rules_ts is better

### tsc Edge-Case Compatibility

Oxc is not tsc. Decorator metadata (`emitDecoratorMetadata`) may behave
differently, very new TypeScript syntax can lag tsc by a few weeks, and exotic
`tsconfig.json` options may not be handled identically. This applies to the
JavaScript transform only. Declarations come from tsgo by default, so the `.d.ts`
are what TypeScript itself would emit.

### Mature Ecosystem

`rules_ts` is published on BCR, used in production by real companies, and
battle-tested at scale. `rules_typescript` has no tag, no BCR entry and no
production users, and its API has broken repeatedly pre-1.0. Expect rough edges,
and read the [changelog](../changelog.md) when you move a pin.

### npm Handling

`rules_js`'s pnpm virtual store with symlinks handles more edge cases than our lockfile parser:
- Nested `node_modules` patterns
- Complex peer dependency resolution
- Hoisting edge cases

Our parser handles the common cases (scoped packages, `@types` pairing, multiple versions, npm aliases, pnpm workspaces, dependency cycles) but exotic lockfile patterns may break.

### Windows

`rules_ts` + `rules_js` work on Windows. Windows is not supported here right
now; it may be considered in the future. See
[Compatibility](../compatibility.md#windows).

## Trade-offs: where rules_typescript is better

### Compilation Speed

Oxc is a Rust transformer with no type program, so the per-file transform is far
cheaper than tsc's. No like-for-like comparison against `rules_ts` has been run.
Measured against this ruleset itself: a rebuild of 1,000 files across 20 packages
after touching every source takes 6.3s with tsgo emitting declarations and 2.7s
with oxc emitting them and nothing type-checking. See
[Cost of each mode](../rules/ts-compile.md#cost-of-each-mode) for method and
caveats.

### Direct Dependencies

An import has to be satisfied by a direct dep. A declaration arriving through
another dep's own deps does not count, and the error names the label to add.
`rules_ts` passes the whole transitive closure to `tsc`, so a target can compile
against a dependency it never declared and break when an unrelated package drops
one. BUILD files must therefore be accurate; Gazelle generates them from the
same specifier scanner the check uses.

### Incremental Boundary

Each target's `.d.ts` is a real Bazel artifact, so changing a function body
without changing its exported types leaves that artifact byte-identical and no
downstream target recompiles. This holds under either declaration emitter. It is
architecturally impossible with tsc project references, which always re-check
the dependency graph.

### Vite-native

Bundling, dev serving, HMR, React Fast Refresh and framework Vite plugins are
built in, and all of them go through one generated Vite config. Vite runs it, or
oj does: `ts_dev_server(server = ...)` is a per-target choice. `rules_ts` has no
bundler and no dev server; you wire those yourself.

### No JS-ruleset Layer

There is no `rules_js` and no virtual store: the ruleset reads `pnpm-lock.yaml`
itself and declares one Bazel repository per package behind a `@npm` alias hub.
That is fewer moving parts in the JS layer and a larger dependency chain overall.
Oxc is Rust, so `rules_rust` and a Rust toolchain come along; Gazelle is Go, so
`rules_go`, `gazelle` and a Go SDK do too. The first build pays for both
toolchains. `rules_ts` needs neither.

### Gazelle

Open-source BUILD file generation with eleven `# gazelle:ts_*` directives,
framework auto-detection, codegen auto-detection, and automatic
lint/dev-server/bundler target generation. `rules_ts` relies on the proprietary
Aspect CLI.

### System Prerequisites

Bazelisk is the only one. Node.js, Go and Rust are downloaded hermetically. pnpm
can be too, in [two lines of setup](../guides/npm.md#hermetic-pnpm), and is
needed only to edit the lockfile, never to build or test. `rules_ts` requires a
system Node.js and pnpm.

## Migration Steps

If you decide to migrate from `rules_ts`:

1. Replace `ts_project` targets with `ts_compile`
2. Replace `js_library` / `npm_link_all_packages` with the `npm` module
   extension in `MODULE.bazel`:

   ```python
   npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
   npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
   use_repo(npm, "npm", "pnpm")
   ```

   `"pnpm"` goes in even if you never run pnpm through Bazel: Gazelle writes a
   `ts_pnpm` and a `ts_add_package` target beside the lockfile, and both name
   `@pnpm`. See [Setup](../guides/npm.md#setup).

3. Keep your `tsconfig.json` out of BUILD `deps`. Either drop it entirely and
   take the zero-config baseline, or pass it as `ts_compile(tsconfig = ...)` to
   have the generated config extend it — see
   [where compiler options come from](../rules/ts-compile.md#where-compiler-options-come-from).
   Rename it first if you also run `ts_refresh_tsconfig`, which
   [overwrites the root `tsconfig.json` in full](ide-setup.md#setup)
4. Run `bazel run //:gazelle` to regenerate BUILD files
5. Move cross-package `compilerOptions.paths` aliases to `module_name`. Gazelle
   turns a `paths` entry into a `path_aliases` attr, which `ts_compile` accepts
   only for files the same target stages, so `"@/*": ["src/*"]` across two
   packages fails analysis. The
   [quickstart](quickstart.md#path-b-existing-project) has the edit; your sources
   keep importing `@/lib/math` unchanged
6. Nothing else. Missing explicit return types are fine — the default emitter
   infers them

### Key Conceptual Differences

**The tsconfig is generated, but yours can be the baseline.** `ts_compile`
generates a tsconfig per target and owns `rootDirs`, `paths`, the `@types`
`files` list and the emit shape, none of which a user file can supply. Pass
`tsconfig` and the generated config `extends` yours in place. Attributes (`lib`,
`types`, `jsx_import_source`, `compiler_options`) sit between the two.

**One Bazel repository per npm package.** `rules_ts` with `rules_js` builds a
pnpm virtual store of symlinks. Here each package is its own external
repository, fetched when something needs it, and `@npm` holds only aliases into
them. Consumer labels are unchanged: `@npm//:react`, `@npm//:types_react`,
`@npm//:vitest_bin`.

**Isolated declarations are opt-in.** Every target starts on
`declarations = "tsgo"`, which needs no annotations. Add
`# gazelle:ts_declarations oxc` to a package once its exports are annotated, to
move type-checking off the critical path.

**`node_modules` is automatic.** `ts_test` builds its `node_modules` tree from
deps; a manual `node_modules` target is needed only to override a specific case.
The layout is its own, not pnpm's virtual store. A name's primary resolution sits
flat at the top level. Every other resolution of that name — another version, or
the same version resolved against different peers — gets its own store directory
plus a link from the dependent that resolved to it, which is the part of pnpm's
layout Node's resolution needs.
