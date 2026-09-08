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
`bazel_dep` above, and no Go toolchain of their own. Building the extension
fetches a Go SDK and its modules, on top of the Rust toolchain `oxc-bazel` needs.

Run Gazelle:

```bash
bazel run //:gazelle
```

## What a Run Reads

Five things, and no directive of its own.

1. **Every `tsconfig.json`**, listed through tsgo from the repository root:
   `tsgo -p <dir>/tsconfig.json --noEmit --listFilesOnly --explainFiles
   --pretty false`. The listing is the program's files and every edge between
   them -- each import, `/// <reference>` directive and `types` entry, with the
   file it resolved to. The binary is the toolchain's, carried in
   `gazelle_typescript`'s runfiles, so the program Gazelle reads is the one the
   build type-checks; `-ts_tsgo=<path>` names another. Two files are never
   listed: a root `tsconfig.json` whose extends chain sets neither `include`
   nor `files`, since tsgo would enumerate the whole repository, and a
   `tsconfig.json` `ts_refresh_tsconfig` wrote, built out of the very targets
   that would name it.
2. **`pnpm-lock.yaml`** at the repository root, read once: every name the hub
   declares, the importers and what each declares, and the `link:` entries
   that make a directory a workspace member.
3. **The nearest `package.json`** above a package: its `name`, and for a
   `ts_test` its `dependencies` and `devDependencies`.
4. **The vitest configs** the generated tests name, listed together in one
   tsgo run when the first `deps` is written: the runner imports the config,
   so its imports are the test's.
5. **The hand-written `ts_codegen` rules** in the BUILD files walked: their
   `outs`, and every `out_dir`, which is that target's output whatever a local
   run of the generator left on disk.

The listing resolves through the checkout's `node_modules`, so a repository
with a root lockfile is installed before a run. tsgo prints no line for an
import a missing install leaves unresolved, and a root `pnpm-lock.yaml` with no
`node_modules/.modules.yaml` beside it stops the run before any `deps` is
written. A package whose nearest `package.json` is no importer in the lockfile,
an island pnpm installed nothing for, is listed with `--traceResolution`, and
the specifiers its program could not resolve are printed, one line per such
`tsconfig.json`.

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
`tsconfig.json`, not under a deeper package or a declared `out_dir` -- is a
data src of the package: a `.json`, a `.css`, an image, a fixture, the
`package.json`. The one unlisted JavaScript that is a src is the twin beside an
owned declaration of the same stem (`x.mjs` beside `x.d.mts`): tsc drops it
from the program and resolves `./x.mjs` to the declaration, so the listing
never names the module itself.

Two programs importing each other's files is a dependency cycle between two
Bazel targets, which Bazel rejects with the loop of labels when it loads them.
Directories inside one program are one target, so a mutual import between them
is nothing.

### Where a Program Is Not

A directory that is not a package gets `Empty` for every kind Gazelle writes --
`ts_compile`, `ts_test`, `ts_lint`, `ts_config` and `filegroup(vitest_config)`
under the names it would use -- so a rule an earlier run left there is
withdrawn, and the run names the BUILD file to delete when one of them was in
it: Gazelle cannot delete the file, and an empty BUILD file keeps the directory
a Bazel package. Two rules are written there all the same: a `ts_config` over a
`tsconfig.json` a program's `extends` chain names (the root's shared base), and
at the repository root a `filegroup(vitest_config)` over a config the tests
below name. A directory inside a `ts_codegen`'s `out_dir` is that target's
output: nothing under it is a source, and a BUILD file there is emptied and
named the same way.

## What Gazelle Writes

| Rule | Name | Attributes Gazelle owns |
|------|------|-------------------------|
| `ts_compile` | the directory's basename, `root` at the repository root | `srcs`, `deps`, `tsconfig`, `visibility` |
| `ts_test` | `<basename>_test` | `srcs`, `deps`, `tsconfig`, `config` |
| `ts_config` | `tsconfig` | `src`, `deps`, `visibility` |
| `ts_lint` | `<basename>_lint` | `srcs`, `linter`, `linter_binary`, `config`, `fail_on_warnings` |
| `filegroup` | `vitest_config` | `srcs`, `visibility` |

