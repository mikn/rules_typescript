# ts_lint

Runs oxlint or eslint over a set of sources as a Bazel **validation action**.

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_lint")

ts_compile(
    name = "my_lib",
    srcs = ["index.ts"],
    visibility = ["//visibility:public"],
)

ts_lint(
    name = "my_lib_lint",
    srcs = ["index.ts"],
    config = "//:oxlint.json",
    linter = "oxlint",
    linter_binary = "@npm//:oxlint_bin",
)
```

Gazelle writes one `<name>_lint` beside each `ts_compile` when a linter config
file sits in the package or any ancestor. See
[Automatic lint targets](../gazelle/overview.md#automatic-lint-targets) for the
detection order.

## Validation Action

The stamp the rule produces is in the `_validation` output group, which puts
lint in the same position as tsgo's type check:

- it runs during `bazel build`, without being asked for;
- it does not block downstream compilation. A lint error in a library does not
  stop a binary that depends on it from being built;
- it is cached like any action. Unchanged sources and an unchanged config mean
  no lint run at all.

To run only the lint checks:

```bash
bazel build //... --output_groups=+_validation
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | Files to lint: `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`. An empty list is an analysis-time error naming the attribute |
| `linter` | `string` | `"oxlint"` | `"oxlint"` or `"eslint"`. Selects the **flag spelling** the rule passes; the binary comes from `linter_binary` |
| `linter_binary` | `label` | required | The executable: `@npm//:oxlint_bin` or `@npm//:eslint_bin` from your npm hub, or a `filegroup` wrapping a binary. Built for the exec configuration |
| `config` | `label` | `None` | The linter's config file (`oxlint.json`, `.oxlintrc.json`, `eslint.config.mjs`, …). Passed as `--config`, and omitted entirely when unset |
| `fail_on_warnings` | `bool` | `False` | Make a warning an error: `--deny-warnings` for oxlint, `--max-warnings=0` for eslint |

oxlint needs no config for basic use; eslint needs a flat config.

`linter_binary` is mandatory: there is no linter toolchain, and which oxlint or
eslint runs is a property of your lockfile.

## Provider

`TsLintInfo` carries one field, `stamp` — the file written only on a clean run.
A rule wanting to depend on "this target was linted" reads it.

## Paths

Every source and the config reach the linter as **execroot-absolute** paths,
substituted into the command line by the `tsaction` helper. An `npm_bin` wrapper
`cd`s to `RUNFILES_DIR` before running the package's own binary, which
invalidates every execroot-relative path it was given. A linter reporting
`ENOENT` on a file that plainly exists is this going wrong.

## Action Environment

The action runs with `PATH=/bin:/usr/bin` and nothing else from the host
environment. Node comes from the wrapper's own runfiles, staged because the
linter's `FilesToRunProvider` is passed in `tools` and not `inputs`. A linter
binary whose native sidecar or Node cannot be found is usually a non-executable
target.
