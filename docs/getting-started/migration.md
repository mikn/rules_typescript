# Migrating from rules_ts

`rules_typescript` is a fresh implementation, not a fork of `rules_ts` from
[aspect-build](https://github.com/aspect-build/rules_ts). `rules_ts` has a
release, production users and Windows support. This has none of the three.

## When to Use Which

**Choose `rules_ts` (Aspect) if:**
- You need full `tsc` compatibility for every TypeScript edge case, including decorator metadata
- You need Windows support today
- You want a BCR-published ruleset with production users
- You're already invested in `rules_js` and the Aspect ecosystem

**Choose `rules_typescript` (this) if:**
- You dev-serve with Vite
- You want Gazelle to generate the BUILD files, and can live with pinning the
  occasional hand-narrowed attribute with `# keep`
- You want the `.d.ts` compilation boundary: a body-only change recompiles nothing downstream
- You want type errors to fail the build without extra flags
- You want no system prerequisite but Bazelisk (no system Node, no `pnpm install`; a pnpm only to write the first lockfile)

## Comparison

| | rules_ts (Aspect) | rules_typescript (this) |
|---|---|---|
| **Compiler** | tsc (JavaScript) | Oxc (Rust) for an ES-module program, tsgo (Go) for a CommonJS-shaped one |
| **Type-checker** | tsc | tsgo (Go port of TypeScript) |
| **Compilation boundary** | tsc project references | `.d.ts` per target |
| **Bundler** | Bring your own | Bring your own, through `BundlerInfo` on `ts_binary` |
| **Dev server** | None built-in | Vite, with HMR and React Fast Refresh; any `DevServerInfo` rule per target |
| **npm management** | rules_js (pnpm virtual store, symlinks) | Own pnpm lockfile reader: a `pnpm-lock.yaml` is required, npm and yarn lockfiles are not read; one Bazel repository per package, fetched on demand; pnpm's virtual store as Bazel artifacts |
| **BUILD generation** | Aspect CLI (proprietary) | Gazelle (open-source; one package per `tsconfig.json`) |
| **Framework support** | None built-in | None built-in; a framework's Vite plugin runs in the dev server through `vite_config` |
| **Bazel deps** | rules_js + rules_nodejs | rules_nodejs, rules_rust, rules_go + gazelle, rules_shell, bazel_skylib, platforms, toolchain_utils |
| **Isolated declarations** | Not required | Not required; opt-in per package for throughput |
| **pnpm** | System install required | Hermetic, always downloaded ([hermetic pnpm](../guides/npm.md#hermetic-pnpm)); Linux and macOS only |
| **BCR** | Published, stable | Not published; no module release either (the `tools-v1` tag releases the Go tools alone), so consumers pin a commit |
| **Production users** | Many companies | None yet |
| **Windows** | Supported | Not supported |

## Where rules_ts Is Better

### tsc Edge-Case Compatibility

Oxc is not tsc. Decorator metadata (`emitDecoratorMetadata`) may behave
differently, very new TypeScript syntax can lag tsc by a few weeks, and exotic
`tsconfig.json` options may not be handled identically. This applies to the
JavaScript transform only. Declarations come from tsgo by default, so the `.d.ts`
are what TypeScript itself would emit.

### Mature Ecosystem

`rules_ts` is published on the BCR and used in production. `rules_typescript`
has no tag, no BCR entry and no production users, and its API has broken
repeatedly pre-1.0. Read the [changelog](../changelog.md) when you move a pin.

### npm Handling

`rules_js`'s lockfile reader has seen more lockfiles than ours. Ours handles
scoped packages, `@types` pairing, multiple versions, peer sets (one store tree
per snapshot), npm aliases, pnpm workspaces, dependency cycles and pnpm's
hidden hoist; an exotic lockfile pattern may still break it.

### Windows

`rules_ts` + `rules_js` work on Windows. Windows is not supported here right
now; it may be considered in the future. See
[Compatibility](../compatibility.md#windows).

## Where rules_typescript Is Better

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
one. BUILD files must therefore be accurate; Gazelle writes them from the same
tsgo listing the check reads.

### Incremental Boundary

Each target's `.d.ts` is a real Bazel artifact, so changing a function body
without changing its exported types leaves that artifact byte-identical and no
downstream target recompiles. This holds under either declaration emitter.
`tsc -b` with project references always re-checks the dependency graph.

### Vite-Native

Dev serving, HMR, React Fast Refresh and framework Vite plugins are built in,
and all of them go through one generated Vite config. Vite runs it, or any rule
returning `DevServerInfo` does: `ts_dev_server(server = ...)` is a per-target
choice. `rules_ts` has no dev server; you wire that yourself.

### No JS-Ruleset Layer

There is no `rules_js`: the ruleset reads `pnpm-lock.yaml` itself, declares
one Bazel repository per package behind a `@npm` alias hub, and builds pnpm's
virtual store from them as Bazel artifacts.
That is fewer moving parts in the JS layer and a larger dependency chain overall.
Oxc is Rust, so `rules_rust` and a Rust toolchain come along, and the first
build pays for it. The ruleset's Go tools arrive as the binaries of its tools
release; Gazelle is the one Go a consumer compiles, under `bazel run
//:gazelle` alone, which is when `rules_go`'s SDK is fetched. `rules_ts` needs
neither toolchain.

### Gazelle

Open-source BUILD file generation: one package per `tsconfig.json`, its deps
from tsgo's own listing of the program, and no directive of its own. `rules_ts` relies on the proprietary Aspect CLI.

### System Prerequisites

Bazelisk is the only one. Node.js, Rust, the Go tools and, for Gazelle, a Go
SDK are downloaded hermetically. pnpm
can be too, in [two lines of setup](../guides/npm.md#hermetic-pnpm), and is
needed only to edit the lockfile, never to build or test. The first lockfile is
the exception: the extension reads `pnpm-lock.yaml` while `MODULE.bazel` is
evaluated, so the hermetic pnpm is not runnable before the file exists, and a
pnpm of your own writes it. `rules_ts` requires a system Node.js and pnpm.

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

   `"pnpm"` is the hermetic pnpm the `ts_pnpm` and `ts_add_package` targets
   you write beside the lockfile run, and the one that installs the checkout
   Gazelle lists. See [Setup](../guides/npm.md#setup).

3. Leave your `tsconfig.json` where it is, under its name. It is the package:
   step 4 writes `ts_config(name = "tsconfig", src = "tsconfig.json")` beside
   it, a `ts_compile` over what it lists with `tsconfig = ":tsconfig"`, and a
   `ts_test` over its test files, so the generated config extends yours; see
   [where compiler options come from](../rules/ts-compile.md#where-compiler-options-come-from).
   A repository of several projects has one `tsconfig.json` per project, each
   its own package. Deleting or renaming the file leaves no package: Gazelle
   reads only a file named `tsconfig.json`. Delete `baseUrl` from it;
   tsgo rejects the key
   ([Option 'baseUrl' has been removed](../guides/troubleshooting.md#option-baseurl-has-been-removed)).
   If you also run `ts_refresh_tsconfig`, which
   [overwrites the file at `tsconfig`](ide-setup.md#setup), point that at
   another name and extend it from yours
   ([Extending the generated file](ide-setup.md#extending-the-generated-file))
4. Run `bazel run //:gazelle` to regenerate BUILD files
5. Leave `compilerOptions.paths` alone. The rule reads it from your file and
   rewrites each value to its source and `bazel-bin` twins; tsgo resolves an
   aliased import through the same entries when Gazelle lists the program, and
   the package that owns the file it landed on goes into `deps`. No attribute
   repeats the alias; the
   [quickstart](quickstart.md#path-b-existing-project) shows the shape
6. Nothing else. Missing explicit return types are fine; the default emitter
   infers them

### Key Conceptual Differences

**The tsconfig is generated, and yours is what it extends.** `ts_compile` writes
a tsconfig per target that extends the ruleset's baseline and then your file,
referenced where it lives, and sets over both what Bazel owns: `rootDirs`, the
emit shape, `include` and `files`. It rewrites each `paths` value to its source
and `bazel-bin` twins and lists a path-shaped `types` entry as a root file.
Every other option is the file's; the rule
has no attribute for any of them.

**One Bazel repository per npm package.** Both build pnpm's virtual store:
`rules_js` from one repository holding every package, this ruleset from one
repository per package, fetched when something needs it, with `@npm` holding
aliases into them and each lockfile's package declaring the store
(`npm_virtual_store`). Consumer labels are unchanged: `@npm//:react`,
`@npm//:types_react`, `@npm//:vitest_bin`.

**Isolated declarations are a build flag.** `--//ts:declarations=tsgo`, the
default, needs no annotations; `--//ts:declarations=oxc` emits every `.d.ts`
from the annotated source alone, taking type-checking off the critical path.

**`node_modules` is the importer's.** Every lockfile importer's package holds
a `node_modules` target, its declared packages linked into the store, and
`ts_compile` and `ts_test` name the nearest one and resolve each npm dep along
it and its `parent`s, pnpm's walk-up; `ts_dev_server` and `ts_codegen` take
the same target. The layout is pnpm's: one store tree per resolution -- name,
version and peer set -- with its own edges beside it, and an importer's links
at the top.
