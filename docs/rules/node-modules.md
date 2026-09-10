# node_modules

An importer's `node_modules`. One `node_modules` target per Bazel package
declares a symlink `node_modules/<name>` into [the store](#the-store) for
every npm package the importer declares, and one `node_modules_member` per
workspace member it links. `ts_codegen`, `ts_binary`, `ts_dev_server` and the
ruleset's `esbuild_bundle` take the target and stage its links and every store
tree they reach; `ts_compile` and `ts_test` take it as [the chain](#the-chain)
a direct npm dep resolves along.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules", "node_modules_member")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//web:react",
        "@npm//web:vite",
    ],
    parent = "//:node_modules",
)

node_modules_member(
    name = "node_modules/@acme/ui",
    member = "@npm//:acme_ui",
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
)
```

Gazelle writes both in every lockfile importer's package
([What Gazelle Writes](../gazelle/overview.md#what-gazelle-writes)); one in a
package that is no importer is hand-written under `# keep`.

## Attributes

`node_modules`:

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `deps` | `label_list` | `[]` | The npm packages the importer declares, as hub labels: the importer's own package (`@npm//web:react`) where it declares the name, the root's otherwise. Empty for an importer that declares nothing |
| `parent` | `label` | `None` | The `node_modules` target of the importer above; none in the lockfile's package |

`node_modules_member`:

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `member` | `label` | required | The hub's view of the workspace member, `@npm//:<name>`; the target is named `node_modules/<name>` |

## The Layout

Every entry is a declared symlink with a relative target into the lockfile
package's store, pnpm's top level:

```
node_modules/                                  ← the root importer's, beside .pnpm
  .pnpm/                                       ← the store: one tree per snapshot
    minimatch@9.0.9/node_modules/minimatch/
    minimatch@9.0.9/node_modules/brace-expansion → ../../brace-expansion@2.0.2/node_modules/brace-expansion
  minimatch → .pnpm/minimatch@9.0.9/node_modules/minimatch
  shared → .pnpm/shared@0.0.0/node_modules/shared   ← a node_modules_member
web/node_modules/
  react → ../../node_modules/.pnpm/react@19.0.0/node_modules/react
```

A package's own imports resolve from its realpath,
`.pnpm/<key>/node_modules/<name>/`, to the links beside its tree, so every
dependent reaches the resolution pnpm recorded for it: `test-exclude`'s
`minimatch` is 10.2.4 beside its tree while the importer links 9.0.9 at the
top. Node, tsgo and Vite realpath a package before resolving its imports;
`--preserve-symlinks` walks from the link and meets no edge, as over pnpm's
own install.

