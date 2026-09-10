# node_modules

Creates a hermetic `node_modules` directory in the Bazel sandbox holding exactly
the packages named and their transitive dependencies.

Every `ts_compile` and `ts_test` builds one from `deps` as the forest its tsgo
action walks, and a test's is the tree its tests run in (see
[ts_test](ts-test.md)); both go through this rule's builder. A hand-written one
covers a program or tool that needs packages on disk at runtime.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
)
```

One tree can serve several targets in the same package, keeping one copy in the
sandbox.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `deps` | `label_list` | required | npm package targets from `@npm` to include in `node_modules` |

## The Layout

The tree is pnpm-shaped, and it is the same tree whether a test runs on it or
tsgo type-checks against it: every package sits at `node_modules/<name>` with
its own files, a workspace member sits there as its hub view links it (its
`package.json` as built beside its `.js` and `.d.ts`), and a `@types/*` package
sits beside the package it types, so TypeScript's `node_modules/@types` walk
pairs them. One npm name can resolve more than once inside a single closure, and
pnpm records each resolution separately. The tree is flat where flat is
unambiguous and keyed by resolution where it is not:

```
node_modules/
  minimatch/                                     ← the primary resolution, as files
  .pnpm/minimatch@9.0.9/node_modules/minimatch/  ← any other one, files once
  glob/node_modules/minimatch                    ← relative link, → the store above
```

A resolution is name, version and peer set. pnpm resolves a package once per
distinct set of peers and records each outcome: `fdir@6.5.0(picomatch@4.0.3)`
beside `fdir@6.5.0(picomatch@4.0.7)`. They share a tarball and have different
dependency edges. Two of those in one closure get two store entries,
distinguished by a peer component after the version:

```
  .pnpm/fdir@6.5.0_picomatch_4_0_3_<digest>/node_modules/fdir/
```

- **Primary** is the resolution the tree's own `deps` declare. Where they
  declare none it is the highest version present, the same rule `@npm//:<name>`
  follows, and among peer variants of that version the one pnpm left
  un-suffixed, or the lowest-sorting peer set if every variant carries one. It
  keeps the top-level directory Node's walk-up finds, and tsgo's: a target that
  declares `zod` type-checks against the `zod` it declared, whatever version a
  dependency's closure carries.
- **Every other resolution** gets its bytes exactly once under
  `.pnpm/<name>@<version>[_<peer set>]/node_modules/<name>`, using pnpm's own
  encoding for a scoped name (`.pnpm/@scope+name@1.2.3/node_modules/@scope/name`).
  The peer component is a readable prefix plus a digest of the whole peer set.
- **Links** are emitted only for an edge that disagrees with the primary, at
  `<dependent>/node_modules/<name>`, pointing at the store copy with a relative
  target. Cost scales with the disagreeing edges. Links chain: a store copy's
  own disagreeing dep gets a link inside it.

The links are relative and internal to the tree, so they survive everywhere the
tree goes: as an input to another action, in a test's runfiles, and under
`bazel run`.

## Two Resolutions of One Name in `deps`

Declaring two resolutions of a name directly on one `node_modules` target is an
error:

```
node_modules: @@//src/app:node_modules depends on two versions of 'minimatch' at once:
  minimatch@10.2.4
  minimatch@9.0.9
node_modules/minimatch is one directory and Node resolves the name to it, so a
tree cannot present both as the answer to `import "minimatch"`.
Did you mean to depend on one of them here and let the other arrive through the
package that needs it? A version reached transitively keeps its own version.
Otherwise split the two into separate node_modules targets.
```

Two peer resolutions of one version is the same error, one level narrower:

```
node_modules: @@//src/app:node_modules depends on two resolutions of
'fdir@6.5.0' at once, one per peer set:
  peers picomatch_4_0_3_<digest>
  peers picomatch_4_0_7_<digest>
The tarball is the same either way; what differs is what the package's own
dependencies resolve to, and node_modules/fdir/node_modules can hold one
answer.
Did you mean to depend on one of them here and let the other arrive through the
package that needs it? A resolution reached transitively keeps its own peers.
Otherwise split the two into separate node_modules targets.
```

The transitive case both messages point at is the one
[The Layout](#the-layout) handles.

## The Store

Every lockfile has a virtual store, pnpm's `node_modules/.pnpm`, declared in
the lockfile's own package by `npm_virtual_store()`, a macro the lockfile's hub
writes into `@<hub>//:defs.bzl` from the graph the `npm` extension computes:

```python
load("@npm//:defs.bzl", "npm_virtual_store")

npm_virtual_store()
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
  another's tarball. An edge the extension dropped to break a cycle has no
  link; the import resolves through the hidden hoist below.
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
  resolution hoisted for a name is the one pnpm's hoist step picks: the walk
  starts at every importer's direct dependencies, marks each level's children
  in order before it descends into the first child, and visits the snapshots
  it reached by depth and then by snapshot id; the first snapshot whose
  dependencies name a not-yet-taken name claims it, workspace members and the
  importers' own direct dependencies first (the root importer's are never
  hoisted: they sit at the root already), a snapshot skipped on the target
  platform claims nothing, and a name is taken case-insensitively. A pattern
  is pnpm's: `*` matches any run, `!` negates, the last matching pattern
  decides. Where platforms disagree the link is under a `select()`.
  `tests/npm/hoisted_dependencies.bzl` is pnpm's own answer over the fixture
  lockfile, and `tests/npm:store_tests_hoist` asserts the store's.

A tree holds no symlink, so Bazel hashes it as files and restores it as files
from a disk or remote cache in every download mode, and a fresh output base
over a populated cache executes no `NpmStore`; the links are internal actions,
re-run per output base. The store sits in the lockfile's repository because a
relative link has one text in the execroot and the runfiles tree only while
link and target share a repository; so every lockfile is in a package of its
own -- the `npm` extension refuses two in one -- and a module's hub the
extension fills in from another module's lockfile serves that module's own
targets, never a consumer's.

Every `ts_npm_package` carries its snapshot's store as `NpmPackageInfo.store`
(`NpmStoreInfo`: `key`, `tree`, `links`, `transitive`, `manifest`), and a
member's hub view carries the member's. Nothing else reads the store yet: the
forest below is what `ts_compile` and `ts_test` stage.

## Trees `ts_compile` and `ts_test` Generate

`ts_compile` builds `<name>/node_modules` from its npm deps, their closures, the
paired `@types/*` packages and every first-party dep's npm closure, and tsgo
walks it from a program root that mirrors the exec root; see
[the node_modules forest](ts-compile.md#the-node_modules-forest). A `ts_test`
builds the same forest and runs its tests in it: the tree the compile was
checked against is the runtime tree, with nothing to declare. See
[ts_test](ts-test.md).

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
