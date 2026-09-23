# Gazelle Overview

Gazelle writes the BUILD files: one package per `tsconfig.json` program, its
targets' sources and deps read off tsgo's own listing of that program.

## Setup

Add the Gazelle binary to your root `BUILD.bazel`:

```python
load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_typescript",
    tags = ["manual"],
)
```

`gazelle_typescript` carries the TypeScript language alone. The other binary in
that package, `gazelle_ts`, also carries Go and proto; rules_typescript uses it
to generate BUILD files for its own `.go` sources. In a polyglot repo it
rewrites Go BUILD files too.

Add `gazelle` to `MODULE.bazel`:

```python
bazel_dep(name = "gazelle", version = "0.47.0")
```

`rules_typescript` declares `rules_go`, `go_sdk` and `go_deps` as non-dev
dependencies, so they propagate transitively via bzlmod. Consumers need only the
`bazel_dep` above, and no Go toolchain of their own. Gazelle compiles only
under `bazel run`: `tags = ["manual"]` keeps the target and its runner out of
`bazel build //...`, and the run fetches a Go SDK and its modules. `bazel build`
and `bazel test` compile no Go unless the workspace registers
[tsgo from source](../rules/providers.md#tsgo-from-source); the ruleset's
other Go tools are the binaries of its
[tools release](../RELEASE_PROCESS.md#tools).

Run Gazelle:

```bash
bazel run //:gazelle
```

## What a Run Reads

Five things, and no directive of its own.

1. **Every `tsconfig.json`**, listed through tsgo from the repository root:
   `tsgo -p <dir>/tsconfig.json --noEmit --listFilesOnly --explainFiles
--pretty false`. The listing is the program's files and every edge between
   them -- each import, module augmentation, `/// <reference>` directive and
   `types` entry, with the file it resolved to. The binary is the toolchain's,
   carried in `gazelle_typescript`'s runfiles, so the program Gazelle reads is
   the one the build type-checks; `-ts_tsgo=<path>` names another. Two files
   are never listed: a root `tsconfig.json` whose extends chain sets neither
   `include` nor `files`, since tsgo would enumerate the whole repository, and
   a `tsconfig.json` `ts_refresh_tsconfig` wrote, built out of the very targets
   that would name it.
2. **`pnpm-lock.yaml`** at the repository root, read once: every name the hub
   declares, the importers and what each declares, and the `link:` entries
   that make a directory a workspace member.
3. **The nearest `package.json`** above a package: its `name`, and for a
   `ts_test` its `dependencies` and `devDependencies`.
4. **The vitest configs** the generated tests name, listed together in one
   tsgo run when the first `deps` is written: the runner imports the config,
   so its imports, and those of the first-party modules they reach, are the
   test's.
5. **The hand-written `ts_codegen` rules** in the BUILD files walked: their
   `outs` and every `out_dir`, the target's output whatever a local run of the
   generator left on disk.

The listing resolves through the checkout's `node_modules`, so a repository
with a root lockfile is installed before a run. tsgo prints no line for an
import a missing install leaves unresolved, and a root `pnpm-lock.yaml` with no
`node_modules/.modules.yaml` beside it stops the run before any `deps` is
written. A `tsconfig.json` whose nearest `package.json` is no importer in the
lockfile is not listed at all: pnpm installs nothing for that project, so it is
foreign to the workspace ([The Package Model](#the-package-model)).

A run says nothing else about the listings, tsgo's diagnostics included.
`-ts_verbose` prints which binary ran, one line per `tsconfig.json` with what
it listed or why it was not, its diagnostics, how many are packages, and the
`.ts`/`.tsx`/`.mts`/`.cts` files no program lists, per directory and in total:
`bazel run //:gazelle -- -ts_verbose`.

## The Package Model

A directory is a package when its `tsconfig.json` lists a first-party file: a
path inside the repository and not under `node_modules`. `include`, `files` and
`exclude` are the program's, so they decide what the package compiles. A
directory with no `tsconfig.json`, or one whose listing names no first-party
file (a `TS18003` config with no inputs, the refused root), is not a package and
gets no target.

A `package.json` the lockfile has no importer for marks a foreign project:
pnpm installs nothing for it, so a bare import under it resolves through
whatever an importer above hoisted, or not at all. That directory and every
directory below it, down to the next `package.json` that is an importer, is
outside the package model: a `tsconfig.json` there is not listed and is no
package, its files are no src of the package above, a file of it a program
above lists is unowned, and the run names the manifest once. A project meant
to build is listed in `pnpm-workspace.yaml`.

A file belongs to the nearest package at or above its directory when that
package's program lists it. A file that package does not list belongs to no
package: every run from the repository root names each such file with the
programs that reached it, and so names a listed file under a directory the walk
did not enter (`# gazelle:exclude`, `.bazelignore`, `# gazelle:ignore`). A file
two programs list, a parent's and a nested package's, is the nested package's
alone, and the parent's edge to it is a dep.

Within a package, a `*.test.*` or `*.spec.*` file is a test file, a `.d.ts`,
`.d.mts` or `.d.cts` a declaration, and every other listed file a library
file. A file tsgo could have listed -- `.ts`, `.tsx`, `.mts`, `.cts`, `.js`,
`.jsx`, `.mjs`, `.cjs` -- is a src only when the program lists it: one the
tsconfig's `exclude` leaves out is neither a src nor data. Every other regular
file under the package's tree -- not a `BUILD.bazel`, not the package's own
`tsconfig.json`, not under a deeper package -- is a data src of the package: a
`.json`, a `.css`, an image, a fixture, the `package.json`. A file a
`ts_codegen` writes -- one its `outs` declare, or anything under its `out_dir`
-- is neither, listed or not: a copy on disk is what a local run of the
generator left, and the target that reaches it depends on the codegen. The one
unlisted JavaScript that is a src is the twin beside an owned declaration of
the same stem (`x.mjs` beside `x.d.mts`): tsc drops it from the program and
resolves `./x.mjs` to the declaration, so the listing never names the module
itself.

Two programs importing each other's files is a dependency cycle between two
Bazel targets, which Bazel rejects with the loop of labels when it loads them.
Directories inside one program are one target, so a mutual import between them
is nothing.

### Where a Program Is Not

A directory that is not a package gets `Empty` for every kind Gazelle writes --
`ts_compile`, `ts_test`, `ts_config`, `filegroup(vitest_config)` and
`filegroup(wrangler_config)` under the names it would use -- so a rule an
earlier run left there is withdrawn, and the run names the BUILD file to delete
when one of them was in it: Gazelle cannot delete the file, and an empty BUILD
file keeps the directory a Bazel package. Two things are written there all the
same: a `ts_config` over a `tsconfig.json` a program's `extends` chain names
(the root's shared base), and at the repository root the filegroups over a
config the tests below name and over the wrangler config it names. A directory
inside a `ts_codegen`'s `out_dir` is that target's output: nothing under it is
a source, and a BUILD file there is emptied and named the same way.

## What Gazelle Writes

| Rule                  | Name                                                    | Attributes Gazelle owns                                                                     |
| --------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `ts_compile`          | the directory's basename, `root` at the repository root | `srcs`, `deps`, `tsconfig`, `visibility`                                                    |
| `ts_test`             | `<basename>_test`                                       | `srcs`, `deps`, `tsconfig`, `config`, `config_srcs`, `wrangler_config`, `coverage_provider` |
| `ts_config`           | `tsconfig`                                              | `src`, `deps`, `visibility`                                                                 |
| `ts_dev_server`       | `dev` (`dev_server` when the compile target is `dev`)   | `entry_point`, `plugin`, `node_modules`, `visibility`                                       |
| `filegroup`           | `vitest_config`                                         | `srcs`, `visibility`                                                                        |
| `filegroup`           | `wrangler_config`                                       | `srcs`, `visibility`                                                                        |
| `node_modules`        | `node_modules`                                          | `deps`, `parent`, `hoist`, `visibility`                                                     |
| `node_modules_member` | `node_modules/<member name>`                            | `member`, `visibility`                                                                      |
| `npm_virtual_store`   | `node_modules/.pnpm`                                    |                                                                                             |

Per package: a `ts_compile` when the program has a library file, holding the
library files, every owned declaration and the data files; a `ts_test` when it
has a test file, holding the test files and every owned declaration, and the
data files when no `ts_compile` is written; `tsconfig = ":tsconfig"` on both,
naming the `ts_config` over the package's own `tsconfig.json`. A `ts_codegen`
declared in the package's
BUILD file is a dep of every target there; Gazelle recognises the kind and
never writes one. A program listing only declaration files writes neither
target and says so under `-ts_verbose`. An application with non-test program
sources and a package index.html or listed main.ts[x]/app.ts[x] also gets a
default oj dev target. Gazelle updates its entry point, npm tree, plugin and
visibility, and removes a generated dev target when its application entry
disappears. It preserves `server`, `port`, `host`, `open` and `# keep` values
([Dev Server](../guides/dev-server.md)).
A src whose name Bazel cannot spell (a `:` in it) is dropped and named in the
log; a name that opens a label (`@`, `//`) is pinned to the package with a
leading `:`.

Per lockfile importer, package or not: a `node_modules` whose `deps` are the
importer's declared `dependencies`, `devDependencies` and
`optionalDependencies` as hub labels (`@npm//web:react`; `@npm//:react` for
the root's) and whose `parent` is the importer above's target, the
lockfile's root importer naming `hoist = ":node_modules/.pnpm/node_modules"`
instead; one
`node_modules_member` per `link:` entry, `node_modules/<member name>` over the
member's view; and, at the repository root, the lockfile's package,
`npm_virtual_store(name = "node_modules/.pnpm")` from `@npm//:defs.bzl`
([node_modules](../rules/node-modules.md)). A directory that is no importer
withdraws its `node_modules` and every `node_modules_member`; a hand-written
one there is kept under `# keep`.

```python
# packages/core/BUILD.bazel -- tsconfig.json here, the sources under src/
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config", "ts_test")

ts_compile(
    name = "core",
    srcs = [
        "package.json",
        "src/index.ts",
        "src/parse.ts",
    ],
    tsconfig = ":tsconfig",
    visibility = ["//visibility:public"],
    deps = ["@npm//packages/core:zod"],
)

ts_test(
    name = "core_test",
    srcs = ["src/parse.test.ts"],
    tsconfig = ":tsconfig",
    deps = [
        ":core",
        "@npm//packages/core:vitest",
        "@npm//packages/core:zod",
    ],
)

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    visibility = ["//visibility:public"],
    deps = ["//:tsconfig"],
)
```

A `ts_test` runs under the vitest config plain `vitest` would read for its
files: a `vitest.config.*`, else a `vite.config.*`, in vitest's own order of
extensions (`.ts`, `.mts`, `.cts`, `.js`, `.mjs`, `.cjs`), so a package that
configures vitest through vite -- a `define`, a plugin, a `resolve.alias` --
runs as `pnpm vitest` runs it. One beside the tests goes into `config` by
name. With none there, Gazelle walks up to the directory plain `vitest` runs
from, the nearest one holding a `package.json` or the repository root, and
takes the config it holds: that directory gets a public `filegroup` named
`vitest_config` over the file, and the test names it by label. A directory
between the two with no `package.json` is passed over, as `vitest` run from the
package root passes it over. The directory holding the config has to be a
package itself, or the root, for the label to reach it; otherwise the run says
so and the test gets no `config`. Without the config the tests run in plain
Node, so a worker's `defineWorkersConfig` pool becomes no pool and a dependency
that only resolves through Vite (`test.server.deps.inline`) fails at import
time. Every import of the config, bare or relative, is a dep of the test: the
config is listed by tsgo from its own directory, as vitest loads it, and the
listing is followed through every first-party module it reaches. Those modules
are the test's `config_srcs`, spelled from the test's package -- a path under
it, or `//<package>:<file>` for a config an ancestor package exports -- and the
rule writes each at its own path in the runfiles, where the config's relative
imports resolve ([A config file](../rules/ts-test.md#a-config-file)). A module
outside the config's package is said and gets no entry: a config's modules are
its package's files.

### A Workers-Pool Config

A config that imports `@cloudflare/vitest-pool-workers` runs its tests inside
workerd, and the pool reads the wrangler config `wrangler.configPath` names --
that key alone; a `wrangler.jsonc` beside a config naming none is nothing to
it. Gazelle reads the one string literal in the config matching
`wrangler[\w.-]*\.(jsonc|json|toml)` and makes the file a label in the
config's package: `filegroup(name = "wrangler_config")` beside `vitest_config`,
withdrawn when the literal or the file goes. A config naming two files, or a
file outside its package, is said and gets none. The `ts_test` whose config's
edges name the pool gets `wrangler_config` -- the filegroup's label from a
package below, the file's name from the config's own -- and, when the lockfile
declares `@vitest/coverage-istanbul`, `coverage_provider = "istanbul"` with
that package in `deps` in the importer's spelling; the pool refuses v8
coverage, and without the package the run says so and writes neither. The
filegroup follows the literal and is written when the package generates; the
attributes follow the pool's edge and are written with `deps`, from the
combined listing. A config that names a wrangler config and installs no pool
gets the filegroup and no test names it.

## The tsconfig and Its ts_config

Every target compiles under the package's own `tsconfig.json`: its `lib`,
`types`, `paths`, `jsx` and strictness. The rule reads every compiler option
from the tsconfig ([where compiler options come
from](../rules/ts-compile.md#where-compiler-options-come-from)), so no
attribute restates one to the compiler. The `ts_config` beside the file is what
makes it a label, and it declares what the rule needs from the file before any
action reads it: `deps`, the `extends` chain, and the two values that name an
output -- `jsx = "preserve"` when that is the chain's effective `jsx`
([`jsx: preserve`](../rules/ts-compile.md#a-tsx-under-jsx-preserve)) and
`module` when the chain's is one tsgo emits
([The Module Format](../rules/ts-compile.md#the-module-format)). All three
are Gazelle's to recompute on every run.

`ts_config.jsx` is written as `"preserve"` when the chain's effective `jsx` is
`preserve`, read leaf-wins as tsc reads it and inherited through `extends`, and
removed otherwise; no other value is written, since no other value names an
output. `ts_config.module` is written the same way when the chain's effective
`module` is one tsgo emits -- `commonjs`, `node16`, `node18`, `nodenext`,
lowercased as tsgo prints it -- and removed for an ES kind or `preserve`,
which oxc emits with no twin to name.

`ts_config.deps` is the `extends` chain. Gazelle reads the package's
`tsconfig.json` and writes a dep on the `ts_config` of every `tsconfig.json`
its `extends` names, one specifier or an array, each a relative path resolved
from the file. A `tsconfig.json` that is no program of its own gets a
`ts_config` in its directory because a chain names it, which is how the root's
shared base becomes `//:tsconfig`. A base of another name
(`tsconfig.base.json`) has no `ts_config` to name, and the run says so: declare
that dep by hand under `# keep`. A specifier that resolves through
`node_modules` (`"@tsconfig/node20/tsconfig.json"`) is skipped with a warning,
since only configs on disk are read. Without the dep the base is no input to
the action, and tsgo reports `TS5083: Cannot read file` before it reaches the
sources.

```python
# workers/proxy/test/BUILD.bazel; its tsconfig.json extends ../tsconfig.json
ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    visibility = ["//visibility:public"],
    deps = ["//workers/proxy:tsconfig"],
)
```

`deps` is Gazelle's, recomputed on every run, so a run corrects the label when
the base moves or goes away. A hand-written value needs a `# keep` on its line
to survive the next run ([attributes Gazelle
owns](directives.md#attributes-gazelle-owns)).

### A Declaration the tsconfig Names

A path-shaped `compilerOptions.types` entry, `"./worker-configuration.d.ts"` in
the form wrangler writes, names a file the program stages, and the rule reads
the entry from the tsconfig itself. The listing carries it as an edge from the
tsconfig to the file, and it resolves as any edge does: to the rule whose
`srcs` hold the file, or to the `ts_codegen` whose `outs` declare it. An out
that is not in the checkout is listed by no program, so Gazelle reads the entry
from the chain itself, resolved against the package's directory as tsc
resolves it, and writes the codegen into `deps`. Nothing else is written: no
`types`, no filegroup. A copy of a declared out left on disk by a local
`wrangler types` is no rule's src: the out is the codegen's, and the program
that lists it depends on the codegen. The codegen is named for what it writes,
never for its directory: the directory's name is the package's `ts_compile`,
and a hand-written rule of another kind holding that name keeps the merger
from writing the compile. The run names such a rule; rename it.

```python
# workers/proxy/BUILD.bazel -- the ts_codegen is hand-written; Gazelle leaves it
ts_codegen(
    name = "worker_types",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    args = ["--config", "wrangler.jsonc", "--out", "{out}", "--srcs", "{srcs}"],
    generator = "@rules_typescript//tools/codegen:wrangler_types",
    node_modules = ":node_modules",
    visibility = ["//workers/proxy:__subpackages__"],
)

ts_compile(
    name = "proxy",
    srcs = [
        "src/handler.ts",
        "wrangler.jsonc",
    ],
    tsconfig = ":tsconfig",
    visibility = ["//visibility:public"],
    deps = [":worker_types"],
)
```

## Import Resolution

`deps` is written from the listing, one label per edge target. An edge is one
file reaching another: an import as written, a `declare module "<name>"`
augmentation of a module the program holds, a `/// <reference path>` or
`/// <reference types>` directive, a `compilerOptions.types` entry, the JSX
runtime a `.tsx` file imports under `react-jsx` without writing the import, the
`importHelpers` module. An augmentation adds no file -- its module is in the
program by an import elsewhere, or the checker reports `TS2664` -- and tsgo
lists it (`Augmented via`) under [the source-built
compiler](../rules/providers.md#tsgo-from-source), not the lockfile's binary.
tsgo resolved each under the program's own options --
its `paths`, its `moduleResolution`, the `imports` map of its `package.json`,
the `exports` maps under `node_modules` -- so Gazelle reads no specifier and
applies no resolution rule of its own. It maps the file tsgo landed on to a
label:

- **A file under `node_modules`** is an npm package: the bare specifier's
  package when the edge is an import and the lockfile mentions that name, else
  the package the path names by the segments after its last `node_modules/`
  (two when the first is a scope). The label is spelled through
  [the lockfile gate](#the-lockfile-gate). The `@types/*` twin of a package
  the lockfile mentions is the importer's to declare beside it and gets no
  label of its own; a `@types/<name>` package installed with no `<name>`
  beside it is the label, `@npm//web:types_mdast`.
- **A `/// <reference types>` directive in a file under `node_modules`** is the
  target's edge when the file it landed on is the `@types/<name>` package an
  importer at or above the target's package declares, spelled as that
  importer's, `@npm//:types_node`: TypeScript's primary lookup for the directive
  walks `node_modules/@types` up from the tsconfig's directory -- the chain --
  before the referencing file's own directory, so that copy is the one the
  program loads, and the link has to be staged for the build's program to load
  it too. A directive the chain does not answer resolved beside the referencing
  package's own tree and is nothing. The directive is the edge of the rule
  whose files reach the referencing file, the `ts_compile`'s or the `ts_test`'s.
- **A workspace member imported by its name** -- a bare specifier whose
  package is a `link:` name in the lockfile, or the manifest name of an
  importer -- is the name as the nearest importer at or above the package
  that has it resolves it: the importer's link target where it links the
  member, `//web:node_modules/@acme/ui`; the importer-scoped label where it
  declares the name with a version, `@npm//npm-packages/lovite:lovable-tagger`
  for lovite's `"lovable-tagger": "1.1.13"` beside the member
  `npm-packages/tagger`, since pnpm installs the published package there;
  where no importer above links or declares it there is no label and one
  line names the member, since a target resolves through its importers
  alone. The name of the nearest
  `package.json` above the importing file, a subpath included, is a
  self-reference, which tsc resolves through that manifest's `exports` to a
  file of the member: a first-party file, resolved as any owned file is -- the
  member's `ts_compile` from a `ts_test` or a package below it, nothing from
  the member's own `ts_compile` -- and no line.
- **A file another package owns**, reached by a relative path or a `paths`
  alias, is that package's `ts_compile`, or whichever rule holds the file in
  `srcs`: a hand-written one under `# keep` answers as a generated one does. A
  file this package's own rules hold is nothing.
- **A file under a `ts_codegen`'s `out_dir`** is the codegen, matched by the
  root the path sits under, deepest root first; a file a `ts_codegen` declares
  in `outs` is that codegen.
- **A file no package owns** gets no label and one line in the log naming the
  file, its importer and why: the nearest `tsconfig.json` above it does not
  list it, no `tsconfig.json` above it lists a file, or it sits under a
  directory this run did not walk.
- **The compiler's own libs** (`lib.dom.d.ts` and its kin: under `../` from
  the lockfile's binary, which reads them from beside itself, under
  `bundled:///libs/` from the source build, which embeds them) are nothing.

The tsgo action checks every edge against `deps` from the same listing
([Deps Have to Be Direct](../rules/ts-compile.md#deps-have-to-be-direct)), so
a build over what Gazelle wrote has no undeclared import to report;
`//tests/integration:gazelle_roundtrip_test` builds Gazelle's output and pins
it.

Core Gazelle's `# gazelle:resolve typescript <repository path> <label>` names
the label for a first-party file and wins over every first-party case above; a
file under `node_modules` is the gate's alone
([`# gazelle:resolve`](directives.md#gazelleresolve)).

A `ts_test`'s `deps` is the union of `:<basename>`, the package's `ts_compile`
when there is one; the edges of every file the package owns -- the test files,
the library files and the declarations, since the test runs the package's code
and needs its npm closure; the edges of its vitest config and of the modules
the config reaches; and the nearest
`package.json`'s `dependencies` and `devDependencies`, each spelled as an edge
would be, a member's name as its link target and every other name through the
gate.
So a test carries the packages the config and the manifest name and no source
imports (`vitest`, a pool package, `jsdom`), and none of them needs a `# keep`.

`runner` and `data` are the owner's attributes: Gazelle writes neither, and a
hand-written value survives every run without `# keep`
([Runners](../rules/ts-test.md#runners)).

### The Lockfile Gate

Every npm label passes through the root `pnpm-lock.yaml`. An edge the lockfile
never mentions, under the specifier's package or the listed file's -- one a
nested `package-lock.json` installed, one a stale store supplied -- gets no
label and one line naming the importer, the specifier and the name. The hub is
built from that lockfile, so `@npm//:<name>` for such a package is a target
that cannot exist, and `no such target` fails analysis for every target in the
build where a missing dep fails one import with `TS2307`.
With no root lockfile at all, npm imports get no dep, said once per run.

The spelling follows the importer. The file tsgo listed carries the exact
version the importing file's own `package.json` resolved, and the hub declares
each importer's resolutions under the importer's directory beside the root's:
`@npm//web:marked` beside `@npm//:marked`. A name is spelled under the nearest
importer on the chain above the importing file that declares it, the root
last. A flat label for a name two importers
resolve differently names a resolution the target's chain does not link, and
fails analysis. Every `ts_compile` and `ts_test` gets `node_modules`, the
nearest lockfile importer's target at or above the package -- the root's for a
package under no importer -- which is that chain.

## Verifying a Run

`//tests/integration:gazelle_roundtrip_test` pins four properties against a
nested workspace: the output builds, generating it twice from scratch produces
byte-identical BUILD files, `bazel test //...` passes on that output, and the
set of test targets is unchanged across a delete-and-regenerate. A run that
deletes a test still builds and is still idempotent, so check the test-target
set on your own repository:

```bash
bazel query 'tests(//...)' | sort > before
bazel run //:gazelle
bazel query 'tests(//...)' | sort | diff before -
```

Gazelle's Go language, in `gazelle_ts`, turns `# gazelle:exclude *_test.go`
into a deletion stub named `<dirbase>_test`, and a hand-written `go_test` of
that name goes with it.

### Getting the Clean-Tree Diff to Empty

Once a repository has settled, a Gazelle run on an unmodified checkout should
change nothing. Check without writing anything:

```bash
bazel run //:gazelle -- -mode=diff
```

Three things commonly keep that diff non-empty on a hand-written BUILD file,
and none is drift:

- **Gazelle's own rendering.** It writes a one-element list inline
  (`deps = ["//pkg"]`) and names a generated file by its producing label where
  you wrote the filename. Reformat the file to match.
- **A hand-written rule under a name Gazelle would use.** In a directory that
  is no package, every rule Gazelle would write is withdrawn under the names it
  would use -- `<dirbase>`, `<dirbase>_test`, `tsconfig`, `vitest_config`,
  `wrangler_config` -- so a rule of yours under one of them is proposed for
  deletion on every run. `# keep` above the rule holds it.
- **A hand-narrowed attribute it merges.** `visibility` is a merged attribute
  and generated rules carry `//visibility:public`, so a target restricted to
  `["//myapp:__subpackages__"]` comes back public on every run. Pin it with
  `# keep`:

  ```python
  ts_compile(
      name = "internal",
      srcs = ["index.ts"],
      # keep
      visibility = ["//myapp:__subpackages__"],
  )
  ```

  `# keep` is Gazelle's own directive. Above an attribute it means "never
  touch this value"; above a whole rule, "never touch this rule". See the
  [Directives Reference](directives.md).

## Refreshing adopted source inventories

`//gazelle/sources:gazelle` refreshes only `srcs` on an existing canonical
`ts_compile` (the directory's basename, or `root` at the repository root).
Use it as a separate invocation with Gazelle's `generation_mode update_only`.
The directory's existing `tsconfig.json` remains the compiler input owner.
The operation preserves dependencies, emission settings and other target families;
it does not adopt directories or resolve new imports.

Gazelle's existing BUILD boundaries determine ownership. A nested tsconfig
without a BUILD stays within its parent package. Existing child BUILD files
remain boundaries even when they declare no TypeScript compile target.
The same compiler listing and data-file classification as normal generation
supply the refreshed sources, including nested files collected by `update_only`.

Custom Gazelle binaries can register `//gazelle/sources:sources`, which exports
`NewSourceInventoryLanguage` from the TypeScript language package. Run this
operation separately from the ordinary language; both use the same config key.
Root language and exclude directives still apply. Callers needing different
traversal policy can supply a rebuildable BUILD-file view with Gazelle's standard
read/write BUILD-directory flags while leaving `repo_root` at the source tree.
