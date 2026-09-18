# Lint

The linter is configured once, in the root `MODULE.bazel`, and every
`ts_compile` and `ts_test` runs it over its own sources as a validation
action -- the shape of `nogo` in rules_go.

```python
ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
ts.lint(
    binary = "@npm//:oxlint_bin",
    config = "//:oxlint.json",
)
```

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `binary` | `label` | required | The linter's executable from the workspace's hub: `@npm//:oxlint_bin` or `@npm//:eslint_bin`. The lockfile decides which oxlint or eslint runs; there is no linter toolchain |
| `config` | `label` | `None` | The linter's own config file (`oxlint.json`, `.oxlintrc.json`, `eslint.config.mjs`, ...), passed as `--config`. Unset, no flag is passed: oxlint runs its defaults, eslint needs a flat config |
| `fail_on_warnings` | `bool` | `False` | A warning fails the build: `--max-warnings=0`, which oxlint and eslint spell alike |

Only the root module's call takes effect, and it makes one; a module graph
with no call lints nothing. The config file is read from another repository,
so its package exports it: `exports_files(["oxlint.json"])`. One build without
the linter: `--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint`, a
`lint_config` naming no binary.

## What Runs

The call writes the `@lint_config` repository, whose one target the label flag
`@rules_typescript//ts:lint` names. Every `ts_compile` and `ts_test` reads the
flag and, when it names a binary, runs one `TsLint` action over the target's
TypeScript, JavaScript and declaration srcs, never a data src, and writes
`<name>.tslint` when the linter exits 0. The stamp is in the
`_validation` output group, the position of tsgo's own check:

- it runs during `bazel build`, without being asked for;
- it does not block downstream compilation: a lint error in a library does not
  stop a binary that depends on it from being built;
- it is cached like any action: unchanged sources and an unchanged config mean
  no lint run at all.

To run the checks alone, or where a `.bazelrc` turned validations off:

```bash
bazel build //... --output_groups=+_validation
```

## Paths

Every source and the config reach the linter as **execroot-absolute** paths,
substituted into the command line by `tsaction`, the tools toolchain's runner. An `npm_bin` wrapper
`cd`s to `RUNFILES_DIR` before running the package's own binary, which
invalidates every execroot-relative path it was given. A linter reporting
`ENOENT` on a file that exists is this.

## Action Environment

The action runs with `PATH=/bin:/usr/bin` and nothing else from the host
environment. Node comes from the wrapper's own runfiles, staged because the
linter's `FilesToRunProvider` is passed in `tools` and not `inputs`. A linter
binary whose native sidecar or Node cannot be found is usually a non-executable
target.
