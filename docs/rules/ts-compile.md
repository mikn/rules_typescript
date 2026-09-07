# ts_compile

Compiles TypeScript with Oxc and checks it with tsgo, which emits the `.d.ts`.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    deps = ["//other/package", "@npm//:zod"],
    tsconfig = "tsconfig.json",
    visibility = ["//visibility:public"],
)
```

Unmodified TypeScript compiles: no explicit export type annotations, no extra
flags in `.bazelrc`. Every compiler option is the tsconfig's.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The package's files: TypeScript is compiled, JavaScript and declarations join the program, every other file is staged as data. See [Sources](#sources) |
| `deps` | `label_list` | `[]` | `ts_compile`, `ts_codegen` or `ts_npm_package` targets, and a workspace member's hub view `@npm//:<name>` |
| `tsconfig` | `label` | `None` | The project's own `tsconfig.json`, or a [`ts_config`](#ts_config) target: where every compiler option comes from. See [Where compiler options come from](#where-compiler-options-come-from) |

Those are the three. The emit knobs are build flags, one value for the whole
build:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--//ts:declarations` | `string` | `"tsgo"` | Which tool emits `.d.ts`: `"tsgo"` or `"oxc"`. See [Which tool emits the declarations](#which-tool-emits-the-declarations) |
| `--//ts:source_map` | `bool` | `True` | Emit a `.js.map` next to every `.js`. See [Source and declaration maps](#source-and-declaration-maps) |
| `--//ts:declaration_map` | `bool` | `False` | Emit a `.d.ts.map` next to every declaration. See [Source and declaration maps](#source-and-declaration-maps) |
| `--//ts:lib_check` | `bool` | `False` | Turn `skipLibCheck` off for every target. See [Finding a broken declaration](#finding-a-broken-declaration) |

A target that has to be built under another value of one of these is reached
through a Starlark transition; `tests/flags.bzl` is the ruleset's own.

### Sources

A `.js`, `.mjs` or `.cjs` src is staged into the output tree unchanged and joins
the type program. The rule sets `allowJs` for it, so its JSDoc types cross the
package boundary; `checkJs` in the tsconfig has its own body checked.

A `.jsx` src is rejected at analysis time, because oxc has no output extension
for one. The message says to rename it `.tsx`.

A `.d.mts` or `.d.cts` src is a declaration, handled as a `.d.ts` is: passed
through to consumers, and global when it has no top-level import or export. It
is the declaration of the `.mjs` or `.cjs` of the same stem, the pairing `tsc`
resolves by name, so `import { compile } from "./compile.mjs"` resolves to
`compile.d.mts` ahead of `compile.mjs`, and a checked-in declaration types an
untyped JavaScript module whether or not that module is in `srcs`. When it is,
the `.mjs` is staged and leaves the type program: TypeScript keeps the
higher-priority extension of a pair listed together, as `tsc` does. The
checked-in file is then the module's only declaration, and `checkJs` does not
reach that `.mjs`.

Every other src is a data file: staged into the output tree unchanged at its
package-relative path, so the compiled module beside it reaches it by the same
relative path at run time -- `import data from "./data.json"`,
`new URL("./logo.svg", import.meta.url)`, a `readFileSync` of a fixture. A
consumer gets the closure as `JsInfo.transitive_data_files`; `ts_test` stages it
in the runfiles beside the `.js`, `ts_binary` in its runfiles and its bundle,
`ts_dev_server` in its runfiles. A data file is never a tsgo input, with one
class of exception: a `.json` is, this target's and its deps' alike. An import
of it resolves to the file and is typed from its contents under
`resolveJsonModule`, which bundler resolution implies, and tsc reads the nearest `package.json` of every source for the
module's format and for the package's own name, so a package that imports
itself by name (`import "@scope/pkg/wire"` from inside `pkg`) resolves through
the manifest in `srcs`. At run time that name resolves through the hub's view of
the member, not the staged manifest: Vite's resolver walks `node_modules` and
has no package self-reference.

A `.mts` or `.cts` src is refused: the rule emits `.js` and `.d.ts` from `.ts`
alone, and has no output shape for one.

### Source and Declaration Maps

Turn `--//ts:source_map` off for a build whose JavaScript nothing debugs, or
whose bundler makes its own map.

`--//ts:declaration_map` is what makes go-to-definition across a package boundary
land on the `.ts` source. It requires the tsgo declaration emit; under
`--//ts:declarations=oxc` it is an analysis error naming both flags, since oxc
emits no map.

## Outputs

For each source file `foo.ts`:

| Output | Description |
|--------|-------------|
| `foo.js` | Compiled JavaScript (always from Oxc) |
| `foo.js.map` | Source map, under `--//ts:source_map` |
| `foo.d.ts` | Declaration file, the compilation boundary |

Every other src is staged at its package-relative path, unchanged.

## Where Compiler Options Come From

The tsconfig the actions read is written by `tsaction tsconfig`, one `TsConfig`
action per target. It `extends` two files, the ruleset's baseline and then the
target's `tsconfig`, runs `tsgo --showConfig` over that chain to read the
effective options, and writes the keys Bazel owns over both. Lowest precedence
first:

1. **The ruleset baseline**: `strict`, `module: "Preserve"`, `target: "es2022"`,
   `jsx: "react-jsx"`, `skipLibCheck`, `esModuleInterop`. Without a `tsconfig`
   they are the program's whole options; with one they reach only the keys the
   file and its own `extends` chain never mention.

   `moduleResolution` is asserted nowhere: TypeScript couples it to `module`, and
   tsgo derives the resolver from whichever `module` wins, `Bundler` for all of
   them but `Node16`/`NodeNext`.
2. **`tsconfig`**: the project's own `tsconfig.json`, and whatever it extends.
   Referenced where it lives, never copied, so relative paths inside it resolve
   against the directory they were written for. Everything it says wins over
   layer 1, so tsgo checks the code under the options `tsc` would.
3. **The keys Bazel owns**, written last: `rootDirs` bridging the source and
   output trees, `preserveSymlinks`, `declaration`, `emitDeclarationOnly`,
   `declarationMap`, `composite`, `incremental`, `rootDir`; under the tsgo emit
   `noEmit: false`, `noEmitOnError`, `outDir` and `declarationDir`; `allowJs`
   when a src is JavaScript; `isolatedDeclarations` under
   `--//ts:declarations=oxc`; `skipLibCheck: false` under `--//ts:lib_check`.
   `include` is `srcs`; `files`, `exclude` and `references` are `[]`. Two keys
   are the tsconfig's values rewritten: `paths`, each value from the directory
   of the chain file that set it and a `bazel-bin` twin beside it, and `types`,
   each path-shaped entry rebased to the file the sandbox stages (below). A
   value the tsconfig sets for one of these keys is overridden by `extends`
   order, not refused.

`--showConfig` is run over the chain, not over the user's file alone, so a
default the baseline supplies reaches oxc as tsgo sees it: oxc transforms with
the `target`, `jsx` and `jsxImportSource` the same run yields, handed over in
`<name>.options.json`, and the two compilers agree.

`preserveSymlinks` is what keeps a program to its declared inputs. Bazel stages
every input as a symlink into the source tree; resolved through the link, a
`types` entry is read at its realpath and its own imports resolve from there,
into files the target never declared. Read at the staged path, a program holds
its declared inputs and nothing the source tree has beside them: an import
inside a staged `.d.ts` that names a file nothing stages resolves to nothing,
and `skipLibCheck` drops the `TS2307`.

Read the tsconfig a target handed the compiler with
`bazel build //pkg:lib --output_groups=tsconfig`.

### What Fails Before tsgo Runs

Analysis rejects a `.jsx` src, a directory in `srcs` (a `ts_codegen` `out_dir`
tree belongs in `deps`), a `.mts` or `.cts` src,
`--//ts:declaration_map` under `--//ts:declarations=oxc`, and a target with
sources and no tsgo toolchain. One more is the root check below. `tsaction`
fails the `TsConfig` action on a path-shaped `types` entry no input sits at
([a `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file)),
naming the entry, the tsconfig and the path it looked for.

### One Root per Declaration Emit

One tsgo declaration emit has one `rootDir`. A checked-in source hangs off the
package directory, a generated one off the package's directory in `bazel-bin`,
and a src from another package off that package's directory. A target whose
srcs hang off more than one root fails at analysis under the tsgo emit:

```
ts_compile: srcs on @@//src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

Put the generated sources in their own target and depend on it, or build with
`--//ts:declarations=oxc`, which groups sources by root and runs oxc once per
group. A target holding only generated sources has one root and builds under
either emitter. A descendant package's file is inside this package's directory
and shares its root; a `.d.ts` from anywhere is passed through, not compiled,
and is not judged.

## Deps Have to Be Direct

A source may import only what a **direct** dep provides. Every `ts_compile`
target that has both sources and `deps` runs a `TsStrictDeps` action, which
reads those sources and fails on any specifier that resolves only because it
arrives through another dep's own deps:

```
ERROR: .../src/app/BUILD.bazel:3:11: TsStrictDeps //src/app:app failed: (Exit 1)
//src/app:app imports modules no direct dep provides:

  src/app/main.ts:1  imports "./hidden"
                     add "//src/app:hidden" to deps
  src/app/main.ts:2  imports "zod"
                     add "@npm//:zod" to deps

Each of those resolves today only because it reaches this target through
another dep's own deps, and stops resolving the moment that dep drops it.
Re-run gazelle to regenerate deps, or add the labels above by hand.
```

`bazel run //:gazelle` writes those labels. There is no flag and no opt-out.

**What is checked:** relative imports, and bare specifiers that name an npm
package the closure carries. **What is exempt:** Node builtins and `node:`
specifiers, and a specifier no package in the closure answers, a tsconfig
`paths` alias included: an alias resolves to files this target already stages,
and an import nothing provides has no label to suggest, so TypeScript reports it
as `TS2307`.

`/// <reference types="x" />` is not checked: it is not an import. The dep it
names is Gazelle's to write
([Import Resolution](../gazelle/overview.md#import-resolution)).

### The node_modules Forest

npm packages reach tsgo the way they reach node: through a `node_modules` tree.
Every target with a program builds one, the tree artifact `<name>/node_modules`,
with [`node_modules`](node-modules.md)'s builder over the target's npm deps,
their closures, the `@types/*` package paired with each, and the npm closure of
every first-party dep (`TsDeclarationInfo.transitive_npm_packages`): a dep's
emitted `.d.ts` imports the packages the dep declared, and they resolve in this
program by the same walk. The tree has one entry per resolution, the target's
own deps flat, so a name the closure resolves twice is answered for this target
by the version it declared.

tsgo walks up from the importing file for a bare specifier, and nothing above a
source in the exec root is an action output, so `tsaction tsgo` lays out a
program root under the target's output directory -- a symlink to every top-level
entry of the exec root plus the forest at `node_modules` -- and runs tsgo from
there. A bare specifier, an `exports` condition, a subpath, a `@types/*` pairing
and a `types` entry resolve as tsc resolves them over a pnpm install, and a
declaration tsgo emits names a package the way that package's `exports` allow.
npm deps contribute no other input: a `ts_compile`'s
`transitive_declaration_files` holds first-party declarations alone, and a
package's file sits under `node_modules/<name>/`, the segment TypeScript reads to
take it for a library file, type-checked and never emitted.

A workspace member is one of those packages. Its hub view `@npm//:<name>` links
the member's `package.json` as built -- every source-file target under `main`,
`module`, `browser`, `exports` and `imports` rewritten to the emitted `.js`,
`types` to the `.d.ts` -- beside the member's `.js` and `.d.ts` at the paths the
manifest names, so the bare name and each `exports` subpath resolve for tsgo and
for node through one manifest. See
[what a workspace member is imported as](../guides/npm.md#what-a-workspace-member-is-imported-as).

### `@types/*` Packages

DefinitelyTyped publishes `x`'s declarations as `@types/x`, and a scoped
`@a/b`'s as `@types/a__b`. The hub pairs the two from the lockfile, and a
`ts_npm_package` carries its paired `@types/*` package in `transitive_deps`, so
the forest links `@types/x` beside `x` and tsgo pairs them by walking
`node_modules/@types`, as it does over an install. Which of the two a name
resolves to follows npm: `x` is answered by the runtime package when it
publishes declarations of its own, and by `@types/x` when it publishes none. A
`@types/*` entry that forwards (`@types/bun/index.d.ts` is exactly
`/// <reference types="bun-types" />`) resolves the directive through the same
walk.

`types` is always written. When the tsconfig chain sets none, the direct
`@types/*` deps' names are written, so nothing auto-includes: a `@types/*`
package the closure carries but no entry names is in the tree for the imports
that reach it and out of the global scope, and a use of its globals is `TS2304`.
`//tests/npm:transitive_types_probe` pins that.

### Finding a Broken Declaration

The baseline sets `skipLibCheck: true`, so a `.d.ts` whose own imports do not
resolve reports nothing. What it exports becomes `any`, and the first visible
error is elsewhere, typically a `TS7006` on a callback parameter in application
code.

`--//ts:lib_check` turns `skipLibCheck` off for every target in the build, over
whatever the target's tsconfig says:

```bash
bazel build //... --//ts:lib_check
```

It reports findings unrelated to the one being chased: a `lib` a dependency
needs and the program does not set reports here too. It is a diagnostic sweep,
not a build mode.

### What Is in Global Scope

A `.d.ts` with no top-level import or export is a **global script**, and
everything it declares belongs to every program the file is part of. Under the
forest a file joins the program two ways: by import, where a bare specifier
resolves to the package's module entry and no further, and by `types`, which is
always written. `@sentry/cloudflare`'s declarations import
`@cloudflare/workers-types`, and that resolves to the package's `index.ts`, a
module; its `index.d.ts`, 15k lines of global script, enters a program only
through a `types` entry that names the package. A worker names it in its
tsconfig; a browser target that depends on `@sentry/cloudflare` does not, and
keeps `lib.dom`'s `Element`.

An import of a package the closure does not carry is `TS2307`. A `declare
module "x"` in a `.d.ts` src answers it, since nothing resolves the specifier
first.

### Importing Another Target by Bare Specifier

Two kinds of target answer a bare specifier. A pnpm workspace member is imported
through its hub view, `@npm//:<name>`, and resolves through the forest like any
npm package; its `exports` map, rewritten to the emitted files, decides what
`@acme/ui` and `@acme/ui/button` are. Any other first-party target is reached
through the tsconfig's `paths`: the rule rewrites each value to the source
directory and its `bazel-bin` twin, so a dep's declarations and a `ts_codegen`
`out_dir` tree resolve through the alias the tsconfig already has.

```jsonc
// apps/web/tsconfig.json
{ "compilerOptions": { "paths": { "#shared/*": ["../../packages/shared/src/*"] } } }
```

```python
# apps/web/BUILD.bazel
ts_compile(
    name = "web",
    srcs = ["main.ts"],          # import { flag } from "#shared/flags";
    tsconfig = "tsconfig.json",
    deps = ["//packages/shared"],
)
```

The value is read from the directory of the chain file that sets it, which
`--showConfig` does not print, so tsaction walks the `extends` chain for it. An
alias is type-checking only: a value import through one needs the module
reachable at runtime, which a dep on the producing target gives a `ts_test` or
`ts_binary`.

### ts_config

Starlark cannot read a file to follow its `extends` chain, so a tsconfig that
extends another file has to declare the chain. Every file in it becomes an input
to the type-check action:

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    deps = ["//:tsconfig.base.json"],
)

ts_compile(
    name = "lib",
    srcs = ["index.ts"],
    tsconfig = ":tsconfig",
)
```

A tsconfig that extends nothing goes straight into `ts_compile`:

```python
ts_compile(
    name = "lib",
    srcs = ["index.ts"],
    tsconfig = "tsconfig.json",
)
```

### A Cloudflare Worker

Ambient globals from a generated `.d.ts`, no DOM, and no stray `@types` package
from the dependency graph reaching global scope, all in the worker's own
tsconfig:

```jsonc
// tsconfig.json
{
  "compilerOptions": {
    "lib": ["es2022"],
    "types": ["./worker-configuration.d.ts"],
    "resolveJsonModule": true
  }
}
```

```python
ts_compile(
    name = "worker",
    srcs = ["index.ts"],
    tsconfig = "tsconfig.json",
    deps = [":worker_types"],   # the ts_codegen whose outs write worker-configuration.d.ts
)
```

`types` names a package the forest resolves or a declaration file a dep stages
(both below). Neither puts a dep's globals in scope on its own; the entry does.

### A `types` Entry That Names a Package

`types: ["vite/client"]` names a package, and tsgo resolves it as tsc does over
an install: the package's own `types`, an `exports` subpath
(`@cloudflare/vitest-pool-workers/types`), a directory the package ships at that
subpath (`@cloudflare/workers-types/2023-07-01`), or the paired `@types/*`
package (`node` is `@types/node`). The package has to be in the forest -- a
direct dep, or in a dep's closure -- and an entry the forest does not answer is
tsgo's `TS2688: Cannot find type definition file`, from the action.

`typeRoots` stays unset. With a custom `typeRoots` tsgo skips the
`node_modules` walk for a `types` entry, and that walk is the only place an
`exports`-only subpath resolves; inside the program root tsgo's default root is
`node_modules/@types`, so `types: ["node"]` resolves as before.

### A `types` Entry That Names a Declaration File

`types: ["./worker-configuration.d.ts"]` names a path. tsc resolves it against
the tsconfig's own directory, and tsaction rebases it to where the sandbox
stages the file, with tsc's lookup: the path as a file or directory, or the
path with a TypeScript or declaration extension added, in the source tree first
and under `bazel-bin` second. A checked-in declaration is in the source tree; a
generated one, such as the [`ts_codegen`](ts-codegen.md#cloudflare-worker-bindings)
running `wrangler types` writes, is in `bazel-bin`. What stages the file is a
label: `srcs`, or a dep whose `srcs` hold it or whose `outs` write it (a `.d.ts`
in `srcs` is passed through unchanged, so a dep edge stages it at the path the
entry names). Gazelle writes that dep from the tsconfig
([the `compilerOptions` baseline](../gazelle/overview.md#the-compileroptions-baseline)).

An entry nothing stages fails the `TsConfig` action before tsgo runs, where
tsgo's own `TS2688` would name no dep:

```
tsaction: compilerOptions.types entry "./worker-configuration.d.ts" in
worker/tsconfig.json names worker/worker-configuration.d.ts, which no input of
this action sits at: not in the source tree, not under bazel-out/k8-fastbuild/bin.
A declaration this program names is a src of the target or an output of one of
its deps.
```

Only the two relative shapes are paths. `./x.d.ts` and `../x.d.ts` resolve
against the tsconfig's directory; anything else (`x.d.ts`, `vendor/x.d.ts`) is a
package name to tsc and to tsaction alike. A `../` entry reaches a file in an
ancestor directory, the way a test directory's tsconfig names the worker's
declaration beside the tsconfig it extends.

Such an entry is for globals. A module (a `.d.ts` with a top-level import or
export) resolves and joins the program, but its declarations stay scoped to it;
a module augmentation inside it needs the module in the program, which the entry
gives it.

### Ambient Precedence

The target's own declarations are root files, listed in `include` ahead of what
`types` brings in, so where both declare the same `declare module` pattern the
project's own wins. `tsc` orders them the same way: a `types` entry arrives as a
type-reference directive, which joins the program after the root files. The
first declaration of a pattern wins, and a narrower pattern does not change
that: an earlier `declare module "*.svg"` beats a later
`declare module "*.icon.svg"` even for `star.icon.svg`.

To let a package's ambient win instead, drop the project's competing
declaration.

### Which Ambients a Consumer Gets

A global `.d.ts` in `srcs` types the target that owns it and travels to every
consumer as a declaration output, and a consumer's program holds what its own
`include` and `types` name. So a consumer that needs the globals names the file
in its own tsconfig `types`, with the owning target in `deps`:

```python
# workers/proxy/BUILD.bazel
ts_compile(
    name = "worker_types",
    srcs = ["worker-configuration.d.ts"],
    visibility = ["//workers/proxy:__subpackages__"],
)

# workers/proxy/test/tsconfig.json: "types": ["../worker-configuration.d.ts"]
# workers/proxy/test/BUILD.bazel
ts_test(
    name = "handler_test",
    srcs = ["handler.test.ts"],
    tsconfig = "tsconfig.json",
    deps = ["//workers/proxy:worker_types", "//workers/proxy/src", "@npm//:vitest"],
)
```

A package can hold an ambient it needs for its own standalone `tsc -p` that is
no part of its public type surface, such as a `process` shim in a library with no
`@types/node`, and no consumer sees it unless that consumer asks. The unit is
the file: TypeScript decides module-or-global per file. A `.d.ts` mixing a shim
for the package's own build with a declaration consumers are meant to have is
two files.

A consumer that uses a global no entry supplies sees the identifier as
undefined. Two things supply it: a `@types/*` dep of its own (`@types/node` for
`process`, in `types` or as a direct dep when the tsconfig sets no `types`), or
the owning target's file named in `types`.

## Which Tool Emits the Declarations

Oxc always does the JavaScript transform. `--//ts:declarations` decides which
tool produces the `.d.ts`, for every target in the build.

### `--//ts:declarations=tsgo` (default)

tsgo emits declarations from the complete type program: the target's sources,
every first-party dep's `.d.ts` and the forest.

- **No source annotations required.** Inferred export types are fine.
- **Declarations are exactly what `tsc` would emit**, including inferred object
  shapes, literal unions and `RegExp`.
- **Type errors fail `bazel build`.** The `.d.ts` are real outputs of the tsgo
  action, so a target with a type error produces nothing. No
  `--output_groups=+_validation` needed.
- **Type-checking is on the critical path.** A consumer waits for its
  dependency's declarations.

### `--//ts:declarations=oxc`

Oxc emits declarations syntactically, per file, with no type program. This
requires [isolated declarations](../getting-started/isolated-declarations.md):
every export needs an explicit type, and Oxc **errors** when one does not have
one. Type-checking moves into the `_validation` output group, off the critical
path, so downstream targets compile while checking runs concurrently.

Set it in `.bazelrc` once every package's exports are annotated.

## Cost of Each Mode

Measured with:

```bash
tools/bench_declarations.sh 20 50 3
```

1,000 annotated files across 20 packages in one linear dependency chain,
medians of three interleaved runs:

| Mode | Rebuild wall | Critical path |
|------|--------------|---------------|
| `--//ts:declarations=tsgo` | 6.3s | 4.89s |
| `--//ts:declarations=oxc` | 3.8s | 2.15s |

Both modes run tsgo once per target, so the gap is serialisation. Under `oxc`
the check is a validation action nothing waits for, and the critical path is
Oxc's per-file transform; under `tsgo` each of the 20 links waits for its
dependency's declarations. The gap shrinks on shallower graphs and widens on
deeper ones.

## Providers

Fields for both, and the load path, are in
[Providers and Toolchains](providers.md).

- **`JsInfo`**: this target's `.js` and `.js.map` files as direct depsets, and
  the closure of both as transitive ones; `ts_binary` reads the transitive `.js`
  set. Its data srcs are `data_files`, the closure's `transitive_data_files`:
  what `ts_test`, `ts_binary` and `ts_dev_server` stage beside the `.js`
- **`TsDeclarationInfo`**: this target's declarations and their first-party
  closure, plus the npm packages that closure imports; a downstream `ts_compile`
  type-checks against the closure and links the packages into its forest
- **`OutputGroupInfo(tsconfig=...)`**: the tsconfig this target handed the
  compiler, on any target with a program
- **`OutputGroupInfo(_validation=...)`**: the tsgo check stamp, written only
  under `--//ts:declarations=oxc`; under the default the declarations are the
  tsgo action's own outputs.
- **`OutputGroupInfo(strict_deps=...)`**: the `TsStrictDeps` stamp, on any
  target with both `deps` and sources. The compile actions take it as an input,
  so a violation fails a plain `bazel build`; the output group exposes the stamp
  and the checker on their own.

## Architecture

Four actions per target. `TsStrictDeps` runs first, as a Node action over a
params-file manifest of the target's declared and reachable providers. Its
scanner is a character walk over the source: a quoted string is a specifier only
when the tokens before it say so. Gazelle generates deps with the same walk.

`TsConfig` writes `<name>.tsconfig.json` and `<name>.options.json` from
`tsgo --showConfig` ([above](#where-compiler-options-come-from)). `OxcCompile`
processes each `.ts` file with the options file's `target`, `jsx` and
`jsxImportSource`:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations), only under `--//ts:declarations=oxc`
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

tsgo runs from the program root, with `--project` on the written tsconfig.
Under `--//ts:declarations=tsgo` that tsconfig sets `declaration`,
`emitDeclarationOnly`, `rootDir` and `outDir` so the emitted declarations land
beside Oxc's `.js` (mnemonic `TsgoDeclare`). Under `oxc` it runs with `--noEmit`
and writes only a stamp (mnemonic `TsgoCheck`); `rootDir` is the exec root
there, which every input is under, since tsgo checks the program against it
even when nothing is emitted (`TS6059`).

## Output Paths

Output paths are derived from source file names, not target names, so
`import "./foo"` resolves to `bazel-bin/.../foo.js`. Two `ts_compile` targets in
the same package therefore cannot list the same source file: Bazel reports
conflicting actions. Split by directory, or give each target its own sources.
