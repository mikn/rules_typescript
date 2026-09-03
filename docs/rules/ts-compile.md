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
| `srcs` | `label_list` | required | `.ts`, `.tsx`, `.d.ts`, `.js`, `.mjs` or `.cjs` files. See [Sources](#sources) |
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
| `types` | `string_list` | `None` | `compilerOptions.types` — which ambient type packages load. `[]` loads none; relative entries resolve against this target's package, from `srcs` or `types_srcs`. See [A `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file) |
| `jsx_import_source` | `string` | `None` | `compilerOptions.jsxImportSource`, e.g. `"solid-js"`, `"preact"` |
| `compiler_options` | `dict` | `None` | Any other `compilerOptions`, passed through verbatim |
| `module_name` | `string` | `""` | The bare specifier this target is importable as, e.g. `"@acme/ui"` |
| `path_aliases` | `string_dict` | `{}` | Alias prefix → workspace-relative source directory. Must resolve to files this target stages: its own `srcs`, or `path_alias_srcs` |
| `path_alias_srcs` | `label_list` | `[]` | Files a `path_aliases` entry resolves to when they are not in `srcs`. They join this target's type program, so a type error in one of them fails this target |
| `types_srcs` | `label_list` | `[]` | The declarations a relative `types` entry names, when neither `srcs` nor a dep stages them. See [A `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file) |
| `public_globals` | `label_list` | `[]` | The `.d.ts` in `srcs` whose globals every consumer gets too. Unnamed is private. See [Which ambients a consumer gets](#which-ambients-a-consumer-gets) |
| `untyped_packages` | `string_list` | `[]` | npm packages this target's type program leaves out entirely — no `paths` key, no `files` entry. See [Keeping a package out of the program](#keeping-a-package-out-of-the-program) |
| `vite_types` | `bool` | `False` | Prepend the Vite ambient type shim to `srcs` |

### Sources

A `.js`, `.mjs` or `.cjs` src is staged into the output tree unchanged and joins
the type program. The rule sets `allowJs` for it, so its JSDoc types cross the
package boundary; add `checkJs` through `compiler_options` to have its own body
checked.

A `.jsx` src is rejected at analysis time, because oxc has no output extension
for one. The message says to rename it `.tsx`.

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
| `foo.d.ts` | Declaration file — the compilation boundary |

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

1. **The ruleset baseline** — `strict`, `module: "Preserve"`, `skipLibCheck`,
   `esModuleInterop`. Applied in both modes: without a `tsconfig` they go
   straight into the generated file, and with one they go into a file the
   generated config `extends` **before** yours, so they reach only the keys your
   file never mentions.

   `moduleResolution: "Bundler"` joins them only where the baseline also owns
   the `module` it belongs to: without a `tsconfig`, and while
   `compiler_options` names neither key. TypeScript couples the two, so a
   baseline `moduleResolution` still standing after your `module: "NodeNext"`
   replaced the baseline's is `TS5109` before a source is read. Left out, tsgo
   derives the resolver from whichever `module` won — `Bundler` for all of them
   but `Node16`/`NodeNext`, which derive their own.
2. **`tsconfig`** — the project's own `tsconfig.json`, and whatever it extends.
   Referenced where it lives, never copied, so relative paths inside it still
   resolve against the directory they were written for. Everything it says wins
   over layer 1, so tsgo checks the code under the options `tsc` would — and
   setting the attribute adds what the file says instead of taking the baseline
   away.
3. **`target` and `jsx_mode`**, then `jsx_import_source`, `lib`, `types`, then
   `compiler_options`, which wins among these.
4. **The options Bazel owns** — `paths`, `include`, and the 16 keys listed
   below.

`target` and `jsx_mode` are injected in every mode, including over a tsconfig
baseline, and supersede a `target` or `jsx` in the file. Oxc transforms with
them, and the two compilers have to agree.

One option rides in unasked above layer 2: `allowArbitraryExtensions`, which the
`.d.ts` files generated for `css_module` / `css_library` / `asset_library` /
`json_library` deps require. It overrides the value in your tsconfig file; to
get your own value back, set it in `compiler_options`, which sits above it.

Layer 1 is a real file — `<target>.tsconfig_baseline.json` beside the generated
config — because Starlark cannot read your tsconfig to see which keys it
already sets. TypeScript settles that itself: `extends` takes a list, and a
later entry overrides an earlier one.

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
Remove "paths" from compiler_options on //src/app:app.
```

**A `path_aliases` value pointing into the output tree.** A path under
`bazel-out/` or `bazel-bin/` embeds the build configuration, so it breaks under
`-c opt` or a different exec platform:

```
ts_compile: path_aliases["@acme/ui"] on //src/app:app points into the output
tree (bazel-out/k8-fastbuild/bin/packages/ui).
```

Set `module_name` on the producing target.

### Generated and Checked-In Sources in One Target

One tsgo declaration emit has one `rootDir`. A checked-in source hangs off the
package directory, a generated one off the package's directory in `bazel-bin`.
A target holding both fails at analysis under the default emit:

```
ts_compile: srcs on //src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

Put the generated sources in their own target and depend on it, or set
`declarations = "oxc"` (which groups sources by root and runs oxc once per
group), or set `enable_check = False`. Neither of the last two emits from tsgo.
Generated sources cannot use the default emit even on their own — see
[ts_codegen § Compiling the output](ts-codegen.md#compiling-the-output).

### A Src From Outside This Package's Tree

The same rule, one layer out, and the one half of it a `srcs` list can decide on
its own — so it is decided while the BUILD file loads, before anything is
analysed. A src is compiled into the package of the target that **lists** it:
its outputs are declared under that package. The root its package-relative path
hangs off, though, is where the file actually lives, so a src from a sibling
package, from an ancestor, or from another repository hangs off a root of its
own while this package's sources hang off the package:

```
ts_compile: srcs on //src/app:app mix this package's own files with files that
live outside it:
  //packages/shared:util.ts
```

Give the file a target in its own package and depend on that; set `module_name`
on it when the import is by bare specifier. The same two escape hatches apply,
and the check is skipped when either is set.

Only the **mix** is rejected, and five shapes are not it:

- A **descendant package's** file is already inside this package's directory,
  which is the root this package's own sources hang off: `srcs = ["a.ts",
  "//src/app/sub:x.ts"]` in `//src/app` is the one `rootDir` `src/app`, and
  builds. A `ts_compile` may hold a whole subtree, and a subtree may grow a
  BUILD file without the target above it having to be split. What it must not
  do is compile a file the descendant package **also** compiles: both declare
  `bazel-bin/src/app/sub/x.js`, and Bazel rejects that pair as conflicting
  actions, naming both targets.
- A target whose srcs **all** come from elsewhere has one root like any other.
- The **top-level package** is the exec root, which is the root a src from any
  other package hangs off too — so `srcs = ["main.ts", "//lib:util.ts"]` at the
  repository root is one root, not two, and builds.
- A **`.d.ts`** from anywhere is passed through rather than compiled, which is
  what lets `vite_types = True` prepend the Vite shim from
  `@rules_typescript//ts`.
- A **`select`** resolves after the loading phase, so this check never sees its
  branches. A mix hidden inside one still reaches the analysis-time error.

Only a label that names a **source file** is judged. `//other:some_target`
stands for whatever files that rule or filegroup produces, wherever those live,
which the loading phase cannot know; the analysis-time root check is what covers
them.

A label naming a repository — `@other//pkg:f` — is outside this tree whatever
its path. `@//pkg:f` and the canonical `@@//pkg:f` are not: an empty repository
part is this repository, so `@@//<this package>:f` is this package. A repository
that names **itself** by its own apparent name is the one shape read as foreign
when it is not; spell it `//pkg:f`.

## Deps Have to Be Direct

A source may import only what a **direct** dep provides. Every `ts_compile`
target that has both sources and `deps` runs a `TsStrictDeps` action, which
reads those sources and fails on any specifier that resolves only because it
arrives through another dep's own deps:

```
ERROR: .../src/app/BUILD.bazel:3:11: TsStrictDeps //src/app:app failed: (Exit 1)
//src/app:app imports a module no direct dep provides:

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

Resolution becomes direct; the action inputs do not. One `paths` map serves the
whole type program, so dropping the transitive entries would stop a declared
dep's own `.d.ts` from resolving its imports, which TypeScript widens to `any`
and does not report. They stay available, and `TsStrictDeps` is what stops your
own source from leaning on them.

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
not. A `path_aliases` prefix outranks both -- it is the consumer naming the
module outright.

Without the second key the declarations are in the program under a name nothing
imports, and the failure is not yours to see: the imports that need it are
mostly inside *other packages'* `.d.ts` files -- `rollup`'s say
`from "estree"`, `@types/chai`'s say `from "deep-eql"` -- where `skipLibCheck`
hides the `TS2307` and the types those files export widen to `any` instead. See
[Finding a broken declaration](#finding-a-broken-declaration).

### Finding a Broken Declaration

The baseline sets `skipLibCheck: true`, so a `.d.ts` whose own imports do not
resolve reports nothing. What it exports becomes `any`, and the first visible
error is somewhere else entirely -- typically a `TS7006` on a callback
parameter in application code, whose real cause is a package the program never
loaded.

`--//ts:lib_check` turns `skipLibCheck` off for every target in the build, over
whatever `compiler_options` or a named `tsconfig` say:

```bash
bazel build //... --//ts:lib_check
```

Expect findings unrelated to the one you are chasing -- a `lib` a dependency
needs and the program does not set reports here too. It is a diagnostic sweep,
not a mode to build in.

### Keeping a Package Out of the Program

A `.d.ts` with no top-level import or export is a **global script**, and
everything it declares belongs to every program the file is part of. A dynamic
`import()` loads a module's declarations exactly like a static one, and the
package one hop behind it comes along.

That is how one line in a browser component broke 21 files in the Lovable
monorepo. `void import("@sentry/cloudflare")` sat inside an
`import.meta.env.SSR` branch; `@sentry/cloudflare`'s own declarations import
`@cloudflare/workers-types`, which is 15k lines of global script, so its
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

A named package gets no `paths` key — not its own, not one per `exports`
subpath, and not the bare name a `@types/*` package would answer for it — and
no `files` entry. It stays in `deps`, its files stay among the action's
inputs, and no JavaScript moves.

`TsStrictDeps` sees the exclusion, though. A **direct** dep stays declared, so
an import of it is still attributed. A package that was only **reachable** —
the case this attribute is for — leaves the reachable set with the key, so an
import of it is a bare `TS2307` rather than "add this dep". That is the honest
answer: adding the dep back would not type it, because the exclusion is what
took the types away.

An entry names one package, and a package's declarations live wherever npm put
them. `ms` ships none and is typed by `@types/ms`, so `["@types/ms"]` is the
entry that takes those declarations out — after which `ms` resolves to the
runtime package it names rather than being redirected into them. `["ms"]` takes
away the bare name `@types/ms` answered for it. Name both to leave nothing.

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
needs the package resolves it as before, which is what lets a worker entry
point keep the Cloudflare types the browser target drops.

**The editor is workspace-wide.** The tsconfig `bazel run //:refresh_tsconfig`
writes has one `paths` map for the whole workspace — a nested tsconfig extends
the root and inherits it unchanged. Where every target reaching a package
excludes it, the editor drops it too with no further configuration. Where one
target excludes a package another still resolves, one map has no way to answer
both, and `ts_refresh_tsconfig` fails rather than let an editor report what a
build does not:

```
ts_refresh_tsconfig: //web:web keeps "@cloudflare/workers-types" out of its type program
  (untyped_packages), and this config still resolves it for something it
  reaches. ...
Add "@cloudflare/workers-types" to host_only_packages to drop it from the editor everywhere,
or name it in untyped_packages on the targets that still resolve it.
```

### Importing Another Target by Bare Specifier

`path_aliases` maps a prefix to a source directory, which is right for
`@/components` → `src/components`. The rule maps the prefix onto that directory
and its `bazel-bin` mirror, so a `css_library`, `asset_library` or
`json_library` declaration for a file under it resolves too -- but a target
whose declarations land anywhere else is out of reach, because only that target
knows where they go under the current configuration. Set `module_name` there
instead:

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

A pnpm workspace member linked through `@npm//:<name>` gets more than that: its
own `package.json` decides. The `exports` map, `typings`, `types` and `main` are
read where the lockfile is, and each specifier the member declares becomes its
own `paths` entry -- so `@acme/ui/button` resolves to the declaration emitted
from whatever file `exports["./button"]` names, four directories down if that is
where it is. A wildcard subpath (`"./icons/*": "./icons/components/*.tsx"`)
becomes a wildcard pattern. The guesses above stay behind every declared entry,
so a manifest naming a file this build does not produce is no worse than a
manifest nobody read. See [npm workspace members](../guides/npm.md).

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

### Worked example: a Cloudflare Worker

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
stages (both below). Neither is what puts a *dep's* globals in scope: a `.d.ts`
in another target's `srcs` with no top-level import or export declares globals,
and those reach every target that depends on it -- however far down the graph
the declaration sits, the scope a single `tsc` run over the same sources would
give it -- when that target names the file in `public_globals`.

### A `types` entry that names a package

`types = ["vite/client"]` names a package, and TypeScript would resolve it by
walking `node_modules` for it. There is no `node_modules` here, so the rule
resolves the entry against this target's `deps` and puts the declaration the
package's own manifest designates into the generated config's `files`. Three
spellings resolve: the package itself, one of its `exports` subpaths, and the
bare name a paired `@types/*` package supplies (`node` is `@types/node`).

An entry no dep answers fails at analysis, naming the entry and the dep to add.
It has to, because nothing downstream would say it: `tsc` reports
`error TS2688: Cannot find type definition file` for such an entry, and tsgo --
the compiler this ruleset runs -- reports nothing at all and exits 0. The
failure surfaces later, on whatever used the declarations that never arrived:
`TS2339` on `import.meta.env` without `vite/client`, `TS2591` on `process`
without `node`.

Two ways out other than the dep: name a declaration file instead (the next
section), or state a `typeRoots` in `compiler_options` -- a target that does is
exempt, since what sits under a `typeRoots` is the compiler's to find at action
time and the rule cannot see it. The `typeRoots` exemption covers the package
entries only; a declaration file is a path in the sandbox either way.

### A `types` entry that names a declaration file

`types = ["./worker-configuration.d.ts"]` names a path, and a path resolves
against the sandbox: only what this target's action stages is in it. So the
entry is resolved against the source files it stages -- its `srcs`, its deps'
passed-through `.d.ts` (a `.d.ts` in `srcs` is a declaration output unchanged,
so a dep edge stages it at its source path), its `path_alias_srcs`, and
`types_srcs` -- and an entry none of them sits at fails at analysis, for the
reason a package entry does: tsgo reports nothing for a `types` entry that
resolves to nothing, and the target compiles without the declarations it asked
for.

A dep's *generated* declaration is not nameable: the entry resolves against the
source tree and that file is in `bazel-out`. Depend on the target and have it
name the file in `public_globals`, which is the route a generated ambient takes
into a consumer's program.

```python
ts_compile(
    name = "lib",
    srcs = glob(["*.ts"]),
    types = ["../../worker-configuration.d.ts"],
    types_srcs = ["//workers/proxy:worker-configuration.d.ts"],
)
```

`types_srcs` is for the file neither `srcs` nor a dep stages. It is a label
list, so the file may live in another package, and -- unlike a `.d.ts` in
`srcs` -- it is not passed through as this target's own declaration. tsgo parses
it as part of this program, so a syntax error in the file fails this target
(`TS1434` and friends); what it declares goes unchecked, since it is a `.d.ts`
under the baseline's `skipLibCheck` -- `--//ts:lib_check` or
`compiler_options = {"skipLibCheck": False}` is what surfaces a type error
inside it. A file listed there that no entry names is an analysis error too:
nothing else puts it in the program, so it would be staged and unread.

Only the two relative shapes are paths. TypeScript resolves `./x.d.ts` and
`../x.d.ts` against the config's own directory, and anything else -- `x.d.ts`,
`vendor/x.d.ts` -- through `typeRoots` and `node_modules/@types`, which is a
walk the compiler does at action time and this rule neither rewrites nor
resolves. `./typings`, a directory, is the compiler's too: which declaration
inside it the name picks is a question only reading the directory answers.

Globals are what such an entry is for. A module -- a `.d.ts` with a top-level
import or export -- resolves and joins the program, but its declarations stay
scoped to it, so nothing global arrives; `public_globals` rejects a module
outright and this does not, because a module in the program is what a module
augmentation inside it needs.

Only the attribute is checked. A `types` in the `tsconfig` file the target names
is a layer the rule does not read, so nothing there is resolved and nothing
there is guarded: with `"types": ["vite/client"]` in the tsconfig and
`@npm//:vite` in `deps`, the target analyses without complaint, generates a
config whose `files` is empty, and fails in tsgo with `TS2339` on the
`import.meta.env` those declarations would have typed. Put the entries in
`compiler_options`.

### When two ambients declare the same thing

A dep's globals are listed ahead of the ones `types` and `@types/*` packages
supply, so where both declare the same `declare module` pattern the project's
own wins. That is what `tsc` does natively -- a `types` package arrives as a
type-reference directive, which joins the program after the root files -- and
it is the only lever there is: the first declaration of a pattern wins, and a
narrower pattern does not change that. An earlier `declare module "*.svg"`
beats a later `declare module "*.icon.svg"` even for `star.icon.svg`.

To let a package's ambient win instead, drop the project's competing
declaration.

### Which ambients a consumer gets

That scope is right about TypeScript and not always right about packaging, so
`ts_compile` does not give it to a consumer unless the owning target asks. A
`.d.ts` in `srcs` types the target that owns it; `public_globals` is what also
puts it in the program of everything that depends on that target.

```python
ts_compile(
    name = "worker_types",
    srcs = ["worker-configuration.d.ts"],
    public_globals = ["worker-configuration.d.ts"],
)
```

Unnamed is private, and that is the default because the other way round is
silent. A package can hold an ambient it needs for its own standalone
`tsc -p` that is no part of its public type surface -- the usual one being a
`process` shim in a library with no `@types/node`:

```python
ts_compile(
    name = "ui",
    srcs = glob(["**/*.tsx"]) + ["types/ambient.d.ts"],
    tsconfig = "tsconfig.json",
)
```

Exported, that shim lands in `files` ahead of `@types/node` in every consumer
that has the real `process`, and the duplicate identifier is reported inside a
`.d.ts`, where `skipLibCheck: true` hides it. What the consumer sees is the
shim's type at every use site and a diagnostic about a package it has never
heard of.

A consumer that turns out to need a global no `public_globals` names sees the
identifier as undefined: nothing distinguishes a global that stayed private
from one that never existed. Give that consumer the declaration through a dep
of its own -- `@types/node` for `process` -- or name the file in the owning
target's `public_globals`.

The unit is the file, because the module-or-global question TypeScript answers
is per file. A `.d.ts` mixing a shim for the package's own build with a
declaration consumers are meant to have is two files.

Every entry must be in `srcs`, and must be global. Naming a `.d.ts` with a
top-level import or export fails the build rather than passing as a no-op: a
module has no globals, so exporting them states nothing true about the file.

`vite_types = True` is this rule applied to the shim it prepends. The shim is a
src of the target that sets the attribute and of no other, so a consumer using
`import.meta.env` sets `vite_types = True` itself. `ImportMeta` is in `lib`, so
what that consumer sees is
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
| `declarations = "tsgo"` | tsgo emits `.d.ts` and reports errors | **no tsgo, and no `.d.ts`** — an opt-out of types, right for terminal targets (app entries, dev servers, bundle inputs) whose declarations nothing consumes |
| `declarations = "oxc"` | Oxc emits `.d.ts`; tsgo validates | Oxc emits `.d.ts`; **nothing type-checks**. Oxc still enforces isolated declarations, so the declarations stay complete; the function bodies go unchecked |

`declarations = "oxc"` with `enable_check = False` is the only configuration
that runs no tsgo at all. Used broadly, it needs a type-checking gate somewhere
else in the build.

## Cost of Each Mode

One measurement, one machine, one day, reproducible from this tree:

```bash
tools/bench_declarations.sh 20 50 3
```

1,000 annotated files across 20 packages in a single linear dependency chain
(so the critical path has somewhere to show up), medians of three interleaved
runs:

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

- **`JsInfo`** — transitive depset of `.js` files, used by `ts_binary` and `ts_bundle`
- **`TsDeclarationInfo`** — depset of `.d.ts` files, used by downstream `ts_compile` targets for type resolution
- **`TsModuleInfo`** — the `module_name` this target is importable as, plus the
  directories its declarations land in, propagated transitively so a dependent
  can build its own `paths` entries
- **`OutputGroupInfo(_validation=...)`** — the tsgo check stamp, written only
  under `declarations = "oxc"` with checking on; under the default the
  declarations are the tsgo action's own outputs.
- **`OutputGroupInfo(strict_deps=...)`** — the `TsStrictDeps` stamp, on any
  target with both `deps` and sources. The compile actions take it as an input, so a violation
  fails a plain `bazel build`; the output group exposes the stamp and the
  checker on their own.

## Architecture

The oxc-bazel binary processes each `.ts` file through:

1. Parse (oxc_parser)
2. Semantic analysis (oxc_semantic)
3. Isolated declarations emit (oxc_isolated_declarations) — only under `declarations = "oxc"`
4. TypeScript/JSX transform (oxc_transformer)
5. Code generation (oxc_codegen) for `.js` + `.js.map`

`TsStrictDeps` runs before the transform, as a Node action over a params-file
manifest of the target's declared and reachable providers. Its scanner is a
character walk over the source: a quoted string is a specifier only when the
tokens before it say so. Gazelle generates deps with the same walk, so the two
cannot demand different things.

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
