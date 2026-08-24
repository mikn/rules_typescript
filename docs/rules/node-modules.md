# node_modules

Creates a hermetic `node_modules` directory in the Bazel sandbox containing exactly the specified packages and their transitive dependencies.

## Usage

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest", "@npm//:react", "@npm//:react-dom"],
)
```

Reference in `ts_test`:

```python
ts_test(
    name = "my_test",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
)
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `deps` | `label_list` | `[]` | npm package targets from `@npm` to include in `node_modules` |

## When to Use

`node_modules` is needed for targets that require `node_modules/` at runtime — vitest requires it to load ESM packages, for example. Pure compilation targets (`ts_compile`) do not need it; they reference npm packages via depsets.

Use one `node_modules` target per test suite or dev server target. Share it across multiple `ts_test` targets in the same package to avoid duplicating the directory tree in the sandbox.

## The layout

One npm name can resolve to more than one version inside a single closure —
pnpm records each resolution separately, and every one of them is real. So the
tree is flat where flat is unambiguous and version-keyed where it is not:

```
node_modules/
  minimatch/                                     ← the primary version, as files
  .pnpm/minimatch@9.0.9/node_modules/minimatch/  ← any other version, files once
  glob/node_modules/minimatch                    ← relative link, → the store above
```

- **Primary** is the version the tree's own `deps` declare, and the highest
  version present when they declare none — the same rule `@npm//:<name>`
  follows. It keeps the top-level directory Node's walk-up finds, so everything
  that resolved before still resolves.
- **Every other version** gets its bytes exactly once under
  `.pnpm/<name>@<version>/node_modules/<name>`, using pnpm's own encoding for a
  scoped name (`.pnpm/@scope+name@1.2.3/node_modules/@scope/name`).
- **A link is emitted only for an edge that disagrees with the primary**, at
  `<dependent>/node_modules/<name>`, pointing at the store copy with a relative
  target. Cost scales with disagreeing edges, not with all of them. Links chain:
  a store copy's own disagreeing dep gets a link inside it.

The links are relative and internal to the tree, which is what makes them
survive everywhere the tree goes — as an input to another action, in a test's
runfiles, and under `bazel run`.

Before this layout, both versions wrote to `node_modules/<name>`, the last copy
won, and every dependent silently got that version. Nothing downstream could
tell, because the version it got was a real version.

## Two versions of one name in `deps`

Declaring both versions of a name directly on one `node_modules` target is an
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

There is no arrangement that satisfies it: the two answers to `import "<name>"`
from the tree root are one directory. A version that arrives *transitively*
keeps its own version, which is the case the layout above handles.

## `ts_test` builds one for you

`ts_test` generates a per-target tree from its `deps` through this same builder,
so a test gets the same layout with nothing to declare. See
[ts_test](ts-test.md).