A `node_modules` target's outputs are `node_modules/<name>`, so one target per
Bazel package holds them, and they sit in the store's repository: a relative
link has one text in the execroot and the runfiles tree only while link and
target share one, so a dep whose store is another module's lockfile fails at
analysis naming it. `DefaultInfo.files` is every link and every store tree the
links reach; a consumer that runs outside an action stages them and takes the
directory, which no artifact names. `NodeModulesInfo` carries `label`, `dir`
(the directory as a bin-dir path), `links` (name to `NpmLinkInfo(link, store)`)
and `parent`, and a `node_modules_member` returns `NpmLinkInfo` beside the
view's `TsInfo` and `NpmPackageInfo`, so a target names it in `deps` where it
named the view ([Providers](providers.md#nodemodulesinfo)).

## One Link per Name

Two resolutions of one name in `deps` is one link name twice, and fails:

```
node_modules: @@//src/app:node_modules: 'minimatch' linked twice, to
@@+npm+npm__minimatch__10_2_4//:pkg and @@+npm+npm__minimatch__9_0_9//:pkg:
one link per name
```

Two peer resolutions of one version are two keys and fail the same way. Each
dependent reaches its own resolution beside its tree; the importer's `deps` is
what the importer itself declared, one resolution per name.

A workspace member in `deps` fails naming the `node_modules_member` to write
instead. The link is a target of its own because a member's compile walks up
to the importers above it: an importer target holding the member's link would
depend on the member's tree, and through it on the member's compile, a cycle
for every member the root links.

## The Store

Every lockfile has a virtual store, pnpm's `node_modules/.pnpm`, declared in
the lockfile's own package by `npm_virtual_store`, a macro the lockfile's hub
writes into `@<hub>//:defs.bzl` from the graph the `npm` extension computes,
named after the directory it declares:

```python
load("@npm//:defs.bzl", "npm_virtual_store")

npm_virtual_store(name = "node_modules/.pnpm")
```

The call declares, `manual` and public:

- one `npm_store` per snapshot, named after its tree,
  `node_modules/.pnpm/<key>/node_modules/<name>`, where `<key>` is
  `<name with / as +>@<version>` plus `_<peer id>` when pnpm resolved the
  package against a peer set (`NpmPackageInfo.peer_id`). Its one action,
  `NpmStore`, copies the fetched package's files into the tree with
  `tsaction stage`; beside the tree it declares one symlink per dependency
  edge the lockfile records, `node_modules/.pnpm/<key>/node_modules/<dep>` ->
  `../../<dep key>/node_modules/<dep>`, under the name the snapshot imports
  the dependency by, so an npm alias is a link name and not a second copy. An
  edge to a platform-partitioned snapshot is declared under the same
  `select()` the snapshot's repository writes, so no platform fetches
  another's tarball. An edge the extension cut to break a cycle in the target
  graph is a link all the same, with no dependency behind it: its tree is in
  a closure through the edge that closes the cycle, as it is in pnpm's store.
- one `npm_store_member` per workspace member whose BUILD file declares its
  target, `node_modules/.pnpm/<name with / as +>@0.0.0/node_modules/<name>`:
  the member's `package.json` as built (every source-file target rewritten to
  the emitted file, [what a workspace member is imported
  as](../guides/npm.md#what-a-workspace-member-is-imported-as)), written as a
  file beside the tree at `node_modules/.pnpm/<key>/package.json` and copied
  into it, with the member's `.js`, `.js.map`, `.d.ts` and data srcs at their
  package-relative paths, the source `package.json` excepted; and one link per
  dependency the member's importer declares, other members among them.
- one `npm_store_hoist`, `node_modules/.pnpm/node_modules`: pnpm's hidden
  hoist. For every name `hoist-pattern` matches it links one resolution at
  `node_modules/.pnpm/node_modules/<name>`, and for every name
  `public-hoist-pattern` matches one at the root importer's
  `node_modules/<name>`, so a package importing a dependency it does not
  declare resolves as it does in the checkout. The settings are the lockfile
  package's `.npmrc` -- `hoist`, `hoist-pattern`, `public-hoist-pattern`,
  `hoist-workspace-packages` -- and pnpm's defaults where it is silent: hoist
  on, `hoist-pattern` `*`, `public-hoist-pattern` empty, members hoisted. The
  resolution hoisted for a name follows pnpm's hoist step: the walk starts at
  every importer's direct dependencies, marks each level's children in order
  before it descends into the first child, and visits the snapshots it
  reached by depth and then by snapshot id; the first snapshot whose
  dependencies name a not-yet-taken name claims it, workspace members and the
  importers' own direct dependencies first (the root importer's are never
  hoisted: they sit at the root already), a snapshot skipped on the target
  platform claims nothing, and a name is taken case-insensitively. pnpm's
  tie-break at one depth is its graph's key: the snapshot id after a
  resolving install, the store directory's name after a frozen one, which
  spells `/` as `+` and a peer suffix's brackets as `_` and so can order two
  ids the other way round. A pattern is pnpm's: `*` matches any run, `!`
  negates, the last matching pattern decides. Where platforms disagree the
  link is under a `select()`.
  `tests/npm/hoisted_dependencies.bzl` is pnpm's own answer over the fixture
  lockfile, and `tests/npm:store_tests_hoist` asserts the store's.

A tree holds no symlink, so Bazel hashes it as files and restores it as files
from a disk or remote cache in every download mode, and a fresh output base
over a populated cache executes no `NpmStore`; the links are internal actions,
re-run per output base. The store sits in the lockfile's package because that
is pnpm's `node_modules/.pnpm`, and in the lockfile's repository because a
relative link has one text in the execroot and the runfiles tree only while
link and target share a repository: two lockfiles in one package would share
one store, so the `npm` extension refuses them, and a module's hub the
extension fills in from another module's lockfile serves that module's own
targets, never a consumer's.

Every `ts_npm_package` carries its snapshot's store as `NpmPackageInfo.store`
(`NpmStoreInfo`: `key`, `tree`, `links`, `transitive`, `manifest`), and a
member's hub view carries the member's. The importer's links above read it,
and the chain below is where `ts_compile` and `ts_test` find them.

## The Chain

`ts_compile` and `ts_test` build no tree. Each names in `node_modules` the
nearest lockfile importer at or above its package, and a direct npm dep
resolves along that target and its `parent`s nearest first, pnpm's walk-up
from the importing file: the link whose store is the dep's resolution is the
one the program reads. A name no importer on the chain links fails analysis
naming the nearest importer's `package.json`; a name linked at another
resolution fails naming the importer-scoped label, `@npm//web:marked`. A
workspace member is named by the importer's link target,
`//web:node_modules/@acme/ui`, and its store tree comes with the link.

The action stages the links and the store files the program reaches and
nothing else: the chain's links for the direct names, the `@types/<name>`
twin an importer on the chain links, the member links `deps` name, every store
tree and edge link their closures hold, and each first-party dep's
(`TsInfo.npm_files`). tsaction lays the exec root out again under the
target's output directory with each importer's `node_modules` at the
importer's directory -- the lockfile's root importer's at the program root's
`node_modules` -- so a source at `web/src/a.ts` walks up through
`web/node_modules` to the root's, and a dep's declaration under
`bazel-out/<cfg>/bin/packages/ui/` through `packages/ui/node_modules`
([ts_compile](ts-compile.md#the-node_modules-chain)). A `ts_test` runs in
the same layout: the runfiles hold the links and store files at their own
paths, `NODE_PATH` names the chain's directories nearest first, and where the
walk up from the tests meets no `node_modules` before the workspace's root
the launcher links the chain's root in there ([ts_test](ts-test.md)).

## npm_bin

`@rules_typescript//npm:defs.bzl` exports one more rule, `npm_bin`. It is what
every generated `@npm//:<pkg>_bin` label instantiates: `npm_import` writes one
per entry in a package's `bin` field, and `bazel run @npm//:vitest_bin -- --version`
runs it. Nothing in this repository writes one by hand; the labels are the
interface, documented under
[Bin scripts](../guides/npm.md#bin-scripts).

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_script` | `string` | required | The bin entry's path inside the package, e.g. `vitest.mjs` |
| `package_files` | `label_list` | `[]` | Every file of the package, from its `ts_npm_package` target |
| `optional_dep_packages` | `label_list` | `[]` | Sibling package targets holding the platform-specific native binaries the script resolves at run time. The launcher links them under a `node_modules/` so `require.resolve()` finds them inside a sandbox or a runfiles tree |
| `runtime` | `label` | `None` | A JS runtime binary for this target, taking priority over the `js_runtime` toolchain |

The runtime comes from the `js_runtime` toolchain when `runtime` is unset. The
launcher `cd`s to `RUNFILES_DIR` before running the script, which is why a
linter run through one gets execroot-absolute paths
([Lint § Paths](../guides/lint.md#paths)).
