# Testing with vitest

`ts_test` compiles TypeScript test files and runs them with vitest inside the
Bazel sandbox. The full attribute table is in the
[ts_test reference](../rules/ts-test.md); this page is the guide.

## Setup

```python
# BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test")

ts_compile(
    name = "math",
    srcs = ["math.ts"],
    visibility = ["//visibility:private"],
)

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
)
```

```bash
bazel test //path/to:math_test
```

No `node_modules` target is needed. `ts_test` builds a per-target
`node_modules` tree from every dep that provides `NpmPackageInfo`, plus their
transitive npm deps. List every npm package the run needs — imported by the
tests *and* by the production code under test — in `deps`; a `ts_compile` dep
does not contribute its own npm packages to the tree. Gazelle does this for you.

Two consequences worth knowing. The test's own sources are checked for
undeclared imports like any other `ts_compile` sources, so an import that only
some dep's own deps provide fails the build with the label to add
([why](../rules/ts-compile.md#deps-have-to-be-direct)). And the tree places
every *resolution* the closure made rather than one directory per name, so a test
whose dependencies disagree about a package — its version, or the peers it was
resolved against — gets what each of them resolved
([layout](../rules/node-modules.md#the-layout)).

Pass `node_modules` explicitly only when `deps` is a `select()` (a macro cannot
iterate one) or when the tree you need is not the one the deps describe:

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vitest", "@npm//:happy-dom"],
)

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    node_modules = ":node_modules",
)
```

## Controlling the test environment

A vitest config is always generated and always passed with `--config`, so vitest
never picks up a stray config from the runfiles tree. Everything you set
composes with it rather than replacing it — see
[the generated vitest config](../rules/ts-test.md#the-generated-vitest-config)
for the precedence rules.

### DOM tests and polyfills

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    deps = [
        ":button",
        "@npm//:react",
        "@npm//:happy-dom",
        "@npm//:testing-library_react",
        "@npm//:vitest",
    ],
    environment = "happy-dom",
    setup_files = ["setupTests.ts"],
)
```

`environment` is any value vitest accepts — `node`, `jsdom`, `happy-dom`,
`edge-runtime`, or a custom environment package — and the matching package has
to be in `deps`. (Scoped npm names take their label form: `@testing-library/react`
is `@npm//:testing-library_react`.) `setup_files` entries run before every test
file, which is where `matchMedia`, `ResizeObserver`, `PointerEvent` and friends
belong. TypeScript entries are compiled with the same `deps` as the tests.

A DOM environment needs no sandbox flags: the generated config sets
`resolve.preserveSymlinks`, without which vitest's web transform resolves every
runfiles symlink to its target and walks out of the sandbox
(`Failed to load url … Does the file exist?`).

`global_setup` is the same mechanism for `test.globalSetup`, which runs once
around the whole run rather than per file.

### An existing vitest config

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    deps = [":button", "@npm//:react", "@npm//:vitest"],
    config = "vitest.config.ts",
    data = ["test/fixtures.json", "test/msw-handlers.ts"],
)
```

Anything the config imports relatively belongs in `data`; it is not a build
input otherwise. A config that default-exports an array is read as a list of
vitest projects, and each project in it gets the Bazel and attribute layers too
— that array becomes `test.projects`, which needs vitest 3.2 or later
([detail](../rules/ts-test.md#a-config-file)). Every other `config` shape runs
on any vitest 3 or 4.

Small adjustments do not need a file at all — `config` also takes a dict:

```python
config = {"test": {"testTimeout": 30000, "retry": 2}},
```

### Other knobs

```python
ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    globals = True,                          # global describe/it/expect
    reporters = ["default", "junit"],
    coverage_thresholds = {"lines": "80"},
)
```

### Seeing what ran

```bash
bazel build //path/to:math_test --output_groups=vitest_config
```

That writes out the merged config the runner passed to vitest — the fastest way
to tell whether your layer landed where you expected.

## CSS modules

A dep providing `CssModuleInfo` adds a mock plugin to the Bazel layer:
`*.module.css` imports resolve to a `Proxy` whose every property lookup returns
the property name, so class names are deterministic without a CSS parse at test
time.

```ts
import styles from "./Button.module.css";
expect(styles.primary).toBe("primary");
```

This used to be shadowed by vite's own CSS plugins, which produced hashed class
names instead. It now applies, so assertions written against the hashed form
need updating.

## Coverage

```bash
bazel coverage //path/to:math_test
```

Works on every `ts_test` with nothing to opt into, provided
`@vitest/coverage-v8` is in the `node_modules` tree. `coverage = True`
additionally instruments plain `bazel test` runs.

`coverage_thresholds` reaches `test.coverage.thresholds` in the generated
config, and only applies when coverage runs. Its enforcement has no test behind
it, so do not treat a green build as proof a threshold held.

`coverage_provider` picks between `"v8"` (vitest's default) and `"istanbul"`.
A test whose pool runs in a second runtime needs `"istanbul"`: v8 coverage comes
out of Node's inspector, which workerd has none of, while istanbul instruments
at transform time. See
[ts_test § Coverage](../rules/ts-test.md#coverage).

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`. Set `shard_count` on the target and pass
`--test_sharding_strategy=explicit`.

## Snapshots

`toMatchSnapshot()` works, and the `.snap` files stay where a plain `vitest` run
keeps them — `<package>/__snapshots__/<source>.snap`, beside your `.ts` rather
than in `bazel-out`. Adopting `ts_test` renames nothing.

Two halves, and both are needed. Reading: list the files in `snapshots`, which is
what puts them inside the sandbox.

```python
ts_test(
    name = "widget_test",
    srcs = ["widget.test.ts"],
    snapshots = glob(["__snapshots__/*.snap"]),
    deps = [":widget", "@npm//:vitest"],
)
```

Writing: run the updater that every `ts_test` declares next to itself.

```bash
bazel run //path/to:widget_test.update_snapshots
```

It writes into your checkout. Commit the result. `--sandbox_writable_path` is not
part of this any more, and neither is a second hand-written target.

The `snapshots` attr is not cosmetic. `ts_test` runs vitest in read-only snapshot
mode, so a snapshot the test cannot read is a failure — where an unlisted one
used to look like a *new* snapshot, get written into the sandbox, and let the test
pass on what it had just written.

## Watch mode

Use [ibazel](https://github.com/bazelbuild/bazel-watcher) to re-run tests on
every change:

```bash
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest

ibazel test //path/to:my_test
ibazel test //...
```

ibazel watches the build graph, so only affected targets are rebuilt and
re-tested.

To see what the test launcher resolved rather than what it ran:

```bash
bazel run //path/to:my_test -- --dump-config
```

## Build feedback

```bash
bazel test //... --show_result=10   # default is 1
bazel test //... --show_result=0    # all targets
```

Add `test --show_result=20` to `.bazelrc` to make it permanent.
