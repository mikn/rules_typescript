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
