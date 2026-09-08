# ts_test

Compiles TypeScript test files and runs them inside the Bazel sandbox, with
vitest or with node's own test runner.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_test")

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
)
```

`ts_test` is a rule over `ts_compile`'s attributes: `srcs`, `deps` and
`tsconfig` mean what they mean there, the same actions compile the test files,
and the `node_modules` forest tsgo checked them against -- every dep providing
`NpmPackageInfo`, their transitive npm deps and the npm closure of every
`ts_compile` dep -- is the tree the tests run in. `runner` names the target
that runs the compiled files; see [Runners](#runners).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The test files, as `ts_compile`'s `srcs`, with every other file of the package -- a `.snap`, a fixture; the TypeScript ones are in the runfiles at their source paths too, see [Files at Run Time](#files-at-run-time) |
| `deps` | `label_list` | `[]` | `ts_compile` and `@npm//` targets the tests import. A `ts_compile` dep's data srcs are in the runfiles beside its `.js`, and a dep in the test's package has its TypeScript srcs there too |
| `tsconfig` | `label` | `None` | The test program's tsconfig, as on `ts_compile`: the package's own `tsconfig.json`, or a `ts_config` target. Every compiler option the tests check under is its, and under vitest its `paths` resolve at run time; see [The test's tsconfig](#the-tests-tsconfig) |
| `env` | `string_dict` | `{}` | Extra environment variables for the runner |
| `args` | `string_list` | `[]` | The runner's command-line flags: node's under the node:test runner, vitest's under the vitest runner; `bazel test --test_arg` appends to them. See [The node:test runner](#the-nodetest-runner) |
| `size` | `string` | `"medium"` | Bazel test size |
| `timeout` | `string` | `None` | Bazel test timeout |
| `tags` | `string_list` | `[]` | Bazel tags |
| `visibility` | `string_list` | `None` | Visibility of the test |
| `runner` | `label` | `//ts/runners:vitest` | The target that runs the compiled tests: `//ts/runners:vitest`, `//ts/runners:node_test` or any target providing `TsTestRunnerInfo`; see [Runners](#runners). Every attribute below this row except `data` is the vitest runner's, and an analysis error under the node:test runner |
| `config` | `label` | `None` | The vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`), **merged** over the generated config's Bazel layer. Every vitest setting is the file's, as under plain `vitest`; see [A config file](#a-config-file) |
| `config_srcs` | `label_list` | `[]` | The modules `config` imports relatively, and theirs, staged with the config's copy at their paths relative to the config's package. Gazelle writes it from the config's listing. A file outside that package is an analysis-time error; see [A config file](#a-config-file) |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, anything the tests read at run time |
| `wrangler_config` | `label` | `None` | The wrangler config a Workers-pool `config` names through `wrangler.configPath`. A copy whose `main` names the compiled entry is staged at the file's own runfiles path; the file is not also in `data`. See [A Workers pool](#a-workers-pool) |
| `coverage_provider` | `string` | `""` | `test.coverage.provider`: `"v8"` (vitest's default) or `"istanbul"`; see [Coverage](#coverage) |

## Files at Run Time

A test's runfiles hold, at their source paths, the compiled program -- its own
`.js` and every dep's -- the data srcs of every dep, and the TypeScript sources
of its own package: its `srcs` and the srcs of every dep in the same Bazel
package, `.ts`, `.tsx` and declarations. A test that reads its package's tree
finds it where the checkout has it:

```ts
const sdkSource = readFileSync(new URL("./index.ts", import.meta.url), "utf8");
```

`import.meta.url` is the runfiles path on either runner (vitest's
`resolve.preserveSymlinks`; node's `--preserve-symlinks-main` and the runner's
resolve hook), so `./index.ts` beside the compiled test is the same-package
`ts_compile`'s `src/index.ts`. Another package's sources are not in the tree: a
file a test reads across a package boundary is a `data` entry.
`//tests/vitest/reads_own_source` is the example.

The compiled program is what runs. A `setupFiles` entry naming a source runs the
compiled sibling staged beside it ([Setup Files](#setup-files)), and a relative
`.ts` specifier the emit keeps resolves to its compiled sibling
([Relative `.ts` Specifiers](#relative-ts-specifiers); under node:test, the
runner's hook: [The node:test Runner](#the-nodetest-runner)).

A bare specifier reaches the test's node_modules tree by the runner's own
route. Under vitest the resolver walks up from the test's runfiles path and
meets the `node_modules` link the launcher puts at the runfiles root. Under
node:test the hook resolves it from the tree the launcher names in `NODE_PATH`
([The node:test Runner](#the-nodetest-runner)).

## The Test's tsconfig

`tsconfig` is the test program's, as on
[`ts_compile`](ts-compile.md#where-compiler-options-come-from); it carries
every compiler option the tests check under. The test files are a program of
their own, so a `lib`, a `types` entry or a `paths`
alias the package's sources need is in the test program only when the test's
tsconfig has it too; Gazelle names the package's own `tsconfig.json` on the test
as on the compile.

Three entries are the ones a test usually needs:

- **`lib`.** A worker test needs `["esnext", "webworker"]`; `webworker` is in
  no set `target` implies.
- **A `types` entry naming a package or a subpath**, such as
  `@cloudflare/vitest-pool-workers/types` for a pool's `cloudflare:test`
  module, or `vitest/globals`. tsgo resolves it through the forest built from
  the test's `deps`, so the package is listed there; see
  [a `types` entry that names a package](ts-compile.md#a-types-entry-that-names-a-package).
- **A `types` entry naming a declaration file**, `../worker-configuration.d.ts`
  for the declaration a wrangler project keeps beside its worker. The target
  whose `srcs` hold the file, or whose `outs` write it, is in `deps`, and
  tsaction rebases the entry to the staged file; see
  [a `types` entry that names a declaration file](ts-compile.md#a-types-entry-that-names-a-declaration-file).

```jsonc
// workers/proxy/test/tsconfig.json
{
  "extends": "../tsconfig.json",
  "compilerOptions": { "types": ["../worker-configuration.d.ts"] }
}
```

```starlark
ts_test(
    name = "handler_test",
    srcs = ["handler.test.ts"],
    tsconfig = "tsconfig.json",
    deps = ["//workers/proxy:worker_types", "//workers/proxy/src", "@npm//:vitest"],
)
```

A `paths` alias resolves at run time as it did at compile time. oxc leaves an
import specifier alone, so a compiled test still names the alias, and the Bazel
layer of the generated config resolves it to the module the value names in the
runfiles, the compiled sibling for a `.ts` value ([A `paths`
Alias](#a-paths-alias)). A value import through an alias into another package
needs that package's target in `deps`, which is what puts the module in the
runfiles. Under node:test an alias is type-checking only
([The node:test Runner](#the-nodetest-runner)).

## The Generated vitest Config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It is an entry config
that layers four sources, lowest precedence first; every vitest setting the
table does not name -- `test.environment`, `test.globals`, `test.setupFiles`,
`test.globalSetup`, `test.reporters`, `test.coverage.thresholds` -- is the
`config` file's, as under plain `vitest`:

| Layer | Contents | Workspace projects |
|-------|----------|---|
| 1. Bazel | `root` (the `config`'s package; the test's own with none), `cacheDir` under `TEST_TMPDIR`, `resolve.preserveSymlinks`, `test.coverage.allowExternal`, `test.include` naming the compiled test files, the plugin resolving a relative `.ts` specifier to its compiled sibling, the plugin resolving a tsconfig `paths` alias, the plugin serving a `setupFiles` entry from its staged path, and under a `config` whose `plugins` hold `@cloudflare/vitest-pool-workers` `preserveSymlinks: false` and the runfiles-imports plugin | yes |
| 2. user | the `config` file | it supplies the projects |
| 3. provider | `test.coverage.provider` from `coverage_provider` | no, root only |
| 4. snapshots | `test.resolveSnapshotPath` | no, root only |

Objects merge key by key; arrays concatenate base-first, matching vite's own
`mergeConfig`, so a `plugins` list in the config joins layer 1's rather than
displacing it. Scalars from a later layer win: a `resolve.preserveSymlinks` the
config sets wins over layer 1's default, and `coverage_provider` over a
provider the config names. Once the layers have merged, a `setupFiles` or
`globalSetup` entry naming a TypeScript source is rewritten to its compiled
sibling; see [Setup Files](#setup-files).

Layers 3 and 4 are root-only because coverage and `resolveSnapshotPath` are
vitest's non-project options: each is applied once, to the root config, and
never merged into a project.

Layer 1's `include` is the compiled test files, relative to the root: under
Bazel the run is the rule's `srcs`. A config's `include` is written for the
sources (`**/*.test.{ts,tsx}`), which the compiled `.js` the launcher names
never match, and the run would stop with `No test files found`; arrays
concatenate, so the config's globs stay in the merged array and select
nothing the launcher did not name. `//tests/vitest/config_include` is the
example.

`preserveSymlinks` in layer 1 is a default. A DOM environment resolves every
module id to its realpath, which for a runfiles symlink walks out of the test
sandbox, so layer 1 turns it on. Under `@cloudflare/vitest-pool-workers` a
lexical path is a second module identity for the same file, so when the
`config`'s `plugins` hold the pool's plugin layer 1 turns it off instead. Left
on, the pool fails inside its runner: `Cannot read properties of undefined
(reading 'config')` on pool 0.22.0 / vitest 4.1.11, `No such module
".../vitest/dist/@vitest/spy"` on 0.18.4 / 4.1.5. A `resolve.preserveSymlinks`
the config sets still wins, as any user value does.

On realpaths a compiled module is imported from `bazel-out`, which has no
`node_modules` above it and every output the output base holds beside it, so
the same case adds a plugin: a compiled module's relative imports are resolved
from its runfiles path (what the test stages, under whatever name), its bare
imports from the root, where the runfiles tree's `node_modules` link is, and an
import that resolves to a build output the runfiles do not hold at its own path
is refused with `rules_typescript: "<id>" resolved to <path>, a build output
this test's runfiles do not hold`. `//tests/workers` is the example with the
config beside the tests, `//tests/workers_nested` the one with the config at the
package root.

Two things sit outside the layering and outrank it: npm resolution into the
runfiles tree (the launcher's `node_modules` link at the runfiles root,
[Files at Run Time](#files-at-run-time)) and coverage output paths
(vitest CLI flags, so `bazel coverage` writes lcov where Bazel expects it).

To see what the launcher resolved (the node binary, the vitest entry, the
`node_modules` tree, the shard split):

```bash
bazel run //path/to:my_test -- --dump-config
```

Read the config that actually ran:

```bash
bazel build //path/to:my_test --output_groups=vitest_config
```

### A Config File

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    deps = [":button", "@npm//:react", "@npm//:vitest"],
    config = "vitest.config.ts",
    data = ["test/fixtures.json"],
)
```

The file may default-export an object, a function of `env`, or a promise of
either. An array is read as a list of vitest projects and becomes
`test.projects`; each project in it receives the Bazel layer too, because every
project gets its own Vite server.

Vite's root is the config's package, so a relative path in the config names the
directory the file sits in, as under plain `vitest`, whether the test is in that
package or one below it; with no config it is the test's package. The config
file itself is a copy beside the `node_modules` tree, where
its bare imports resolve, so a path relative to the config file is a different
path. `TS_TEST_PACKAGE_DIR` holds the test's package directory, which the root
is resolved from, for a path that has to be absolute.

Vite bundles the config from that copy's realpath, so a module the config
imports relatively has to be a copy beside it too: `config_srcs` names the
modules the config imports and the ones they import, first-party files of the
config's package, and each is staged at its path relative to that package
(`./plugins/foo` in the copy is `plugins/foo.ts` beside it, and a bare import in
`plugins/foo.ts` walks up to the same tree). Gazelle writes the attribute from
the config's listing ([What Gazelle Writes](../gazelle/overview.md#what-gazelle-writes)). A
module outside the config's package is an analysis-time error naming it; a
config from an ancestor package names its modules as that package's files,
`//<package>:<file>`. `//tests/vitest/config_srcs` is the example.

!!! warning "The array form needs vitest 3.2 or later"
    `test.projects` is the name `test.workspace` was renamed to in vitest 3.2;
    vitest 4 removed the old name and throws on it. The
    generated config emits `test.projects`, so a `config` that default-exports
    an array needs vitest 3.2 or later. Every other `config` shape (object,
    function, promise) uses no version-sensitive key.

### Setup Files

`test.setupFiles` in the `config` names the files that run before every test
file, which is where DOM polyfills (`matchMedia`, `ResizeObserver`,
`PointerEvent`) belong; `test.globalSetup` runs once around the whole run. An
entry is resolved against the root, and the program vitest runs is the compiled
one. Once the
layers have merged, an entry ending in `.ts`, `.tsx`, `.mts` or `.cts` whose
compiled sibling is in the runfiles (`.js`, `.mjs` or `.cjs`; `.jsx` for a
`.tsx` under `jsx: preserve`) is rewritten to that sibling, so
`setupFiles: ["./test/vitest.setup.ts"]` in a config at the package root runs
`test/vitest.setup.js` whether or not the source is beside it. An entry with no
compiled sibling staged is left as written and fails to load: `Cannot find
module '.../test/vitest.setup.ts'`. The `ts_compile` over the source has to be
in `deps`, and nothing imports a setup file, so Gazelle writes no such dep: the
entry is `# keep`. `//tests/setup_files_compiled` is the example, with the
config at the package root and beside the tests; its tests assert the compiled
sibling ran.

vitest then resolves each `setupFiles` entry through Node's resolver, which
follows the runfiles link to the compiled file in `bazel-out`. The `node`
environment loads that realpath as it stands. A DOM environment (`jsdom`,
`happy-dom`: Vite's `client` environment) loads setup files through Vite, which
serves the root and refuses a path outside it: `Cannot find module
'/@fs/<bazel-out path>/vitest.setup.js'`. Layer 1 carries a plugin that answers
that request with the staged path, so a setup file loads the way a test file
does, its imports resolved from the runfiles tree.
`//tests/setup_files_compiled/dom` is the example.

### Relative `.ts` Specifiers

`import { x } from "./util.ts"` is legal TypeScript under
`allowImportingTsExtensions`, and the emit keeps the specifier as written, so
the compiled test imports `./util.ts`. The runfiles hold `util.js` and, for a
source of the test's package, `util.ts` beside it
([Files at Run Time](#files-at-run-time)); resolved as written, a same-package
import runs the source under vite's transform, and an import of another
package's file fails with

```
Error: Cannot find module './util.ts' imported from .../util.test.js
```

Layer 1 carries a plugin that resolves a relative specifier ending in `.ts`,
`.tsx`, `.mts` or `.cts` to the compiled sibling beside it (`.js`, `.mjs` or
`.cjs`; `.jsx` for a `.tsx` under `jsx: preserve`) whenever that sibling exists,
from the importing file's directory, whether or not the source is there -- the
rule a `setupFiles` entry is rewritten by, applied to every import. A specifier
with no compiled sibling resolves as written, and the source and the emit are
untouched: no `rewriteRelativeImportExtensions`, no edit to the `import`.
`//tests/vitest/relative_ts` is the example: the test imports `./lib.ts`, lib
`./deep/util.ts`, and both assert the `.js` ran.

### A `paths` Alias

A compiled test imports `@app/flags` as its source did: oxc leaves the
specifier alone, and vitest alone would resolve it as a package. At compile
time tsgo resolved it through the tsconfig's `paths`
([ts-compile.md](ts-compile.md#importing-another-target-by-bare-specifier));
at run time layer 1 does the same, over the runfiles.

The `TsTestPaths` action reads the `tsconfig` chain with the reader the
compile's `TsConfig` action uses and writes its `paths` beside the generated
config, with the directory of the chain file that set them (a leaf replaces
the map whole, as under tsc). The plugin matches a specifier the way tsc does:
the exact key first, then the pattern with the longest prefix; the matched
key's values in order, each read from that directory, a value naming a `.ts`
swapped for its compiled sibling before resolving; the first value that
resolves wins, and one that resolves nothing leaves the specifier to vite. A
relative, absolute or virtual specifier is never matched.

```jsonc
// tsconfig.json
{ "compilerOptions": { "paths": {
  "@shared/*": ["./shared/*"],
  "@platform/auth": ["./lib/auth/platform-adapter.ts"]
} } }
```

`@shared/flags` resolves to `shared/flags.js` (vite's extension order puts the
compiled `.js` before the staged `.ts`); `@platform/auth` to
`lib/auth/platform-adapter.js`, the sibling of the file the value names. A
value under another package resolves when that package's target is in `deps`,
which stages its compiled module; the source it names is not in the runfiles
and is not needed. A `config` that sets `resolve.tsconfigPaths` still finds no
`tsconfig.json` in the runfiles, and does not need one.
`//tests/vitest/path_aliases` is the example: a pattern alias, an exact alias
naming a `.ts`, and an alias into another package, every one a value import.

### A Workers Pool

A `config` whose `plugins` hold `@cloudflare/vitest-pool-workers` runs the tests
inside workerd. Four things put the compiled worker in front of it. Three are
layer 1's, above: the root is the config's package, so `wrangler.configPath`
names the file beside the config; `resolve.preserveSymlinks` is off, so a module
has one identity; a compiled module's bare imports are resolved from the root,
where the runfiles tree's `node_modules` link is. The fourth is
`wrangler_config`:

```python
ts_test(
    name = "worker_test",
    srcs = ["worker.test.ts"],
    config = "//workers/proxy:vitest_config",
    coverage_provider = "istanbul",
    tsconfig = "tsconfig.json",   # lib esnext + webworker; types @cloudflare/vitest-pool-workers/types
    wrangler_config = "//workers/proxy:wrangler.jsonc",
    deps = [
        "//workers/proxy:worker",
        "@npm//:cloudflare_vitest-pool-workers",
        "@npm//:vitest",
        "@npm//:vitest_coverage-istanbul",
    ],
)
```

The pool boots the file `main` names, resolved against the config file's
directory. In a repository that is the source, `src/index.ts`, and the worker
under test is the compiled one. A build action, `WranglerTestConfig`, copies
the file and patches `main` and every `env.<name>.main` to the compiled entry
with wrangler's `experimental_patchConfig` (`.ts` and `.tsx` to `.js`, `.mts`
to `.mjs`, `.cts` to `.cjs`; a `.js` is left as written). wrangler is the one
in the test's `node_modules` tree, resolved from the pool package, so the copy
is patched by the reader that parses it. The copy is staged at the source's
runfiles path, which is what `configPath` names, and under its own name beside
the generated config, which is what admits its realpath when a `?raw` import of
the config re-resolves it. Comments and every other key survive; the formatting
is wrangler's. A config naming no `main`, or a `.toml` holding `#` comments,
fails the action.

The pool's half of the rule -- `wrangler_config`, the copy of the layer beside
the generated config, `WranglerTestConfig`, and the runfiles and symlink both
add -- is `ts/private/actions/workers_pool.bzl`, the one file that names
wrangler; `ts_test` reaches it through the one struct it returns, so the file
moves to a Workers ruleset as it is.

A runfiles file at the copy's path wins over it silently, with the unpatched
`main`. The file in `data` as well is an analysis error, `is staged through
wrangler_config; do not list it in data too.`, and a `ts_compile` dep's data
src at that path is dropped from the runfiles. Every other data src of the deps
is in the runfiles, which is what a wrangler `rules` module the worker imports
needs. `//tests/workers_nested` is the
example; `//tests/workers`, with the config beside the tests and
`main: "src/index.js"` in `data`, is the same-package one.

What else a wrangler config names, and where each comes from under `ts_test`:

| Key | Source |
|---|---|
| `main`, `env.<name>.main` | the compiled entry, through the patched copy |
| `rules` modules (`**/*.txt`, `**/*.md`, ...) | a src of the worker's `ts_compile` |
| `assets.directory` | its contents in `data`, at the same path relative to the config |
| `.dev.vars`, `.dev.vars.<env>` | read beside the config; in `data` when a test needs one |
| `compatibility_date`, `compatibility_flags`, `vars`, `kv_namespaces`, `r2_buckets`, `services`, `durable_objects`, `migrations` | inline values; miniflare emulates the bindings and nothing is staged |
| `durable_objects[].script_name` naming another worker | `miniflare.workers` in the config, as under plain `vitest` |
| `tsconfig`, `alias`, `define`, `no_bundle`, `build` | esbuild and deploy keys; the pool runs none of them |

## Finding What a Test Reads

A test that opens a file the runfiles do not hold fails in the sandbox with
`ENOENT`, and nothing in the build says which file: a run-time read is in no
program listing, so Gazelle writes no `data` entry for it. The vitest runner
says, under `bazel run`:

```bash
bazel run //path/to:my_test -- --reads
```

It runs the same compiled tests unsandboxed, from the runfiles tree `bazel
test` runs them in, under a Node `--require` hook that records every path the
test processes open, stat or read. Once vitest has exited it prints, on
stdout, every regular file under the workspace that the run reached outside
the runfiles tree -- once, sorted, as the workspace-relative path and the
label a `data` entry would take, with the nearest `BUILD` file naming the
package:

```
MODULE.bazel	//:MODULE.bazel
tests/vitest/reads_report/fixtures/outside.txt	//tests/vitest/reads_report/fixtures:outside.txt
```

vitest's own output goes to stderr, so stdout is the report alone; a test
reading nothing outside its runfiles prints nothing. The exit status is the
test's.

The report is the test processes' reads, wherever they came from. A test that
walks up from its own directory to a `MODULE.bazel` or a `package.json` leaves
the runfiles tree, finds the workspace's file through the execution root and
reads from there, and the marker it stopped at is in the report beside the
files it then opened: both go in `data`, or under `bazel test` the walk still
finds nothing. A path the test never reaches (an `ENOENT` in this run too) is
not in it, and what the vitest process itself opens -- a `config`, a
`globalSetup` entry -- is not recorded: its walk up from the root for a workspace
marker is vite's, not a test's.

`data` is the owner's attribute: Gazelle writes nothing into it and leaves what
is written, so the labels go in as they are, with an `exports_files` in the
package that holds a file another package's test reads:

```python
ts_test(
    name = "reads_declared_test",
    srcs = ["reads_declared.test.ts"],
    data = [
        "//:MODULE.bazel",
        "//tests/vitest/reads_report/fixtures:outside.txt",
    ],
    deps = [":workspace_root", "@npm//:types_node", "@npm//:vitest"],
)
```

`//tests/vitest/reads_report` is the example: `reads_report_test` makes the
read undeclared and is `manual`, so `bazel test` never runs the failure, and
run with `--reads` it prints the two lines above; `reads_declared_test` lists
them, passes under `bazel test`, and run with `--reads` prints nothing.

## Coverage

`bazel coverage //path/to:my_test` works on any `ts_test` on the vitest runner
with no attribute set; `@vitest/coverage-v8` must be in `node_modules`. A target
on the node:test runner reports no coverage and says so.

### Which Files Are Reported

`--instrumentation_filter` selects the targets whose files reach the report, and
`ts_test` reports on the selection and nothing else. Bazel derives a default
from the targets on the command line; for `bazel coverage //foo:bar_test` that
is `^//foo[/:]`, so a library in another package is absent from the report until
a wider filter names it:

```bash
bazel coverage //tests/vitest/coverage:math_coverage_test --combined_report=lcov
# SF:tests/vitest/coverage/same_package.js only

bazel coverage //tests/vitest/coverage:math_coverage_test --combined_report=lcov \
    --instrumentation_filter='^//tests/vitest[/:]'
# adds SF:tests/vitest/math.js
```

Every `ts_compile` carries its own `InstrumentedFilesInfo`, so the filter is
applied per target. The test's own files are a test target's, which Bazel
leaves out of the report unless `--instrument_test_targets` is set. None
declares baseline coverage files: a
baseline would name the `.ts` a target declared, and the runner reports on the
`.js` compiled from it, so the record would be a second name for the same code
carrying no lines at all.

### Choosing a Provider

A test whose pool runs the tests in a second runtime, such as a
`@cloudflare/vitest-pool-workers` test in workerd, needs istanbul. v8 coverage
is counters read back out of Node's inspector, which workerd has no equivalent
of; istanbul instruments at transform time, before the code crosses the boundary.
Set `coverage_provider = "istanbul"` and put `@vitest/coverage-istanbul`, pinned
to the same version as `vitest`, in `deps`:

```python
ts_test(
    name = "worker_test",
    srcs = ["src/worker.test.ts"],
    config = "vitest.workers.config.mjs",
    coverage_provider = "istanbul",
    deps = [
        ":worker",
        "@npm_workers//:cloudflare_vitest-pool-workers",
        "@npm_workers//:vitest",
        "@npm_workers//:vitest_coverage-istanbul",
    ],
)
```

With only `@vitest/coverage-istanbul` in `deps` and no `coverage_provider`,
vitest falls back to its v8 default and the run fails with
`MISSING DEPENDENCY  Cannot find dependency '@vitest/coverage-v8'`.

Coverage is reported against the compiled `.js` in `bazel-out`, not the `.ts`
source, so `SF:` paths and line numbers are the compiler's. That holds for every
`ts_test`, pooled or not.

## Running Tests

```bash
bazel test //path/to:math_test
```

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`, on either runner. Set `shard_count` on the target and pass
`--noincompatible_check_sharding_support`: the runner never touches
`TEST_SHARD_STATUS_FILE`, which is how Bazel expects a test runner to advertise
sharding support, so without that flag a sharded run fails before any test
starts.

## Runners

A runner is a target providing `TsTestRunnerInfo`
([providers](providers.md#tstestrunnerinfo)), the way a toolchain is: `ts_test`
compiles the tests and builds the forest, and the runner's `launch` turns them
into the launcher's config and the runfiles of one test. Two ship,
`//ts/runners:vitest`, the default, and `//ts/runners:node_test`
(`@rules_typescript//ts/runners:node_test` from a consumer); a third is a rule
in its own ruleset returning the provider. A runner names the npm packages it
needs in the tree -- `vitest` for the vitest runner -- and a test whose `deps`
provide none fails at analysis naming it.

`runner` is the owner's attribute. tsgo's listing records no edge for an import
of an ambient module -- `node:test` resolves to no file -- so nothing Gazelle
reads says which runner a test file was written for; Gazelle never writes
`runner`, and a hand-written value survives every run without `# keep`.

## The node:test Runner

A test written against [`node:test`][node-test] registers with node's runner,
not with vitest's collector, so vitest reports `0 test` for the file and fails
it as an empty suite. `runner = "//ts/runners:node_test"` runs such a file
under `node --test` instead:

```python
ts_test(
    name = "scripts_test",
    srcs = ["cloudflare-account-token.test.ts"],
    runner = "@rules_typescript//ts/runners:node_test",
    deps = [":scripts", "@npm//:types_node"],
)
```

`tsconfig` carries over unchanged and means on a node:test target what it means
above, but a `paths` alias is type-checking only under node:test: the resolve
hook below answers a relative specifier and a bare one from the tree, and
nothing reads the chain's `paths`. Under vitest an alias resolves at run time;
see [The test's tsconfig](#the-tests-tsconfig).

The package's code runs at its runfiles paths, as under vitest: the launcher
passes `--preserve-symlinks-main`, and the runner target's `node:module`
resolve hook (`ts/private/node_test_hook.mjs`) resolves every specifier from a
file outside the node_modules tree:

- A relative specifier resolves, at the importer's runfiles path, to the file
  the compiled tree holds for it: `./util.ts`, `.tsx`, `.mts` or `.cts` to the
  compiled sibling (`.js`; `.jsx` for a `.tsx` under `jsx: preserve`; `.mjs`;
  `.cjs`), whether or not the source is staged beside it; an extensionless
  `./util` to `util.js` or `util/index.js`, the file bundler resolution named at
  compile time; any other to the file as written. `//tests/node_test` pins the
  three: `:ts_specifier_test`, `:extensionless_test`, `:runfiles_layout_test`.
- A bare specifier resolves from the test's node_modules tree, the directory
  the launcher puts on `NODE_PATH`, never from a `node_modules` the walk up
  from `bazel-out` happens to meet (`:bare_import_test`). Code inside the tree
  resolves as node resolves it, at its realpath, so a link the tree holds for
  a second resolution of a name works as installed.

Under vitest, [layer 1's plugin](#relative-ts-specifiers) resolves the relative
`.ts` specifier and the generated config the rest.

node:test takes no config file; it is configured by CLI flags and by the test
file itself. `args` are those flags, placed before `--test` so that the
children `node --test` spawns inherit them: a suite that calls `mock.module`
sets `args = ["--experimental-test-module-mocks"]`, as its package's test
script does (`//tests/node_test:module_mocks_test`). Every vitest attribute is
an analysis error under it, naming the ones set:

```
ts_test @@//scripts:scripts_test: the node:test runner reads none of config,
coverage_provider. Every one of them configures vitest, which this target does
not run. Drop them, or drop `runner` to run the test under vitest.
```

The rejected set is `config`, `coverage_provider` and `wrangler_config`. The
runner takes no argument under `bazel run`.

`--test_filter` reaches node as `--test-name-pattern` (a regular expression
over test names), sharding works as above, and the exit status is the test
result. Nothing writes a JUnit XML on either runner; Bazel synthesises
`test.xml` from the log.

[node-test]: https://nodejs.org/api/test.html

## Snapshots

Vitest resolves a `.snap` beside the test file it ran, which under Bazel is the
compiled `.js` in `bazel-out`. `ts_test` replaces that resolution with the path
the `.ts` source implies:

```
<package>/__snapshots__/<source file name>.snap
```

the path a plain `vitest` run uses. The `.snap` is a src of the test, as every
other file under the package is ([`srcs`](ts-compile.md#sources)), which is what
puts it in the sandbox; Gazelle lists it with the package's files:

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

A snapshot the test cannot read is a failure. `ts_test` runs vitest in read-only
snapshot mode (`CI=true`), so no `bazel test` writes a `.snap`; `env = {"CI":
"false"}` opts out.

Writing one is vitest's own `vitest -u`, run in the package as outside Bazel:
it writes the file where the read above resolves it. Commit the result.

## Debugging

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
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

## Listing npm Deps

Test sources are checked for undeclared imports like any other `ts_compile`
sources: a module that only some dep's own deps provide fails the build with the
label to add. See
[Deps have to be direct](ts-compile.md#deps-have-to-be-direct).

A `ts_compile` dep brings its npm closure into the forest: its
compiled JS value-imports the packages it declared, and `TsInfo`
carries that closure (`npm_packages`) beside the declarations, so a
test in one package runs production code from another without repeating its npm
deps. `deps` lists what the test files import; where the closure resolves a name
more than one way, the test's own dep is the resolution that sits flat.
`bazel run //:gazelle` writes the list from tsgo's listing of the package:
the edges of the test files, the production sources and the declarations, the
vitest config's imports, and the nearest `package.json`'s `dependencies` and
`devDependencies`.

The tree keys each resolution apart by name, version and peer set wherever one
name resolved more than once; see [the layout](node-modules.md#the-layout).

See [Testing with vitest](../guides/testing.md) for the full guide including
watch mode and build feedback.
