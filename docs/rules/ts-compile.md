# ts_compile

Compiles TypeScript with Oxc and emits `.d.ts` declarations with tsgo.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    deps = ["//other/package", "@npm//:zod"],
    visibility = ["//visibility:public"],
)
```

Unmodified TypeScript compiles: no explicit export type annotations, no tsconfig
wiring, no extra flags in `.bazelrc`.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`, `.tsx`, `.d.ts`, `.d.mts`, `.d.cts`, `.js`, `.mjs` or `.cjs` files. See [Sources](#sources) |
| `deps` | `label_list` | `[]` | `ts_compile`, `ts_npm_package`, `css_library`, `css_module`, `asset_library` or `json_library` targets |
| `target` | `string` | `"es2022"` | ECMAScript target version |
| `jsx_mode` | `string` | `"react-jsx"` | JSX transform: `react-jsx`, `react`, `preserve`; empty disables JSX |
| `declarations` | `string` | `"tsgo"` | Which tool emits `.d.ts`: `"tsgo"` or `"oxc"` |
| `enable_check` | `bool` | `True` | Run tsgo. See [Turning tsgo off](#turning-tsgo-off) |
| `source_map` | `bool` | `True` | Emit a `.js.map` next to every `.js`. See [Source and declaration maps](#source-and-declaration-maps) |
| `declaration_map` | `bool` | `False` | Emit a `.d.ts.map` next to every declaration. See [Source and declaration maps](#source-and-declaration-maps) |
| `tsgo_args` | `string_list` | `[]` | Extra tsgo flags. See [tsgo flags](#tsgo-flags) |
| `tsconfig` | `label` | `None` | The project's own `tsconfig.json`, or a [`ts_config`](#ts_config) target, as the `compilerOptions` baseline |
| `lib` | `string_list` | `None` | `compilerOptions.lib`, e.g. `["es2022", "webworker"]`. Replaces the whole set `target` implies |
| `types` | `string_list` | `None` | `compilerOptions.types`: which ambient type packages load. `[]` loads none; relative entries resolve against this target's package, from `srcs` or `types_srcs`. See [A `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file) |
| `jsx_import_source` | `string` | `None` | `compilerOptions.jsxImportSource`, e.g. `"solid-js"`, `"preact"` |
| `compiler_options` | `dict` | `None` | Any other `compilerOptions`, passed through verbatim |
| `module_name` | `string` | `""` | The bare specifier this target is importable as, e.g. `"@acme/ui"` |
| `path_aliases` | `string_dict` | `{}` | Alias prefix → workspace-relative source directory. Must resolve to files this target stages: its own `srcs`, or `path_alias_srcs` |
| `path_alias_srcs` | `label_list` | `[]` | Files a `path_aliases` entry resolves to when they are not in `srcs`. They join this target's type program, so a type error in one of them fails this target |
| `types_srcs` | `label_list` | `[]` | The `.d.ts`, `.d.mts` or `.d.cts` files a relative `types` entry names, when neither `srcs` nor a dep stages them. See [A `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file) |
| `public_globals` | `label_list` | `[]` | The `.d.ts`, `.d.mts` or `.d.cts` in `srcs` whose globals every consumer gets too. Unnamed is private. See [Which ambients a consumer gets](#which-ambients-a-consumer-gets) |
| `untyped_packages` | `string_list` | `[]` | npm packages this target's type program leaves out: no `paths` key, no `files` entry. See [Keeping a package out of the program](#keeping-a-package-out-of-the-program) |
| `vite_types` | `bool` | `False` | Prepend the Vite ambient type shim to `srcs` |

### Sources

A `.js`, `.mjs` or `.cjs` src is staged into the output tree unchanged and joins
the type program. The rule sets `allowJs` for it, so its JSDoc types cross the
package boundary; add `checkJs` through `compiler_options` to have its own body
checked.

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

### Source and Declaration Maps

Turn `source_map` off for a target whose JavaScript nothing debugs: a codegen
step, or a bundle input whose bundler makes its own map.

`declaration_map` is what makes go-to-definition across a package boundary land
on the `.ts` source. It requires the tsgo declaration emit
(`declarations = "tsgo"` with `enable_check = True`); oxc emits no map.

### tsgo Flags

`tsgo_args` accepts only the flags that report on the program:
`--traceResolution`, `--explainFiles`, `--listFiles`, `--listEmittedFiles`,
`--diagnostics`, `--extendedDiagnostics`, `--noErrorTruncation`. A
compilerOption belongs in `compiler_options`, where the Bazel-owned-key guard
can see it.

## Outputs

For each source file `foo.ts`:

| Output | Description |
|--------|-------------|
| `foo.js` | Compiled JavaScript (always from Oxc) |
| `foo.js.map` | Source map |
| `foo.d.ts` | Declaration file, the compilation boundary |

Under `declarations = "tsgo"` with `enable_check = False` there is no type
program and therefore no `.d.ts` at all; see
[Turning tsgo off](#turning-tsgo-off).

## Where Compiler Options Come From

The generated tsconfig carries the machinery the rules own: `rootDirs` bridging
the source and output trees, `paths` for npm packages, the `files` list that
carries each `@types/*` dep, and under `declarations = "tsgo"` the
`outDir`/`rootDir`/`noEmitOnError` triple. A user tsconfig supplies the
baseline, and the generated one `extends` it.

Lowest precedence first:

1. **The ruleset baseline**: `strict`, `module: "Preserve"`, `skipLibCheck`,
   `esModuleInterop`. Without a `tsconfig` they go straight into the generated
   file; with one they go into a file the generated config `extends` before
   yours, so they reach only the keys your file never mentions.

   `moduleResolution: "Bundler"` joins them only where the baseline also owns
   `module`: without a `tsconfig`, and while `compiler_options` names neither
   key. TypeScript couples the two; a baseline `moduleResolution` left standing
   after a `module: "NodeNext"` replaced the baseline's is `TS5109` before a
   source is read. Left out, tsgo derives the resolver from whichever `module`
   won: `Bundler` for all of them but `Node16`/`NodeNext`, which derive their
   own.
2. **`tsconfig`**: the project's own `tsconfig.json`, and whatever it extends.
   Referenced where it lives, never copied, so relative paths inside it resolve
   against the directory they were written for. Everything it says wins over
   layer 1, so tsgo checks the code under the options `tsc` would. Setting the
   attribute adds what the file says; the baseline stays.
3. **`target` and `jsx_mode`**, then `jsx_import_source`, `lib`, `types`, then
   `compiler_options`, which wins among these.
4. **The options Bazel owns**: `paths`, `include`, and the 16 keys listed
   below.

`target` and `jsx_mode` are injected in every mode, including over a tsconfig
baseline, and supersede a `target` or `jsx` in the file. Oxc transforms with
them, and the two compilers have to agree.

`allowArbitraryExtensions` is set above layer 2: the `.d.ts` files generated for
`css_module` / `css_library` / `asset_library` / `json_library` deps require it.
It overrides the value in your tsconfig file; `compiler_options` sits above it.

Layer 1 is a file, `<target>.tsconfig_baseline.json`, beside the generated
config: Starlark cannot read your tsconfig to see which keys it sets, and
`extends` takes a list in which a later entry overrides an earlier one.

### The Two Hard Errors

Both fail at analysis time.

**A Bazel-owned key in `compiler_options`.** These 16 encode the sandbox layout
or the action's declared outputs:

`baseUrl`, `rootDirs`, `paths`, `outDir`, `rootDir`, `declarationDir`,
`declaration`, `emitDeclarationOnly`, `declarationMap`, `sourceMap`, `noEmit`,
`noEmitOnError`, `isolatedDeclarations`, `composite`, `incremental`,
`tsBuildInfoFile`.

`declarationMap` and `sourceMap` follow from the `declaration_map` and
`source_map` attributes. Each failure message names the attribute to use
instead where there is one, and otherwise says what the key encodes.

```
ts_compile: compilerOptions.paths is set by the rule and cannot be overridden --
use path_aliases for source aliases, or module_name on the target that produces
the declarations.
Remove "paths" from compiler_options on @@//src/app:app.
```

**A `path_aliases` value pointing into the output tree.** A path under
`bazel-out/` or `bazel-bin/` embeds the build configuration, so it breaks under
`-c opt` or a different exec platform:

```
ts_compile: path_aliases["@acme/ui"] on @@//src/app:app points into the output
tree (bazel-out/k8-fastbuild/bin/packages/ui).
```

Set `module_name` on the producing target.

### Generated and Checked-In Sources in One Target

One tsgo declaration emit has one `rootDir`. A checked-in source hangs off the
package directory, a generated one off the package's directory in `bazel-bin`.
A target holding both fails at analysis under the default emit:

```
ts_compile: srcs on @@//src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

Put the generated sources in their own target and depend on it, or set
`declarations = "oxc"` (which groups sources by root and runs oxc once per
group), or set `enable_check = False`. Neither of the last two emits from tsgo.
Generated sources cannot use the default emit even on their own; see
[ts_codegen § Compiling the output](ts-codegen.md#compiling-the-output).

### A Src From Outside This Package's Tree

A src is compiled into the package of the target that lists it: its outputs are
declared under that package. Its root is the directory the file lives in, so a
src from a sibling package, from an ancestor, or from another repository hangs
off a root of its own while this package's sources hang off the package. A
`srcs` list decides this on its own, so the mix is rejected while the BUILD file
loads, before analysis:

```
ts_compile: srcs on //src/app:app mix this package's own files with files that
live outside it:
  //packages/shared:util.ts
```

Give the file a target in its own package and depend on that; set `module_name`
on it when the import is by bare specifier. `declarations = "oxc"` and
`enable_check = False` skip the check.

Only the mix is rejected. Five shapes are not it:

- A descendant package's file is inside this package's directory, the root this
  package's own sources hang off: `srcs = ["a.ts",
  "//src/app/sub:x.ts"]` in `//src/app` is the one `rootDir` `src/app`, and
  builds. A `ts_compile` may hold a whole subtree, and a subtree may grow a
  BUILD file without the target above it being split. A file the descendant
  package also compiles is a conflict: both declare
  `bazel-bin/src/app/sub/x.js`, and Bazel rejects that pair as conflicting
  actions, naming both targets.
- A target whose srcs all come from elsewhere has one root like any other.
- The top-level package is the exec root, the root a src from any other package
  hangs off too, so `srcs = ["main.ts", "//lib:util.ts"]` at the repository root
  is one root, and builds.
- A `.d.ts` from anywhere is passed through, not compiled. `vite_types = True`
  prepends the Vite shim from `@rules_typescript//ts` this way.
- A `select` resolves after the loading phase, so this check never sees its
  branches. A mix hidden inside one reaches the analysis-time error.

Only a label that names a source file is judged. `//other:some_target` stands
for whatever files that rule or filegroup produces, wherever those live; the
analysis-time root check covers them.

A label naming a repository, `@other//pkg:f`, is outside this tree whatever its
path. `@//pkg:f` and the canonical `@@//pkg:f` are not: an empty repository part
is this repository, so `@@//<this package>:f` is this package. A repository
naming itself by its own apparent name is read as foreign; spell it `//pkg:f`.

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

**What is checked:** relative imports, bare specifiers that name an npm package,
and bare specifiers that name another target's `module_name` (including a
subpath of one). **What is exempt:** Node builtins and `node:` specifiers, and
anything under a `path_aliases` prefix, since an alias resolves to files this
target already stages. An import that nothing in the closure provides is left
alone, because there is no label to suggest; TypeScript reports it as `TS2307`.

`/// <reference types="x" />` is not checked: Gazelle generates no dep for it
either, so the check would have no label to suggest.

### Action Inputs

The action inputs stay transitive. One `paths` map serves the whole type
program; without the transitive entries a declared dep's own `.d.ts` could not
resolve its imports, and TypeScript would widen them to `any` without a report.
`TsStrictDeps` stops your own source from using them.

### `@types/*` Packages

DefinitelyTyped publishes `x`'s declarations as `@types/x`, and a scoped
`@a/b`'s as `@types/a__b`. TypeScript pairs the two by walking
`node_modules/@types`; there is no `node_modules` here, so the pairing is a
`paths` key. A `@types/*` dep therefore gets its entries twice: once under its
own name, and once under the name it types.

Which of the two a name resolves to follows npm: `x` is answered by the runtime
package when that package publishes declarations of its own, and by `@types/x`
when it publishes none. `@babel/core` ships no `.d.ts`, so `@babel/core`
resolves into `@types/babel__core`; `@babel/types` ships its own, so it does
not. A `path_aliases` prefix outranks both.

Without the second key the declarations are in the program under a name nothing
imports. The imports that need it are mostly inside other packages' `.d.ts`
files (`rollup`'s say `from "estree"`, `@types/chai`'s say `from "deep-eql"`),
where `skipLibCheck` hides the `TS2307` and the types those files export widen
to `any`. See [Finding a broken declaration](#finding-a-broken-declaration).

A `@types/*` entry that forwards (`@types/bun/index.d.ts` is exactly
`/// <reference types="bun-types" />`) brings the package it names along.
TypeScript resolves that directive through `typeRoots` and a `node_modules`
walk from the referencing file, never through `paths`, so the sandbox resolves
it to nothing and `skipLibCheck` hides the `TS2688`. The names in each
designated declaration's header are read when the package is fetched, resolved
against that package's own dependencies (`@types/x` first, as `typeRoots`
goes), and the answers join `files` beside the entry, chain included:
`bun-types`' own entry references `node`, so `@types/node` arrives with it. A
package in `untyped_packages` answers no directive.

### Finding a Broken Declaration

The baseline sets `skipLibCheck: true`, so a `.d.ts` whose own imports do not
resolve reports nothing. What it exports becomes `any`, and the first visible
error is elsewhere, typically a `TS7006` on a callback parameter in application
code.

`--//ts:lib_check` turns `skipLibCheck` off for every target in the build, over
whatever `compiler_options` or a named `tsconfig` say:

```bash
bazel build //... --//ts:lib_check
```

It reports findings unrelated to the one being chased: a `lib` a dependency
needs and the program does not set reports here too. It is a diagnostic sweep,
not a build mode.

### Keeping a Package Out of the Program

A `.d.ts` with no top-level import or export is a **global script**, and
everything it declares belongs to every program the file is part of. A dynamic
`import()` loads a module's declarations exactly like a static one, and the
package one hop behind it comes along.

One line in a browser component broke 21 files in the Lovable monorepo this
way. `void import("@sentry/cloudflare")` sat inside an `import.meta.env.SSR`
branch; `@sentry/cloudflare`'s own declarations import
`@cloudflare/workers-types`, 15k lines of global script, so its
`interface Element` and `interface Body` merged into the ones `lib.dom`
declares. `Element.append` then took `string | ReadableStream | Response` and
no `Node`, and the errors landed on DOM code that names neither package.

`untyped_packages` keeps a package out of one target's type program:

```python
ts_compile(
    name = "web",
    srcs = glob(["src/**/*.ts", "src/**/*.tsx"]),
    untyped_packages = ["@cloudflare/workers-types"],
    deps = ["@npm//:sentry_cloudflare"],
)
```

A named package gets no `paths` key (not its own, not one per `exports`
subpath, and not the bare name a `@types/*` package would answer for it) and
no `files` entry. It stays in `deps`, its files stay among the action's
inputs, and no JavaScript moves.

`TsStrictDeps` sees the exclusion. A direct dep stays declared, so an import of
it is still attributed. A package that was only reachable leaves the reachable
set with the key, so an import of it is a bare `TS2307`, not "add this dep":
adding the dep would not type it.

An entry names one package, and a package's declarations live wherever npm put
them. `ms` ships none and is typed by `@types/ms`, so `["@types/ms"]` takes
those declarations out, and `ms` then resolves to the runtime package it names.
`["ms"]` takes away the bare name `@types/ms` answered for it. Name both to
leave nothing.

**An import of a named package resolves to nothing**, which is `TS2307`. A
target whose own sources import one needs a `declare module` of its own in a
`.d.ts` src:

```ts
declare module "@sentry/cloudflare" {
  export function captureException(error: unknown): void;
}
```

That declaration answers only because the `paths` key is gone. With the key in
place TypeScript resolves the specifier and adds the file to the program before
the checker ever asks about an ambient module, so the shim alone changes
nothing and the globals still arrive.

The attribute is per target and travels through no dep edge: a dependent that
needs the package resolves it as before. A worker entry point keeps the
Cloudflare types the browser target drops.

The tsconfig `bazel run //:refresh_tsconfig` writes has one `paths` map for the
whole workspace; a nested tsconfig extends the root and inherits it unchanged.
Where every target reaching a package excludes it, the editor drops it too.
Where one target excludes a package another still resolves, one map cannot
answer both, and `ts_refresh_tsconfig` fails:

```
ts_refresh_tsconfig: @@//web:web keeps "@cloudflare/workers-types" out of its type program
  (untyped_packages), and this config still resolves it for something it
  reaches. ...
Add "@cloudflare/workers-types" to host_only_packages to drop it from the editor everywhere,
or name it in untyped_packages on the targets that still resolve it.
`bazel query "rdeps(//..., @npm//:<the package>)"` names those targets.
```

### Importing Another Target by Bare Specifier

`path_aliases` maps a prefix to a source directory, as in
`@/components` → `src/components`. The rule maps the prefix onto that directory
and its `bazel-bin` mirror, so a `css_library`, `asset_library` or
`json_library` declaration for a file under it resolves too. A target whose
declarations land anywhere else is out of reach: only that target knows where
they go under the current configuration. Set `module_name` on it:

```python
# packages/ui/BUILD.bazel
ts_compile(
    name = "ui",
    srcs = glob(["*.ts", "*.tsx"]),
    module_name = "@acme/ui",
    visibility = ["//visibility:public"],
)

# apps/web/BUILD.bazel
ts_compile(
    name = "web",
    srcs = ["main.ts"],          # import { Button } from "@acme/ui";
    deps = ["//packages/ui"],
)
```

The dependent gets a `paths` entry for `@acme/ui` and `@acme/ui/*` pointing at
the `.d.ts` files Bazel produced, with `index.d.ts` as the entry point. The name
travels in the `TsModuleInfo` provider, transitively.

For a pnpm workspace member linked through `@npm//:<name>`, its own
`package.json` decides. The `exports` map, `typings`, `types` and `main` are
read where the lockfile is, and each specifier the member declares becomes its
own `paths` entry, so `@acme/ui/button` resolves to the declaration emitted from
the file `exports["./button"]` names, wherever it sits. A wildcard subpath
(`"./icons/*": "./icons/components/*.tsx"`) becomes a wildcard pattern. The
default entries above stay behind every declared entry. See
[npm workspace members](../guides/npm.md).

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
from the dependency graph reaching global scope:

```python
ts_compile(
    name = "worker",
    srcs = ["index.ts"],
    tsconfig = "tsconfig.json",
    lib = ["es2022"],
    types = ["./worker-configuration.d.ts"],
    types_srcs = ["worker-configuration.d.ts"],
    compiler_options = {"resolveJsonModule": True},
)
```

Relative entries in `types` and `typeRoots` are rewritten to resolve from the
generated config, so they are written exactly as they would be in the package's
own tsconfig. Other path-valued options are not rewritten: they resolve against
the generated config's directory, so they belong in the `tsconfig` file.

`types` names a package resolved from `deps` or a declaration file a label
stages (both below). Neither puts a dep's globals in scope. A `.d.ts` in
another target's `srcs` with no top-level import or export declares globals,
and those reach every target that depends on it, however far down the graph the
declaration sits, when that target names the file in `public_globals`.

### A `types` Entry That Names a Package

`types = ["vite/client"]` names a package, and TypeScript would resolve it by
walking `node_modules` for it. There is no `node_modules` here, so the rule
resolves the entry against this target's `deps` and puts the declaration the
package's own manifest designates into the generated config's `files`. Three
spellings resolve: the package itself, one of its `exports` subpaths, and the
bare name a paired `@types/*` package supplies (`node` is `@types/node`).

An entry no dep answers fails at analysis, naming the entry and the dep to add.
`tsc` reports `error TS2688: Cannot find type definition file` for such an
entry; tsgo reports nothing and exits 0, and the failure surfaces on whatever
used the declarations: `TS2339` on `import.meta.env` without `vite/client`,
`TS2591` on `process` without `node`.

Two alternatives to the dep: name a declaration file instead (the next
section), or set `typeRoots` in `compiler_options`. A target with a `typeRoots`
is exempt from the package-entry check: what sits under it is the compiler's to
find at action time. A declaration-file entry is checked either way.

### A `types` Entry That Names a Declaration File

`types = ["./worker-configuration.d.ts"]` names a path, and a path resolves
against the sandbox. The entry is resolved against the files the action stages:
`srcs`, the deps' declarations (a `.d.ts` in `srcs` is a declaration output
unchanged, so a dep edge stages it), `path_alias_srcs`, and `types_srcs`. An
entry none of them sits at fails at analysis: tsgo reports nothing for a
`types` entry that resolves to nothing, and the target would compile without
the declarations it asked for.

The entry is written into the generated config as the path to the file it
resolved to. A checked-in declaration is in the source tree; a generated one,
such as [`ts_worker_types`](ts-worker-types.md) writes, is in `bazel-out`. The
entry points wherever the label put it, so the two are named the same way:

```python
ts_compile(
    name = "lib",
    srcs = glob(["*.ts"]),
    types = ["../../worker-configuration.d.ts"],
    types_srcs = ["//workers/proxy:worker_types"],
)
```

!!! note "Upgrading"
    A relative `types` entry no label answers used to resolve to nothing in
    silence. It is an analysis error now. For each `./x.d.ts` or `../x.d.ts`
    entry in `types`, name the file with a label: in `srcs`, on a dep whose
    `srcs` hold it, or in `types_srcs`. The message names the path it looked
    for and the attribute to list it in.

`types_srcs` is for the file neither `srcs` nor a dep stages. It is a label
list, so the file may live in another package. Unlike a `.d.ts` in `srcs`, it
is not passed through as this target's own declaration. tsgo parses it as part
of this program, so a syntax error in the file fails this target (`TS1434` and
friends). What it declares goes unchecked, since it is a `.d.ts` under the
baseline's `skipLibCheck`; `--//ts:lib_check` or
`compiler_options = {"skipLibCheck": False}` surfaces a type error inside it. A
file listed there that no entry names is an analysis error too: nothing else
puts it in the program.

Only the two relative shapes are paths. TypeScript resolves `./x.d.ts` and
`../x.d.ts` against the config's own directory, and anything else (`x.d.ts`,
`vendor/x.d.ts`) through `typeRoots` and `node_modules/@types`, a walk the
compiler does at action time and this rule neither rewrites nor resolves.
`./typings`, a directory, is the compiler's too.

Such an entry is for globals. A module (a `.d.ts` with a top-level import or
export) resolves and joins the program, but its declarations stay scoped to it.
`public_globals` rejects a module; a `types` entry does not, since a module
augmentation inside it needs the module in the program.

Only the attribute is checked. The rule does not read a `types` in the
`tsconfig` file the target names: with `"types": ["vite/client"]` in the
tsconfig and `@npm//:vite` in `deps`, the target analyses, generates a config
whose `files` is empty, and fails in tsgo with `TS2339` on the
`import.meta.env` those declarations would have typed. Put the entries in
`compiler_options`. Gazelle does that for a relative entry naming a file in the
tsconfig's own directory, rebasing it onto each target below and naming the
file in `types_srcs`; see
[a declaration the tsconfig names](../gazelle/overview.md#a-declaration-the-tsconfig-names).

### Ambient Precedence

A dep's globals are listed ahead of the ones `types` and `@types/*` packages
supply, so where both declare the same `declare module` pattern the project's
own wins. `tsc` orders them the same way: a `types` package arrives as a
type-reference directive, which joins the program after the root files. The
first declaration of a pattern wins, and a narrower pattern does not change
that: an earlier `declare module "*.svg"` beats a later
`declare module "*.icon.svg"` even for `star.icon.svg`.

To let a package's ambient win instead, drop the project's competing
declaration.

### Which Ambients a Consumer Gets

A `.d.ts` in `srcs` types the target that owns it. `public_globals` also puts
it in the program of everything that depends on that target.

```python
ts_compile(
    name = "worker_types",
    srcs = ["worker-configuration.d.ts"],
    public_globals = ["worker-configuration.d.ts"],
)
```

Unnamed is private. A package can hold an ambient it needs for its own
standalone `tsc -p` that is no part of its public type surface, such as a
`process` shim in a library with no `@types/node`:

```python
ts_compile(
    name = "ui",
    srcs = glob(["**/*.tsx"]) + ["types/ambient.d.ts"],
    tsconfig = "tsconfig.json",
)
```

Exported, that shim lands in `files` ahead of `@types/node` in every consumer
that has the real `process`. The duplicate identifier is reported inside a
`.d.ts`, where `skipLibCheck: true` hides it, and the consumer sees the shim's
type at every use site.

A consumer that needs a global no `public_globals` names sees the identifier as
undefined. Three things supply it: a dep of its own (`@types/node` for
`process`), the owning target's `public_globals`, or a relative `types` entry
with the file in `types_srcs`, which stages it into this one program and no
other.

The unit is the file: TypeScript decides module-or-global per file. A `.d.ts`
mixing a shim for the package's own build with a declaration consumers are
meant to have is two files.

Every entry must be in `srcs`, and must be global. Naming a `.d.ts` with a
top-level import or export fails the build: a module has no globals. A `.d.mts`
or `.d.cts` is accepted wherever a `.d.ts` is.

`vite_types = True` follows the same rule. The shim is a src of the target that
sets the attribute and of no other, so a consumer using `import.meta.env` sets
`vite_types = True` itself. `ImportMeta` is in `lib`, so without the shim that
consumer sees
`TS2339: Property 'env' does not exist on type 'ImportMeta'`.

## Which Tool Emits the Declarations

Oxc always does the JavaScript transform. `declarations` decides which tool
produces the `.d.ts`.

### `declarations = "tsgo"` (default)

tsgo emits declarations from the complete type program: the target's sources
plus every transitive `.d.ts` and npm `package.json`.

- **No source annotations required.** Inferred export types are fine.
- **Declarations are exactly what `tsc` would emit**, including inferred object
  shapes, literal unions and `RegExp`.
- **Type errors fail `bazel build`.** The `.d.ts` are real outputs of the tsgo
  action, so a target with a type error produces nothing. No
  `--output_groups=+_validation` needed.
- **Type-checking is on the critical path.** A consumer waits for its
  dependency's declarations.

### `declarations = "oxc"`

Oxc emits declarations syntactically, per file, with no type program. This
requires [isolated declarations](../getting-started/isolated-declarations.md):
every export needs an explicit type, and Oxc **errors** when one does not have
one. Type-checking moves into the `_validation` output group, off the critical
path, so downstream targets compile while checking runs concurrently.

Use it per package once that package's exports are annotated.

### Turning tsgo Off

`enable_check = False` means different things in the two modes:

| | `enable_check = True` (default) | `enable_check = False` |
|---|---|---|
| `declarations = "tsgo"` | tsgo emits `.d.ts` and reports errors | no tsgo and no `.d.ts`, for terminal targets (app entries, dev servers, bundle inputs) whose declarations nothing consumes |
| `declarations = "oxc"` | Oxc emits `.d.ts`; tsgo validates | Oxc emits `.d.ts`; **nothing type-checks**. Oxc still enforces isolated declarations, so the declarations stay complete; the function bodies go unchecked |

`declarations = "oxc"` with `enable_check = False` is the only configuration
that runs no tsgo at all. Used broadly, it needs a type-checking gate somewhere
else in the build.

## Cost of Each Mode

Measured with:

```bash
tools/bench_declarations.sh 20 50 3
```

1,000 annotated files across 20 packages in one linear dependency chain,
medians of three interleaved runs:

| Mode | Rebuild wall | Critical path |
|------|--------------|---------------|
| `declarations = "tsgo"` | 6.3s | 4.89s |
| `declarations = "oxc"` | 3.8s | 2.15s |
| `declarations = "oxc"`, `enable_check = False` | 2.7s | 1.06s |

Both modes run tsgo once per target, so the gap is serialisation. Under `"oxc"`
the check is a validation action nothing waits for, and the critical path is
Oxc's per-file transform; under `"tsgo"` each of the 20 links waits for its
dependency's declarations. The gap shrinks on shallower graphs and widens on
deeper ones.

## Providers

Fields for all three, and the load path, are in
[Providers and Toolchains](providers.md).

- **`JsInfo`**: this target's `.js` and `.js.map` files as direct depsets, and
  the closure of both as transitive ones; `ts_binary` and `ts_bundle` read the
  transitive `.js` set
- **`TsDeclarationInfo`**: this target's declarations and their closure, plus
  the global-entry files `public_globals` produces; a downstream `ts_compile`
  type-checks against the closure
- **`TsModuleInfo`**: the `module_name` this target is importable as, plus the
  directories its declarations land in, propagated transitively so a dependent
  can build its own `paths` entries
- **`OutputGroupInfo(_validation=...)`**: the tsgo check stamp, written only
  under `declarations = "oxc"` with checking on; under the default the
  declarations are the tsgo action's own outputs.
- **`OutputGroupInfo(strict_deps=...)`**: the `TsStrictDeps` stamp, on any
  target with both `deps` and sources. The compile actions take it as an input, so a violation
  fails a plain `bazel build`; the output group exposes the stamp and the
  checker on their own.

## Architecture

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations), only under `declarations = "oxc"`
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

`TsStrictDeps` runs before the transform, as a Node action over a params-file
manifest of the target's declared and reachable providers. Its scanner is a
character walk over the source: a quoted string is a specifier only when the
tokens before it say so. Gazelle generates deps with the same walk.

tsgo runs as a separate Bazel action against the generated `tsconfig.json`.
Under `declarations = "tsgo"` that tsconfig sets `declaration`,
`emitDeclarationOnly`, `rootDir` and `outDir` so the emitted declarations land
beside Oxc's `.js` (mnemonic `TsgoDeclare`). Under `declarations = "oxc"` it
runs with `--noEmit` and writes only a stamp (mnemonic `TsgoCheck`).

## Output Paths

Output paths are derived from source file names, not target names, so
`import "./foo"` resolves to `bazel-bin/.../foo.js`. Two `ts_compile` targets in
the same package therefore cannot list the same source file: Bazel reports
conflicting actions. Split by directory, or give each target its own sources.
