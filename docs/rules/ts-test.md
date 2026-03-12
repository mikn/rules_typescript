# ts_test

Compiles TypeScript test files and runs them with vitest inside the Bazel sandbox.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_test")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest"],
)

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    node_modules = ":node_modules",
)
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`/`.tsx` test files |
| `deps` | `label_list` | `[]` | `ts_compile` or npm targets the tests import |
| `node_modules` | `label` | `None` | A `node_modules` target for runtime npm resolution |
| `vitest` | `label` | `None` | Explicit vitest binary label (auto-detected from `node_modules` when absent) |
| `runtime` | `label` | `None` | Per-target JS runtime binary override |
| `env` | `string_dict` | `{}` | Additional environment variables |
| `size` | `string` | `"medium"` | Bazel test size |
| `update_snapshots` | `bool` | `False` | When True, produces an executable that runs `vitest run --update` and writes snapshot files back to the source tree |

## Running Tests

```bash
bazel test //path/to:math_test
```

## Snapshot Updating

When `update_snapshots = True`, `bazel run` (not `bazel test`) writes updated snapshots to the source tree:

```bash
bazel run //path/to:update_snapshots
```

## Debugging

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
    tags = ["manual"],
    env = {
        "NODE_OPTIONS": "--inspect-brk=9229",
    },
)
```

```bash
bazel run //path/to:my_test_debug
```

Then attach with VS Code or `chrome://inspect`.

See [Testing with vitest](../guides/testing.md) for the full guide including sharding, watch mode, and build feedback.
