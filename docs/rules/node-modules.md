# node_modules

Creates a hermetic `node_modules` directory in the Bazel sandbox holding exactly
the packages named and their transitive dependencies.

`ts_test` builds its own from `deps` (see [ts_test](ts-test.md)). A
`ts_compile` target needs none: it reaches npm declarations through depsets. A
hand-written one covers a program or tool that needs packages on disk at
runtime, and a `ts_test` whose tree is not the one its `deps` describe.

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

One npm name can resolve more than once inside a single closure, and pnpm
records each resolution separately. The tree is flat where flat is unambiguous
and keyed by resolution where it is not:

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
  keeps the top-level directory Node's walk-up finds.
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

## Trees `ts_test` Generates

`ts_test` generates a per-target tree from its `deps` through this same builder,
so a test gets the same layout with nothing to declare. See
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
([ts_lint § Paths](ts-lint.md#paths)).
