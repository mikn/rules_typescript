# Testing with vitest

`ts_test` compiles TypeScript test files and runs them inside the Bazel
sandbox, under vitest by default. The full attribute table is in the
[ts_test reference](../rules/ts-test.md).

Tests written against node's own runner take
`runner = "@rules_typescript//ts/runners:node_test"`; the rest of this page is
the vitest runner. See
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

The `node_modules` tree the tests run in is the forest tsgo checked them
against: every dep that provides `NpmPackageInfo`, their transitive npm deps,
and the npm closure of every `ts_compile` dep, so the production code under
test runs against the packages it declared. `deps` lists
what the tests import, the npm imports of the package's production sources,
the vitest config's imports and the nearest `package.json`'s dependencies;
`bazel run //:gazelle` writes that list from tsgo's listing of the package.

Test sources are checked for undeclared imports like any other `ts_compile`
sources, so an import that only some dep's own deps provide fails the build with
the label to add. See
[Deps have to be direct](../rules/ts-compile.md#deps-have-to-be-direct).

The tree places every resolution the closure made, keyed apart wherever one name
resolved more than once, so deps that disagree about a package version or peer
set each get what they resolved. See
[the layout](../rules/node-modules.md#the-layout).

## Controlling the Test Environment

A vitest config is always generated and always passed with `--config`, so vitest
never picks up a stray config from the runfiles tree. The `config` file merges
into it, and every vitest setting is the file's; see
[the generated vitest config](../rules/ts-test.md#the-generated-vitest-config)
for the precedence rules.

### DOM Tests and Polyfills

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    config = "vitest.config.ts",
    deps = [
        ":button",
        "@npm//:react",
        "@npm//:happy-dom",
        "@npm//:testing-library_react",
        "@npm//:vitest",
    ],
)
```

```ts
// vitest.config.ts
export default {
  test: {
    environment: "happy-dom",
    setupFiles: ["./setupTests.ts"],
  },
};
```

`test.environment` takes any value vitest accepts (`node`, `jsdom`, `happy-dom`,
`edge-runtime`, or a custom environment package) and the matching package has to
be in `deps`; Gazelle writes it from the config's imports and the nearest
`package.json`. Scoped npm names take their label form: `@testing-library/react`
is `@npm//:testing-library_react`. `test.setupFiles` entries run before every
test file, which is where `matchMedia`, `ResizeObserver` and `PointerEvent`
belong; an entry naming a TypeScript source runs its compiled sibling, so the
`ts_compile` whose `srcs` hold `setupTests.ts` is in `deps` -- under Gazelle the
package's own compile, which is there already. `test.globalSetup` is the same
mechanism for a file that runs once around the whole run. See
[Setup Files](../rules/ts-test.md#setup-files).

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

The modules the config imports relatively are `config_srcs`, staged beside the
config's copy; Gazelle writes them from the config's listing. A config that
default-exports an array is read as a list of vitest projects, and each project
in it gets the Bazel layer too. That array becomes `test.projects`, which needs
vitest 3.2 or later; see
[A config file](../rules/ts-test.md#a-config-file). Every other `config` shape
runs on any vitest 3 or 4.

Gazelle writes `config` from the file plain `vitest` would read: a
`vitest.config.*` beside the tests by name, else the one in the nearest
directory above holding a `package.json`, or the repository root, as the label
`//pkg:vitest_config` of a public `filegroup` Gazelle writes over the file in
that package. Vite's root is the config's package either way, so a relative
path in such a config resolves against the directory it sits in, as it does
under plain `vitest`; `//tests/config_at_root` is the example. Every import of
the config is a dep of the test. See
[what Gazelle writes](../gazelle/overview.md#what-gazelle-writes).

## CSS Modules

A `*.module.css` in a `ts_compile`'s `srcs` is staged beside the compiled `.js`,
and vitest loads it as it does outside Bazel: the stylesheet is replaced by a
proxy whose properties are the class names `css.modules.classNameStrategy`
shapes, `_<name>_<hash>` under the default `stable` and the bare name under
`non-scoped`; Vite's CSS modules run on the file only under a `css` key in the
config. The Bazel layer sets no `css` key. The import is typed by the tsconfig
-- `vite/client` in `types`, or a `declare module "*.module.css"` in `srcs`.

## Coverage

```bash
bazel coverage //path/to:math_test
```

Works on every vitest `ts_test` when `@vitest/coverage-v8` is in the
`node_modules` tree. A target on the node:test runner reports no coverage, and
`bazel coverage` on one fails saying so.

Which files are reported is `--instrumentation_filter`'s answer; Bazel derives a
default from the targets on the command line, so a library in another package is
absent until a wider filter names it. `coverage_provider` picks between `"v8"`
(vitest's default) and `"istanbul"`, and a test whose pool runs in a second
runtime needs `"istanbul"`. See
[ts_test § Coverage](../rules/ts-test.md#coverage) for both.

## Cloudflare Workers

A Worker's tests can run inside workerd, so `SELF.fetch()` dispatches to the
`fetch` handler in the runtime. `@cloudflare/vitest-pool-workers` supplies the
pool; `//tests/workers_nested` is the worked example, in the shape a Worker
repository has: `package.json`, the vitest config, `wrangler.jsonc` and the
worker's tsconfig at the worker root, the tests in `test/`:

```jsonc
// workers/proxy/tsconfig.worker.json
{
  "compilerOptions": {
    "lib": ["esnext", "webworker"]
  }
}
```

```python
# workers/proxy/BUILD.bazel
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

package(default_visibility = ["//visibility:public"])

ts_config(
    name = "worker_tsconfig",
    src = "tsconfig.worker.json",
)

ts_compile(
    name = "worker",
    srcs = ["src/index.ts"],
    tsconfig = ":worker_tsconfig",
)

filegroup(
    name = "vitest_config",
    srcs = ["vitest.config.mjs"],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "wrangler_config",
    srcs = ["wrangler.jsonc"],
    visibility = ["//visibility:public"],
)
```

```python
# workers/proxy/test/BUILD.bazel
ts_test(
    name = "worker_test",
    size = "medium",
    srcs = ["worker.test.ts"],
    config = "//workers/proxy:vitest_config",
    coverage_provider = "istanbul",
    tsconfig = "//workers/proxy:worker_tsconfig",
    wrangler_config = "//workers/proxy:wrangler_config",
    deps = [
        "//workers/proxy:worker",
        "@npm_workers//:cloudflare_vitest-pool-workers",
        "@npm_workers//:vitest",
        "@npm_workers//:vitest_coverage-istanbul",
    ],
)
```

Gazelle writes the two filegroups from the config -- `vitest_config` over the
file, `wrangler_config` over the file its `configPath` names -- and the test's
`config`, `wrangler_config`, `coverage_provider` and `deps` from the config's
imports ([a Workers-pool config](../gazelle/overview.md#a-workers-pool-config)).

```typescript
/// <reference types="@cloudflare/vitest-pool-workers/types" />
import { SELF } from 'cloudflare:test';
import { describe, expect, it } from 'vitest';

describe('worker', () => {
  it('answers /health', async () => {
    const res = await SELF.fetch('https://example.com/health');
    expect(res.status).toBe(200);
  });
});
```

`tsconfig` names the worker's file on the worker target and on the test target:
the `Request`/`Response` globals a Worker is written against are in `webworker`,
which no set `target` implies, and the test files are a program of their own,
checked against that `lib` only when their tsconfig names it too. The
`ts_config` puts the file behind a label the test's package can name; a test in
the worker's own package names the file directly, `tsconfig =
"tsconfig.worker.json"`, as `//tests/workers` does. See
[the test's tsconfig](../rules/ts-test.md#the-tests-tsconfig).

### The vitest Config

```javascript
import { cloudflareTest } from '@cloudflare/vitest-pool-workers';

export default {
  plugins: [
    cloudflareTest({
      wrangler: { configPath: './wrangler.jsonc', environment: 'test' },
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

`ts_test`'s Bazel layer turns `resolve.preserveSymlinks` on for the sandbox
reason above and, when the config's `plugins` hold the pool's, off: the pool
resolves modules for workerd through a second path, where a lexical path is a
second module identity for the same file. Left on, the run fails with
`TypeError: Cannot read properties of undefined (reading 'config')` from inside
the pool runner (pool 0.22.0; `No such module ".../vitest/dist/@vitest/spy"` on
0.18.4). The same layer resolves the compiled worker's bare imports from the
root, where the runfiles `node_modules` link is, and refuses an import of a
build output the runfiles do not hold; see
[the generated config](../rules/ts-test.md#the-generated-vitest-config).

`configPath` is relative to the config file, and `ts_test` roots Vite at the
config's package, so it names the file it names under plain `vitest`. That
file's `main` is `src/index.ts`, the deploy entry; `wrangler_config` stages a
copy whose `main` and `env.test.main` are
`src/index.js`, the compiled worker, at the file's own path, and that is the
config the pool reads. A `rules` module the worker imports
(`import greeting from "./greeting.txt"`) is a src of the `ts_compile`, which
puts it in the runfiles.
[A Workers pool](../rules/ts-test.md#a-workers-pool) lists what else a config
can name. `//tests/workers` is the same-package shape: the config beside the
tests, `main: "src/index.js"`, and the file in `data`.

### `coverage_provider` and `cloudflare:test`

`coverage_provider = "istanbul"`. v8 coverage is counters read back out of
Node's inspector, and workerd has none; istanbul instruments before the code
crosses into the runtime, so `bazel coverage` reports per-line data for code
running inside workerd.

The pool's ambient declaration for `cloudflare:test` is the `exports` subpath
`@cloudflare/vitest-pool-workers/types`, whose only condition is `types`.
Nothing imports it: the test file names it in a `/// <reference types>`
directive, as above, or the test's tsconfig names it in `types`, and tsgo
resolves either through the forest's `node_modules`, where the pool package is
because it is in `deps`, as tsc resolves it through pnpm's. See
[a `types` entry that names a package](../rules/ts-compile.md#a-types-entry-that-names-a-package).

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

The `.snap` is a src of the test, as every other file under the package is,
which is what puts it inside the sandbox; Gazelle lists it with the package's
files.

```python
ts_test(
    name = "widget_test",
    srcs = [
        "__snapshots__/widget.test.ts.snap",
        "widget.test.ts",
    ],
    deps = [":widget", "@npm//:vitest"],
)
```

Writing: vitest's own `vitest -u`, run in the package as outside Bazel, writes
the file where the test reads it. Commit the result.

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
