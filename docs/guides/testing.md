# Testing with vitest

`ts_test` compiles TypeScript test files and runs them inside the Bazel
sandbox, under vitest by default. The full attribute table is in the
[ts_test reference](../rules/ts-test.md).

Tests written against node's own runner take `runner = "node:test"`; the rest of
this page is the vitest runner. See
[The node:test runner](../rules/ts-test.md#the-nodetest-runner).

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
`node_modules` tree from every dep that provides `NpmPackageInfo`, their
transitive npm deps, and the npm closure of every `ts_compile` dep, so the
production code under test runs against the packages it declared. `deps` lists
what the tests import and the npm imports of the package's production sources;
`bazel run //:gazelle` writes that list.

Test sources are checked for undeclared imports like any other `ts_compile`
sources, so an import that only some dep's own deps provide fails the build with
the label to add. See
[Deps have to be direct](../rules/ts-compile.md#deps-have-to-be-direct).

The tree places every resolution the closure made, keyed apart wherever one name
resolved more than once, so deps that disagree about a package version or peer
set each get what they resolved. See
[the layout](../rules/node-modules.md#the-layout).

Pass `node_modules` explicitly when `deps` is a `select()` (a macro cannot
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

## Controlling the Test Environment

A vitest config is always generated and always passed with `--config`, so vitest
never picks up a stray config from the runfiles tree. Everything you set merges
into it; see
[the generated vitest config](../rules/ts-test.md#the-generated-vitest-config)
for the precedence rules.

### DOM Tests and Polyfills

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

`environment` takes any value vitest accepts (`node`, `jsdom`, `happy-dom`,
`edge-runtime`, or a custom environment package) and the matching package has to
be in `deps`. Scoped npm names take their label form: `@testing-library/react`
is `@npm//:testing-library_react`. `setup_files` entries run before every test
file, which is where `matchMedia`, `ResizeObserver` and `PointerEvent` belong;
TypeScript entries are compiled with the same `deps` as the tests.
`global_setup` is the same mechanism for `test.globalSetup`, which runs once
around the whole run.

A DOM environment needs no sandbox flags. The generated config sets
`resolve.preserveSymlinks`, without which vitest's web transform resolves every
runfiles symlink to its target and walks out of the sandbox
(`Failed to load url … Does the file exist?`).

### An Existing vitest Config

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
vitest projects, and each project in it gets the Bazel and attribute layers too.
That array becomes `test.projects`, which needs vitest 3.2 or later; see
[A config file](../rules/ts-test.md#a-config-file). Every other `config` shape
runs on any vitest 3 or 4.

`config` also takes a dict:

```python
config = {"test": {"testTimeout": 30000, "retry": 2}},
```

Gazelle writes `config` from the file plain `vitest` would read: a
`vitest.config.*` beside the tests by name, else the one in the nearest
directory above holding a `package.json`, or the repository root, as the label
`//pkg:vitest_config` of a public `filegroup` Gazelle writes over the file in
that package. Vite's root is the test's package either way, so a relative path
in such a config resolves against the test's directory, not the config's. See
[the package boundary heuristic](../gazelle/overview.md#package-boundary-heuristic).

### Other Attributes

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

### The Merged Config

```bash
bazel build //path/to:math_test --output_groups=vitest_config
```

That writes out the merged config the runner passed to vitest.

## CSS Modules

A `*.module.css` anywhere in the dep closure adds a plugin to the Bazel layer
that answers the import with the export map `css_module` wrote beside the
stylesheet, so a test sees the class name a bundler emits:

```ts
import styles from "./Button.module.css";
expect(styles.primary).toMatch(/^_primary_[0-9a-f]{8}$/);
```

An assertion on a rendered `class` attribute reads the same map:

```ts
render(host);
expect(host.querySelector("button")?.getAttribute("class")).toBe(styles.button);
```

A `*.module.css` with no `css_module` target behind it has no map and no
`.d.ts`; the import falls back to a proxy returning the property name, so it
loads and the test runs.

## Coverage

```bash
bazel coverage //path/to:math_test
```

Works on every vitest `ts_test` when `@vitest/coverage-v8` is in the
`node_modules` tree. `coverage = True` additionally instruments plain
`bazel test` runs. A `runner = "node:test"` target reports no coverage, and
`bazel coverage` on one fails saying so.

`coverage_thresholds` reaches `test.coverage.thresholds` in the generated
config and applies only when coverage runs. A run that misses a threshold fails,
after the assertions themselves have passed:

```
ERROR: Coverage for lines (50%) does not meet global threshold (90%)
```

Which files are reported is `--instrumentation_filter`'s answer; Bazel derives a
default from the targets on the command line, so a library in another package is
absent until a wider filter names it. `coverage_provider` picks between `"v8"`
(vitest's default) and `"istanbul"`, and a test whose pool runs in a second
runtime needs `"istanbul"`. See
[ts_test § Coverage](../rules/ts-test.md#coverage) for both.

## Cloudflare Workers

A Worker's tests can run inside workerd, so `SELF.fetch()` dispatches to the
`fetch` handler in the runtime. `@cloudflare/vitest-pool-workers` supplies the
pool; `//tests/workers` is the worked example:

```python
ts_compile(
    name = "worker",
    srcs = ["src/index.ts"],
    lib = [
        "esnext",
        "webworker",
    ],
)

ts_test(
    name = "worker_test",
    size = "medium",
    srcs = ["src/worker.test.ts"],
    config = "vitest.workers.config.mjs",
    coverage_provider = "istanbul",
    data = ["wrangler.jsonc"],
    lib = [
        "esnext",
        "webworker",
    ],
    types = ["@cloudflare/vitest-pool-workers/types"],
    deps = [
        ":worker",
        "@npm_workers//:cloudflare_vitest-pool-workers",
        "@npm_workers//:vitest",
        "@npm_workers//:vitest_coverage-istanbul",
    ],
)
```

```typescript
import { SELF } from 'cloudflare:test';
import { describe, expect, it } from 'vitest';

describe('worker', () => {
  it('answers /health', async () => {
    const res = await SELF.fetch('https://example.com/health');
    expect(res.status).toBe(200);
  });
});
```

`lib` names `webworker` on the worker target and on the test target: the
`Request`/`Response` globals a Worker is written against are in no set `target`
implies.

### The vitest Config

```javascript
import { cloudflareTest } from '@cloudflare/vitest-pool-workers';

export default {
  resolve: { preserveSymlinks: false },
  plugins: [
    cloudflareTest({
      remoteBindings: false,
      wrangler: { configPath: 'wrangler.jsonc' },
    }),
  ],
};
```

`cloudflareTest()` belongs in `plugins`, not in `test.pool`. Two of the
package's exports are candidates. `cloudflarePool()` is a pool initializer that
boots workerd and nothing else. `cloudflareTest()` is a Vite plugin that
installs that pool and owns the `cloudflare:test` specifier: `resolveId` maps it
to a virtual id, `load` returns the runtime's bytes. The pool forwards
`cloudflare:test` to Vite and externalises every other `cloudflare:*` specifier
to workerd, so with no plugin registered nothing resolves it and vitest falls
back to Node package resolution, which fails.

`resolve.preserveSymlinks: false` is the other required line. `ts_test`'s Bazel
layer turns it on for the sandbox reason above. The pool resolves modules for
workerd through a second path, where a lexical path is a second module identity
for the same file, and the user layer wins. Without it the run fails with
`TypeError: Cannot read properties of undefined (reading 'config')` from inside
the pool runner.

The wrangler `configPath` is relative because `ts_test` roots Vite at the
package, which is where `data = ["wrangler.jsonc"]` stages the file and where the
compiled worker its `main` names is staged too.

### `coverage_provider` and `types`

`coverage_provider = "istanbul"`. v8 coverage is counters read back out of
Node's inspector, and workerd has none; istanbul instruments before the code
crosses into the runtime, so `bazel coverage` reports per-line data for code
running inside workerd.

`types = ["@cloudflare/vitest-pool-workers/types"]` is an `exports` subpath whose
only condition is `types`, where the pool puts the ambient declaration for
`cloudflare:test`. Nothing imports it, and a tsconfig `types` entry cannot reach
it under a ruleset with no `node_modules`, so it is resolved from the package
manifest into the program's `files`.

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`, on either runner. Set `shard_count` on the target and pass
`--noincompatible_check_sharding_support`: the runner never touches
`TEST_SHARD_STATUS_FILE`, which is how Bazel expects a test runner to advertise
sharding support, so without that flag a sharded run fails before any test
starts.

## Snapshots

`toMatchSnapshot()` works, and the `.snap` files stay where a plain `vitest` run
keeps them: `<package>/__snapshots__/<source>.snap`, beside the `.ts` and not in
`bazel-out`.

Reading them takes the `snapshots` attr, which is what puts the files inside the
sandbox.

```python
ts_test(
    name = "widget_test",
    srcs = ["widget.test.ts"],
    snapshots = glob(["__snapshots__/*.snap"]),
    deps = [":widget", "@npm//:vitest"],
)
```

Writing: run the updater that every vitest `ts_test` declares next to itself.

```bash
bazel run //path/to:widget_test.update_snapshots
```

It writes into the checkout. Commit the result. No `--sandbox_writable_path` and
no second hand-written target are involved.

`ts_test` runs vitest in read-only snapshot mode, so a snapshot the test cannot
read is a failure. In vitest's default mode an unlisted snapshot would be
written into the sandbox as new, and the test would pass on what it had just
written.

## Watch Mode

Use [ibazel](https://github.com/bazelbuild/bazel-watcher) to re-run tests on
every change:

```bash
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest

ibazel test //path/to:my_test
ibazel test //...
```

ibazel watches the build graph, so only affected targets are rebuilt and
re-tested.

To see what the launcher resolved (node binary, vitest entry, `node_modules`
tree, shard split):

```bash
bazel run //path/to:my_test -- --dump-config
```

## Build Feedback

```bash
bazel test //... --show_result=10   # default is 1
bazel test //... --show_result=0    # all targets
```

Add `test --show_result=20` to `.bazelrc` to make it permanent.
