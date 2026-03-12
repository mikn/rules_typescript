# Testing with vitest

`ts_test` compiles TypeScript test files and runs them with vitest inside the Bazel sandbox.

## Setup

```python
# BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test")
load("@rules_typescript//npm:defs.bzl", "node_modules")

ts_compile(
    name = "math",
    srcs = ["math.ts"],
    visibility = ["//visibility:private"],
)

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

```bash
bazel test //path/to:math_test
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

## Sharding

`ts_test` supports Bazel test sharding. Pass `--test_sharding_strategy=explicit` and set `shard_count` on the target to distribute test files across parallel runners.

## Snapshot Testing

Vitest snapshot files (`.snap`) must live in the source tree, but the Bazel sandbox is read-only. Two supported workflows:

**Recommended: `update_snapshots = True`**

```python
ts_test(
    name = "my_test",
    srcs = ["Button.test.tsx"],
    deps = [":button"],
    node_modules = ":node_modules",
)

ts_test(
    name = "update_snapshots",
    srcs = ["Button.test.tsx"],
    deps = [":button"],
    node_modules = ":node_modules",
    update_snapshots = True,  # produces an executable, not a test
)
```

To create or update snapshots:

```bash
bazel run //path/to:update_snapshots
```

Vitest runs with `--update` from the workspace root, so snapshot files are written to the correct `__snapshots__` directory alongside your source files. Commit the resulting `.snap` files to version control.

**Alternative: `--sandbox_writable_path`**

```bash
bazel test //path/to:my_test \
  --sandbox_writable_path=$(pwd)/src/components/__snapshots__
```

## Watch Mode

Use [ibazel](https://github.com/bazelbuild/bazel-watcher) to watch test files and re-run them on every change:

```bash
# Install ibazel
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest

# Watch a single test target
ibazel test //path/to:my_test

# Watch all tests in the repo
ibazel test //...
```

ibazel monitors the build graph and re-runs tests whenever a source file or dependency changes. Only the affected targets are rebuilt and re-tested.

## Build Feedback

To see which targets were (re)built or cached:

```bash
# Show results for up to 10 targets (default is 1)
bazel test //... --show_result=10

# Show results for all targets (unlimited)
bazel test //... --show_result=0
```

Add `test --show_result=20` to `.bazelrc` for a persistent setting.