Per package: a `ts_compile` when the program has a library file, holding the
library files, every owned declaration and the data files; a `ts_test` when it
has a test file, holding the test files and every owned declaration, and the
data files when no `ts_compile` is written; `tsconfig = ":tsconfig"` on both,
naming the `ts_config` over the package's own `tsconfig.json`; a `ts_lint`
beside the `ts_compile` while a linter config is in force
([below](#automatic-lint-targets)). A `ts_codegen` declared in the package's
BUILD file is a dep of every target there; Gazelle recognises the kind and
never writes one. A program listing only declaration files writes neither
target and says so under `-ts_verbose`. `ts_dev_server` is outside all of
this: Gazelle does not write or touch it ([Dev Server](../guides/dev-server.md)).
A src whose name Bazel cannot spell (a `:` in it) is dropped and named in the
log; a name that opens a label (`@`, `//`) is pinned to the package with a
leading `:`.

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
files. A `vitest.config.*` beside the tests goes into `config` by name. With
none there, Gazelle walks up to the directory plain `vitest` runs from, the
nearest one holding a `package.json` or the repository root, and takes the
config it holds: that directory gets a public `filegroup` named
`vitest_config` over the file, and the test names it by label. A directory
between the two with no `package.json` is passed over, as `vitest` run from the
package root passes it over. The directory holding the config has to be a
package itself, or the root, for the label to reach it; otherwise the run says
so and the test gets no `config`. Without the config the tests run in plain
Node, so a worker's `defineWorkersConfig` pool becomes no pool and a dependency
that only resolves through Vite (`test.server.deps.inline`) fails at import
time. Every import of the config, bare or relative, is a dep of the test: the
config is listed by tsgo from its own directory, as vitest loads it.

## The tsconfig and Its ts_config

Every target compiles under the package's own `tsconfig.json`: its `lib`,
`types`, `paths`, `jsx` and strictness. The rule reads every compiler option
from the tsconfig ([where compiler options come
from](../rules/ts-compile.md#where-compiler-options-come-from)), so no
attribute restates one, and the `ts_config` beside the file is what makes it a
label.

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
`types`, no filegroup. The checked-in copy of a declared out goes: a file that
is both a src and an output of its package is a conflict Bazel rejects. The
codegen is named for what it writes, never for its directory: the directory's
name is the package's `ts_compile`, and a hand-written rule of another kind
holding that name keeps the merger from writing the compile. The run names
such a rule; rename it.

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

## Automatic Lint Targets

When a linter config file is present in the package or any ancestor, and
`pnpm-lock.yaml` has the linter it is for, Gazelle writes a `ts_lint` beside
the package's `ts_compile`, named `<basename>_lint`, over the same TypeScript
sources. `linter_binary` is the hub's bin alias for the linter package,
`@npm//:oxlint_bin` or `@npm//:eslint_bin`.

Detected config files:

- **oxlint**: `oxlint.json`, `.oxlintrc.json`, `.oxlintrc`
- **eslint**: `eslint.config.mjs`, `eslint.config.js`, `eslint.config.cjs`,
  `.eslintrc.js`, `.eslintrc.cjs`, `.eslintrc.yaml`, `.eslintrc.yml`,
  `.eslintrc.json`, `.eslintrc`

oxlint configs are detected before ESLint configs. The closest config file wins.

A config on disk whose linter the lockfile never mentions gets no `ts_lint`:
the `eslint.config.js` of a nested `package-lock.json` island, or one left
behind by a package that was never installed. Its binary would be a target the
hub does not declare, and Bazel answers `no such target` by failing analysis
for every target in the package, not the lint alone. Gazelle says so once per
config file, naming the config, the package and the label, and withdraws a
`ts_lint` an earlier run wrote for it. The fix is to add the linter to the
workspace's dependencies or to delete the config. A workspace with no root
lockfile is not refused.

```python
ts_lint(
    name = "core_lint",
    srcs = [
        "src/index.ts",
        "src/parse.ts",
    ],
    config = "//:oxlint.json",
    linter = "oxlint",
    linter_binary = "@npm//:oxlint_bin",
)
```

To run linting:

```bash
bazel build //... --output_groups=+_validation
```

## Import Resolution

`deps` is written from the listing, one label per edge target. An edge is one
file reaching another: an import as written, a `/// <reference path>` or
`/// <reference types>` directive, a `compilerOptions.types` entry, the JSX
runtime a `.tsx` file imports under `react-jsx` without writing the import, the
`importHelpers` module. tsgo resolved each under the program's own options --
its `paths`, its `moduleResolution`, the `imports` map of its `package.json`,
the `exports` maps under `node_modules` -- so Gazelle reads no specifier and
applies no resolution rule of its own. It maps the file tsgo landed on to a
label:

- **A file under `node_modules`** is an npm package, named by the segments
  after the last `node_modules/` in the path (two when the first is a scope),
  or by the bare specifier's package when the edge is an import. The label is
  spelled through [the lockfile gate](#the-lockfile-gate). The `@types/*` twin
  of a package arrives paired through the hub in the forest and gets no label
  of its own.
- **A workspace member imported by its name** -- a bare specifier whose
  package is a `link:` name in the lockfile, or the manifest name of an
  importer -- is the hub's view of the member, `@npm//:<name>`, from every
  package but the member's own `ts_compile`, where the file is its own and the
  edge is nothing. A `ts_test` inside the member takes the view: the runtime
  resolves the name through `node_modules`, and only the view links the member
  there.
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
- **The toolchain's own libs** (`lib.dom.d.ts` and its kin, under `../`) are
  nothing.

Core Gazelle's `# gazelle:resolve typescript <repository path> <label>` names
the label for a first-party file and wins over every first-party case above; a
file under `node_modules` is the gate's alone
([`# gazelle:resolve`](directives.md#gazelleresolve)).

A `ts_test`'s `deps` is the union of `:<basename>`, the package's `ts_compile`
when there is one; the edges of every file the package owns -- the test files,
the library files and the declarations, since the test runs the package's code
and needs its npm closure; the edges of its vitest config; and the nearest
`package.json`'s `dependencies` and `devDependencies`, each spelled as an edge
would be, a member's name as its view and every other name through the gate.
So a test carries the packages the config and the manifest name and no source
imports (`vitest`, a pool package, `jsdom`), and none of them needs a `# keep`.

### The Lockfile Gate

Every npm label passes through the root `pnpm-lock.yaml`. A name the lockfile
never mentions -- one a nested `package-lock.json` installed, one a stale store
supplied -- gets no label and one line naming the importer, the specifier and
the name. The hub is built from that lockfile, so `@npm//:<name>` for such a
package is a target that cannot exist, and `no such target` fails analysis for
every target in the build where a missing dep fails one import with `TS2307`.
With no root lockfile at all, npm imports get no dep, said once per run.

The spelling follows the importer. The file tsgo listed carries the exact
version the importing file's own `package.json` resolved, and the hub declares
each importer's resolutions under the importer's directory beside the root's:
`@npm//web:marked` beside `@npm//:marked`. A name the nearest lockfile importer
above the importing file declares is spelled under that importer, and a name
only the root declares under the root. A flat label for a name two importers
resolve differently would put the root's version at the target's top level in
the forest, where the importer's own code expects its own.

## Verifying a Run

Taking Gazelle's output wholesale is the intended workflow.
`//tests/integration:gazelle_roundtrip_test` pins four properties in CI against
a real nested workspace: the output builds, generating it twice from scratch
produces byte-identical BUILD files, `bazel test //...` passes on that output,
and the set of test targets is unchanged across a delete-and-regenerate. It
runs on every pull request and on every push to `main`.

Check the test-target set on your own repository too. A run that deletes a test
still builds and is still idempotent:

```bash
bazel query 'tests(//...)' | sort > before
bazel run //:gazelle
bazel query 'tests(//...)' | sort | diff before -
```

Seven hand-written `go_test` targets once disappeared this way, through
Gazelle's Go language and not the TypeScript extension. Go turns
`# gazelle:exclude *_test.go` into a deletion stub named `<dirbase>_test`, and a
hand-written `go_test` of that name goes with it.

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
  would use -- `<dirbase>`, `<dirbase>_test`, `tsconfig`, `vitest_config` --
  so a rule of yours under one of them is proposed for deletion on every run.
  `# keep` above the rule holds it.
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
