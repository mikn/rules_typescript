# node_modules

Creates a hermetic `node_modules` directory in the Bazel sandbox holding exactly
the packages named and their transitive dependencies.

Most workspaces declare few of these by hand. `ts_test` builds its own from
`deps` ([why and when to override](ts-test.md)), and Gazelle writes the
`node_modules` a generated `ts_dev_server` or `vite_bundler` needs. A
`ts_compile` target needs none at all — it reaches npm declarations through
depsets, not a directory.

What is left is the case Gazelle cannot infer: a program or tool that needs
packages on disk at runtime, and a `ts_test` whose tree is not the one its `deps`
describe.

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

One tree can serve several targets in the same package, which keeps one copy in
the sandbox instead of one per target.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `deps` | `label_list` | `[]` | npm package targets from `@npm` to include in `node_modules` |

## The layout

One npm name can resolve more than once inside a single closure — pnpm records
each resolution separately, and every one of them is real. So the tree is flat
where flat is unambiguous and keyed by resolution where it is not:

```
node_modules/
  minimatch/                                     ← the primary resolution, as files
  .pnpm/minimatch@9.0.9/node_modules/minimatch/  ← any other one, files once
  glob/node_modules/minimatch                    ← relative link, → the store above
```

A resolution is name, version **and peer set**. pnpm resolves a package once per
distinct set of peers and keys the outcomes apart —
`ansi-styles@6.2.3(ansi-regex@5.0.1)` beside
`ansi-styles@6.2.3(ansi-regex@6.2.2)` — because they share a tarball and have
different dependency edges. Two of those in one closure get two store entries,
distinguished by a peer component after the version:

```
  .pnpm/ansi-styles@6.2.3_ansi-regex_5_0_1_5e7443ea/node_modules/ansi-styles/
```

- **Primary** is the resolution the tree's own `deps` declare; where they declare
  none it is the highest version present — the same rule `@npm//:<name>` follows
  — and, among peer variants of that version, the one pnpm left un-suffixed. It
  keeps the top-level directory Node's walk-up finds, so everything that resolved
  before still resolves.
- **Every other resolution** gets its bytes exactly once under
  `.pnpm/<name>@<version>[_<peer set>]/node_modules/<name>`, using pnpm's own
  encoding for a scoped name (`.pnpm/@scope+name@1.2.3/node_modules/@scope/name`).
  The peer component is a readable prefix plus a digest of the whole peer set,
  because a nested peer set can run to hundreds of characters and truncating
  alone would collide two resolutions into one directory.
- **A link is emitted only for an edge that disagrees with the primary**, at
  `<dependent>/node_modules/<name>`, pointing at the store copy with a relative
  target. Cost scales with disagreeing edges, not with all of them. Links chain:
  a store copy's own disagreeing dep gets a link inside it.

The links are relative and internal to the tree, which is what makes them
survive everywhere the tree goes — as an input to another action, in a test's
runfiles, and under `bazel run`.

Before this layout, every resolution of a name wrote to `node_modules/<name>`,
the last copy won, and every dependent silently got that one. Nothing downstream
could tell, because the version it got was a real version.

## Two resolutions of one name in `deps`

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
'ansi-styles@6.2.3' at once, one per peer set:
  peers ansi-regex_5_0_1_5e7443ea
  peers ansi-regex_6_2_2_602a0566
The tarball is the same either way; what differs is what the package's own
dependencies resolve to, and node_modules/ansi-styles/node_modules can hold one
answer.
```

There is no arrangement that satisfies either: the answer to `import "<name>"`
from the tree root is one directory, and that directory's own `node_modules` is
one directory too. A resolution that arrives *transitively* keeps its own version
and its own peers, which is the case the layout above handles.

## `ts_test` builds one for you

`ts_test` generates a per-target tree from its `deps` through this same builder,
so a test gets the same layout with nothing to declare. See
[ts_test](ts-test.md).
