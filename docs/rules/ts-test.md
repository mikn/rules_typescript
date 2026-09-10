# ts_test

Compiles TypeScript test files with `ts_compile`'s actions and runs them inside
the Bazel sandbox under a runner target: vitest by default, node's own runner,
or one of your own.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_test")

ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
)
```

`srcs`, `deps`, `tsconfig` and `node_modules` are `ts_compile`'s. The same
actions compile the test files -- the emit as ES modules under the vitest
runner ([Runners](#runners)) -- and the importer chain tsgo checked them
against ([The node_modules Chain](ts-compile.md#the-node_modules-chain)) is
what they run in: the chain's links and the store files the program reaches
sit in the runfiles at their own paths. `runner` names the target that runs
the compiled files.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The test files, as `ts_compile`'s `srcs`, with every other file of the package: a `.snap`, a fixture. The TypeScript ones are in the runfiles at their source paths too; see [Files at Run Time](#files-at-run-time) |
| `deps` | `label_list` | `[]` | `ts_compile` and `@npm//` targets the tests import. A `ts_compile` dep's data srcs are in the runfiles beside its `.js`, and a dep in the test's package has its TypeScript srcs there too |
| `tsconfig` | `label` | `None` | The test program's tsconfig, as on `ts_compile`: the package's own `tsconfig.json` or a `ts_config` target; under vitest its `paths` resolve at run time. See [The Test's tsconfig](#the-tests-tsconfig) |
| `node_modules` | `label` | `None` | The nearest lockfile importer's `node_modules` target, as on `ts_compile`: the chain the tests resolve their npm deps along, at run time too. See [Files at Run Time](#files-at-run-time) |
| `runner` | `label` | `//ts/runners:vitest` | The target that runs the compiled tests: `//ts/runners:vitest`, `//ts/runners:node_test` or any target providing `TsTestRunnerInfo`. See [Runners](#runners) |
| `env` | `string_dict` | `{}` | Extra environment variables for the runner |
| `args` | `string_list` | `[]` | The runner's command-line flags: node's under the node:test runner, vitest's under the vitest runner; `bazel test --test_arg` appends to them. See [The node:test Runner](#the-nodetest-runner) |
| `config` | `label` | `None` | The vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`), merged over the generated config's Bazel layer. Every vitest setting is the file's. See [A Config File](#a-config-file) |
| `config_srcs` | `label_list` | `[]` | The modules `config` imports relatively, and theirs, each at its own path in the runfiles; Gazelle writes it from the config's listing. See [A Config File](#a-config-file) |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, anything read at run time |
| `wrangler_config` | `label` | `None` | The wrangler config a Workers-pool `config` names through `wrangler.configPath`. See [A Workers Pool](#a-workers-pool) |
| `coverage_provider` | `string` | `""` | `test.coverage.provider`: `"v8"` (vitest's default) or `"istanbul"`. See [Coverage](#coverage) |
| `size`, `timeout`, `tags`, `visibility` | | | Bazel's test attributes |

`config`, `config_srcs`, `coverage_provider` and `wrangler_config` are the
vitest runner's and an analysis error under the node:test runner.

## Files at Run Time

A test's runfiles hold, at their source paths, the compiled program -- its own
`.js` and every dep's -- the data srcs of every dep, and the TypeScript sources
of its own package: its `srcs` and the srcs of every dep in the same Bazel
package, `.ts`, `.tsx` and declarations. A test that reads its package's tree
finds it where the checkout has it:

```ts
const sdkSource = readFileSync(new URL("./index.ts", import.meta.url), "utf8");
```

`import.meta.url` -- and `__dirname`, in a CommonJS test -- is the runfiles
path on either runner (the vitest layer's module ids, [The Generated vitest
Config](#the-generated-vitest-config); node's `--preserve-symlinks-main` and
the runner's resolve hook), so `./index.ts`
beside the compiled test is the same-package `ts_compile`'s `src/index.ts`.
Another package's sources are not in the tree: a file a test reads across a
package boundary is a `data` entry.
`//tests/vitest/reads_own_source` is the example.

vitest runs from the `config`'s package in the runfiles, the test's own with no
config -- the directory `pnpm run test` runs from -- so `process.cwd()` names it
and `join(process.cwd(), "fixtures/x.txt")` reads the package's file. The
`config` file and its `config_srcs` are regular files at their own paths in the
runfiles, and the generated config vitest is handed is in the test's package,
all written by the launcher before the run: a runfiles entry is a symlink, and
Vite bundles a config from its realpath, so through the symlink `__dirname`
and the walk up for a bare import would start in `bazel-out`. `__dirname`,
`import.meta.dirname` and `__filename` in the config and in a `config_srcs`
module are the package's paths, as under plain `vitest`, and a bare import
walks up to the importer's `node_modules`.
`//tests/vitest/cwd_and_dirname` is the example.

The compiled program is what runs -- under vitest as ES modules, whatever the
tsconfig's `module` ([Runners](#runners)). A `setupFiles` entry naming a
source runs the compiled sibling staged beside it ([Setup Files](#setup-files)),
and a
`.ts` specifier the emit keeps -- relative, or a subpath into a workspace
member -- resolves to the compiled file ([`.ts` Specifiers](#ts-specifiers);
under node:test, the runner's hook:
[The node:test Runner](#the-nodetest-runner)).

A bare specifier resolves through the importer chain by the runner's own
route. Under vitest the resolver walks up from the test's runfiles path
through each importer's `node_modules` to the lockfile's root importer's;
where that walk meets none before the workspace's root, the launcher links the
chain's root in there. Under node:test the hook resolves it from the chain's
directories the launcher names in `NODE_PATH`, nearest first
([The node:test Runner](#the-nodetest-runner)). A package's own imports
resolve from its realpath in the store to the edges beside its tree, on both
runners (`//tests/npm/multi_version:own_edge_node_test`,
`:own_edge_vitest_test`), and an import of a name it does not declare to the
hoist link the runfiles carry for a name the closure holds
(`//tests/npm/features/hoist`).

## The Test's tsconfig

`tsconfig` is the test program's, as on
[`ts_compile`](ts-compile.md#where-compiler-options-come-from). The test files
are a program of their own, so a `lib`, a `types` entry or a `paths` alias the
package's sources need is in the test program only when the test's tsconfig
has it too; Gazelle names the package's own `tsconfig.json` on the test as on
the compile. Three entries are the ones a test usually needs:

- **`lib`.** A worker test needs `["esnext", "webworker"]`; `webworker` is in
  no set `target` implies.
- **A `types` entry naming a package or a subpath**, such as
  `@cloudflare/vitest-pool-workers/types` for a pool's `cloudflare:test`
  module, or `vitest/globals`. tsgo resolves it through the chain
  `node_modules` names, so the package is in `deps`, which stages the
  importer's link the walk up meets; see
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

## Runners

A runner is a target providing `TsTestRunnerInfo`
([providers](providers.md#tstestrunnerinfo)), the way a toolchain is: `ts_test`
compiles the tests against the importer chain, and the runner's `launch` turns
them into the launcher's config and the runfiles of one test. Two ship,
`//ts/runners:vitest`, the default, and `//ts/runners:node_test`
(`@rules_typescript//ts/runners:node_test` from a consumer); a third is a rule
in its own ruleset returning the provider. A runner names the npm packages it
needs in the closure -- `vitest` for the vitest runner -- and a test whose
`deps` provide none fails at analysis:

```
ts_test @@//path/to:my_test: @@//ts/runners:vitest runs the tests and needs
vitest in the npm closure, which no dep provides.
Add the hub label of each to deps.
```

`runner` is the owner's attribute. tsgo's listing records no edge for an import
of an ambient module -- `node:test` resolves to no file -- so nothing Gazelle
reads says which runner a test file was written for; Gazelle never writes
`runner`, and a hand-written value survives every run without `# keep`.

The two runners run different module formats, and a runner says which with
`TsTestRunnerInfo.es_modules`. vitest imports every file through vite's ESM
transform, so the program a vitest test runs is ES modules whatever its
tsconfig's `module`: the test's own srcs are emitted as such, and a
`ts_compile` dep whose program tsgo emits -- `module: "commonjs"`, or
`"nodenext"` in a package with no `type` -- is staged as the ES twins it
emits beside its `.js`
([The Module Format](ts-compile.md#the-module-format)). node:test has no
transform, so it runs the package's format as tsc emits it
([The node:test Runner](#the-nodetest-runner)). `//tests/vitest/commonjs` and
`//tests/node_test/cjs` pin the two over one `module: commonjs` shape.

## The vitest Runner

vitest is the one the first importer on the chain links, from the test's
package up, the JS runtime the toolchain's: the Node your `.nvmrc` names
([Node.js](../getting-started/quickstart.md#nodejs)). Under `bazel test` the
runner sets `CI=true` so vitest writes no `.snap` ([Snapshots](#snapshots));
`env = {"CI": "false"}` opts out.

### The Generated vitest Config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It layers four sources,
lowest precedence first; every vitest setting the table does not name --
`test.environment`, `test.globals`, `test.setupFiles`, `test.globalSetup`,
`test.reporters`, `test.coverage.thresholds` -- is the `config` file's, as under
plain `vitest`:

| Layer | Contents | Workspace projects |
|-------|----------|---|
| 1. Bazel | `root` (the `config`'s package; the test's own with none), `cacheDir` under `TEST_TMPDIR`, `server.fs.allow` naming the workspace's runfiles and `bazel-bin`'s realpath, `test.coverage.allowExternal`, `test.include` naming the compiled test files, `test.server.deps.inline` naming each workspace member in the closure, the plugin giving each module its id (below), the plugin resolving a relative `.ts` specifier to its compiled sibling, and the plugin resolving a tsconfig `paths` alias | yes |
| 2. user | the `config` file | it supplies the projects |
| 3. provider | `test.coverage.provider` from `coverage_provider` | no, root only |
| 4. snapshots | `test.resolveSnapshotPath` | no, root only |

Objects merge key by key; arrays concatenate base-first, as vite's own
`mergeConfig` does, so a `plugins` list in the config joins layer 1's. Scalars
from a later layer win: a `cacheDir` the config sets wins over layer 1's, and
`coverage_provider` over a provider the config names. Layers 3
and 4 are root-only because coverage and `resolveSnapshotPath` are vitest's
non-project options, applied once and never merged into a project.

Layer 1's `include` is the compiled test files, relative to the root: under
Bazel the run is the rule's `srcs`. A config's `include` is written for the
sources (`**/*.test.{ts,tsx}`), which the compiled `.js` the launcher names
never match, and the run would stop with `No test files found`; arrays
concatenate, so the config's globs stay in the merged array and select
nothing the launcher did not name. `//tests/vitest/config_include` is the
example.

A module's id is its runfiles path where the runfiles hold the file and its
realpath otherwise. Vite resolves every id to its realpath -- a test file's and
a compiled module's lie in `bazel-out`, a source file's in the source tree --
and layer 1's plugin gives a file the runfiles hold at that path its runfiles
path back, so `import.meta.url` and a relative import stay in the runfiles
tree; a file under `node_modules` keeps its realpath, so a package's own
imports resolve from its place in the tree, and one file is one module. An id
that resolves to a file the runfiles do not hold is refused:
`rules_typescript: "<id>" resolved to <path>, which this test's runfiles do not
hold`. `server.fs.allow` names the workspace's runfiles and `bazel-bin`'s
realpath, the two places an id can be, for the DOM environments that load
through Vite's server.

`server.deps.inline` in layer 1 names every workspace member in the closure.
vitest runs a module under `node_modules` in node unless a pattern names it,
and a member's emitted `.js` keeps the extensionless relative imports vite
resolves and node's loader rejects; under pnpm the same member is inlined
because its link's realpath lies outside `node_modules`. Inlined, a member's
own imports resolve through vite with the environment's conditions on both
layouts, and vitest 4 gives a DOM environment the server set (`node`,
`development|production`): a package whose exports map splits `browser` from
`node` answers with its node build unless the `config` sets
`resolve.conditions`.

Two things sit outside the layering: npm resolution into the runfiles tree
(the launcher's `node_modules` link at the runfiles root, [Files at Run
Time](#files-at-run-time)) and coverage output paths (vitest CLI flags, so
`bazel coverage` writes lcov where Bazel expects it).

```bash
bazel run //path/to:my_test -- --dump-config           # what the launcher resolved
bazel build //path/to:my_test --output_groups=vitest_config   # the config that ran
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
package or one below it; with no config it is the test's package. vitest runs
from that directory, and the config is loaded from its own path in it, so
`__dirname` and `import.meta.dirname` name it too ([Files at Run
Time](#files-at-run-time)).

`config_srcs` names the modules the config imports relatively and the ones
they import; each is written at its own path in the runfiles, so `./plugins/foo`
is `plugins/foo.ts` beside the config, and a bare import in `plugins/foo.ts`
walks up to the runfiles tree's `node_modules`. A config from an ancestor
package names its modules as that package's files, `//<package>:<file>`.
`//tests/vitest/config_srcs` is the example.

Gazelle writes `config` from the file plain `vitest` would read -- a
`vitest.config.*`, else a `vite.config.*`; `//tests/vitest/vite_config` is the
example, a `define` the test reads -- beside the tests by name, else the one
in the nearest directory above holding a `package.json`, or the repository
root, as the label `//pkg:vitest_config` of a public `filegroup` it writes over
the file; and `config_srcs` from the config's listing, followed through every
first-party module it reaches ([what Gazelle
writes](../gazelle/overview.md#what-gazelle-writes)).

!!! warning "The array form needs vitest 3.2 or later"
    `test.projects` is the name `test.workspace` was renamed to in vitest 3.2;
    vitest 4 removed the old name and throws on it. Every other `config` shape
    (object, function, promise) uses no version-sensitive key.

### Setup Files

`test.setupFiles` in the `config` names the files that run before every test
file, which is where DOM polyfills (`matchMedia`, `ResizeObserver`,
`PointerEvent`) belong; `test.globalSetup` runs once around the whole run. An
entry is resolved against the root. Once the layers have merged, an entry
ending in `.ts`, `.tsx`, `.mts` or `.cts` whose compiled sibling is in the
runfiles (`.js`, `.mjs` or `.cjs`; `.jsx` for a `.tsx` under `jsx: preserve`)
is rewritten to that sibling, so `setupFiles: ["./test/vitest.setup.ts"]` in a
config at the package root runs `test/vitest.setup.js`. An entry with no
compiled sibling staged is left as written and fails to load: `Cannot find
module '.../test/vitest.setup.ts'`. The `ts_compile` over the source has to be
in `deps`; nothing imports a setup file, so Gazelle writes no such dep and the
entry is `# keep`. `//tests/setup_files_compiled` is the example, with the
config at the package root and beside the tests.

vitest resolves each entry through Node's resolver, which follows the runfiles
link to the compiled file in `bazel-out`; layer 1 gives the module its runfiles
path back as its id, as it does every file the runfiles hold, so a DOM
environment (`jsdom`, `happy-dom`), which loads setup files through Vite's
server, is served a path under the root. `//tests/setup_files_compiled/dom` is
the example.

### `.ts` Specifiers

`import { x } from "./util.ts"` is legal TypeScript under
`allowImportingTsExtensions`, and the emit keeps the specifier as written.
Resolved as written, a same-package import runs the source under vite's
transform, and an import of another package's file fails:

```
Error: Cannot find module './util.ts' imported from .../util.test.js
```

Layer 1 carries a plugin that resolves a specifier ending in `.ts`, `.tsx`,
`.mts` or `.cts` to the compiled file the runfiles hold for it (`.js`, `.mjs`
or `.cjs`; `.jsx` for a `.tsx` under `jsx: preserve`) whenever that file
resolves from the importing file: a relative specifier to the sibling beside
it -- the rule a `setupFiles` entry is rewritten by -- and a bare one, a
subpath into a workspace member (`subpath-member/src/value.ts` from a member
that declares it), to the file under the member's view in the tree ([What a
Workspace Member Is Imported
As](../guides/npm.md#what-a-workspace-member-is-imported-as)). A specifier
with no compiled file resolves as written; the source and the emit are
untouched. `//tests/vitest/relative_ts` and
`//tests/npm:by_name_member_test` are the examples.

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

### Snapshots

vitest resolves a `.snap` beside the test file it ran, which under Bazel is the
compiled `.js` in `bazel-out`. Layer 4 replaces that resolution with the path
the `.ts` source implies, `<package>/__snapshots__/<source file name>.snap`,
the path a plain `vitest` run uses. The `.snap` is a src of the test, as every
other file under the package is ([`srcs`](ts-compile.md#sources)); Gazelle
lists it with the package's files:

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

A snapshot the test cannot read is a failure: `CI=true` puts vitest in
read-only snapshot mode, so no `bazel test` writes a `.snap`. Writing one is
`vitest -u` in the package, as outside Bazel. Commit the result.

### Coverage

`bazel coverage //path/to:my_test` works on any `ts_test` on the vitest runner
with no attribute set; `@vitest/coverage-v8` must be in `node_modules`.

Bazel's `collect_coverage.sh` runs the test with `COVERAGE_DIR` set; the
launcher runs vitest with the lcov reporter and writes the report, its paths
resolved against vitest's root, the config's package, and made
workspace-relative, as `vitest.dat` in that directory. Bazel then runs the
rule's merger, `@rules_typescript//tools/lcov_merger`, over the directory, and
it keeps the records the coverage manifest selects. The merger is the rule's
rather than Bazel's own because the manifest names the `.ts` a target declared
and the report names the `.js` compiled from it, and Bazel's merger keeps a
record only under the manifest's exact spelling; the rule's matches the two by
path. `tools/ci/check_coverage_report.sh` runs the two commands below in CI and
reads the report.

`--instrumentation_filter` selects the targets whose files reach the report.
Bazel derives a default from the targets on the command line; for
`bazel coverage //foo:bar_test` that is `^//foo[/:]`, so a library in another
package is absent from the report until a wider filter names it:

```bash
bazel coverage //tests/vitest/coverage:math_coverage_test --combined_report=lcov
# SF:tests/vitest/coverage/same_package.js only

bazel coverage //tests/vitest/coverage:math_coverage_test --combined_report=lcov \
    --instrumentation_filter='^//tests/vitest[/:]'
# adds SF:tests/vitest/math.js
```

Every `ts_compile` carries its own `InstrumentedFilesInfo`, so the filter is
applied per target. The test's own files are a test target's, which Bazel
leaves out of the report unless `--instrument_test_targets` is set. Coverage is
reported against the compiled `.js` in `bazel-out`, so `SF:` paths and line
numbers are the compiler's.

A test whose pool runs the tests in a second runtime, such as a
`@cloudflare/vitest-pool-workers` test in workerd, needs istanbul: v8 coverage
is counters read back out of Node's inspector, which workerd has no equivalent
of, and istanbul instruments at transform time. Set
`coverage_provider = "istanbul"` and put `@vitest/coverage-istanbul`, pinned to
the same version as `vitest`, in `deps`. With only the package in `deps` and no
`coverage_provider`, vitest falls back to its v8 default and the run fails with
`MISSING DEPENDENCY  Cannot find dependency '@vitest/coverage-v8'`.

### Finding What a Test Reads

A test that opens a file the runfiles do not hold fails in the sandbox with
`ENOENT`, and nothing in the build says which file: a run-time read is in no
program listing, so Gazelle writes no `data` entry for it.

```bash
bazel run //path/to:my_test -- --reads
```

runs the same compiled tests unsandboxed, from the runfiles tree `bazel test`
runs them in, under a Node `--require` hook that records every path the test
processes open, stat or read. Once vitest has exited it prints, on stdout,
every regular file under the workspace that the run reached outside the
runfiles tree -- once, sorted, as the workspace-relative path and the label a
`data` entry would take:

```
MODULE.bazel	//:MODULE.bazel
tests/vitest/reads_report/fixtures/outside.txt	//tests/vitest/reads_report/fixtures:outside.txt
```

vitest's own output goes to stderr; a test reading nothing outside its runfiles
prints nothing; the exit status is the test's. A test that walks up from its
own directory to a `MODULE.bazel` or a `package.json` leaves the runfiles tree
and finds the workspace's file through the execution root, and the marker it
stopped at is in the report beside the files it then opened: both go in
`data`. What the vitest process itself opens -- a `config`, a `globalSetup`
entry -- is not recorded.

`data` is the owner's attribute: Gazelle writes nothing into it and leaves what
is written. A file another package's test reads needs an `exports_files` in
its package:

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
read undeclared and is `manual`, and run with `--reads` it prints the two lines
above; `reads_declared_test` lists them and prints nothing.

## Environments

The Workers pool is the vitest runner's environment: `wrangler_config`, the
config layer the generated config imports, the `WranglerTestConfig` action,
and the runfiles and symlinks it adds are one file,
`ts/private/actions/workers_pool.bzl`, the only file that names wrangler. The
runner reaches it through the one struct it returns, so a Workers ruleset
takes the file as it is.

### A Workers Pool

A `config` whose `plugins` hold `@cloudflare/vitest-pool-workers` runs the tests
inside workerd. Two things put the compiled worker in front of it. One is
layer 1's: the root is the config's package, so `wrangler.configPath` names the
file beside the config, and every module has one id ([The Generated vitest
Config](#the-generated-vitest-config)) -- the pool resolves vitest's own
modules for workerd by realpath, and a package's file has its realpath as its
id. The other is `wrangler_config`:

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
directory; in a repository that is the source, `src/index.ts`, and the worker
under test is the compiled one. `WranglerTestConfig` copies the file and
patches `main` and every `env.<name>.main` to the compiled entry with wrangler's
`experimental_patchConfig` (`.ts` and `.tsx` to `.js`, `.mts` to `.mjs`, `.cts`
to `.cjs`; a `.js` is left as written). wrangler is the one in the test's
`node_modules` tree. The copy is staged at the source's runfiles path, which is
what `configPath` names and the id a `?raw` import of it gets back.
Comments and every other key survive; the formatting is wrangler's. A config
naming no `main`, or a `.toml` holding `#` comments, fails the action.

A runfiles file at the copy's path wins over it silently, with the unpatched
`main`. The file in `data` as well is an analysis error, `is staged through
wrangler_config; do not list it in data too.`, and a `ts_compile` dep's data
src at that path is dropped from the runfiles. Every other data src of the deps
is in the runfiles, which is what a wrangler `rules` module the worker imports
needs. `//tests/workers_nested` is the example with the config at the package
root; `//tests/workers`, with the config beside the tests and
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

Gazelle writes `wrangler_config` and `coverage_provider` from the config's
edges ([a Workers-pool config](../gazelle/overview.md#a-workers-pool-config)).

## The node:test Runner

A test written against [`node:test`][node-test] registers with node's runner,
not with vitest's collector, so vitest reports `0 test` for the file and fails
it as an empty suite. `runner = "//ts/runners:node_test"` runs such a file
under `node --test`:

```python
ts_test(
    name = "scripts_test",
    srcs = ["cloudflare-account-token.test.ts"],
    runner = "@rules_typescript//ts/runners:node_test",
    deps = [":scripts", "@npm//:types_node"],
)
```

`tsconfig` means on a node:test target what it means above, but a `paths`
alias is type-checking only under node:test: the resolve hook below answers a
relative specifier and a bare one from the tree, and nothing reads the chain's
`paths`. Under vitest an alias resolves at run time
([The Test's tsconfig](#the-tests-tsconfig)).

The package's code runs at its runfiles paths, as under vitest: the launcher
passes `--preserve-symlinks-main`, and the runner target's `node:module`
resolve hook (`ts/private/node_test_hook.mjs`) resolves every specifier from a
file outside the store, one whose path holds no `node_modules/` segment:

- A relative specifier resolves, at the importer's runfiles path, to the file
  the compiled tree holds for it: `./util.ts`, `.tsx`, `.mts` or `.cts` to the
  compiled sibling (`.js`; `.jsx` for a `.tsx` under `jsx: preserve`; `.mjs`;
  `.cjs`), whether or not the source is staged beside it; an extensionless
  `./util` to `util.js` or `util/index.js`, the file bundler resolution named at
  compile time; any other to the file as written. `//tests/node_test` pins the
  three: `:ts_specifier_test`, `:extensionless_test`, `:runfiles_layout_test`.
- A bare specifier resolves through the importer chain, the directories the
  launcher puts on `NODE_PATH` nearest first, never from a `node_modules` the
  walk up from `bazel-out` happens to meet (`:bare_import_test`): the hook
  resolves an `import` from each importer's directory in turn, and a `require`
  reads `NODE_PATH` itself. One ending in `.ts`, `.tsx`, `.mts` or `.cts` -- a
  subpath into a workspace member -- resolves to the compiled file the store
  holds for it, on either route (`//tests/npm:by_name_member_node_test`). Code
  in the store resolves as node resolves it, at its realpath, a `.ts`
  specifier to its compiled form, so a package's edge beside its tree answers
  its imports as installed (`//tests/npm/multi_version:own_edge_node_test`).

The compiled tests run in the module format their tsconfig gives them
([The Module Format](ts-compile.md#the-module-format)). A package whose
`module` is `commonjs`, or `nodenext` with no `type` in its `package.json`,
runs as CommonJS: `__dirname` is the test's runfiles directory, `require` is
node's, a relative `require` resolves through the hook as an `import` does,
and a named import from a CommonJS dependency is that dependency's export.
`//tests/node_test/cjs` pins the four.

Under vitest, [layer 1's plugin](#ts-specifiers) resolves the `.ts` specifier
and the generated config the rest.

node:test takes no config file; it is configured by CLI flags and by the test
file itself. `args` are those flags, placed before `--test` so that the
children `node --test` spawns inherit them: a suite that calls `mock.module`
sets `args = ["--experimental-test-module-mocks"]`, as its package's test
script does (`//tests/node_test:module_mocks_test`). The runner refuses the
vitest attributes, naming the ones set:

```
ts_test @@//scripts:scripts_test: the node:test runner reads none of config,
coverage_provider. Every one of them configures vitest, which this target does
not run. Drop them, or drop `runner` to run the test under vitest.
```

The rejected set is `config`, `config_srcs`, `coverage_provider` and
`wrangler_config`. `bazel coverage` on such a target fails saying it reports
none. `--test_filter` reaches node as
`--test-name-pattern` (a regular expression over test names), and the exit
status is the test result. Nothing writes a JUnit XML on either runner; Bazel
synthesises `test.xml` from the log.

[node-test]: https://nodejs.org/api/test.html

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`, on either runner. Set `shard_count` on the target and pass
`--noincompatible_check_sharding_support`: the runner never touches
`TEST_SHARD_STATUS_FILE`, which is how Bazel expects a test runner to advertise
sharding support, so without that flag a sharded run fails before any test
starts.

## Debugging

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib", "@npm//:vitest"],
    tags = ["manual"],
    env = {"NODE_OPTIONS": "--inspect-brk=9229"},
)
```

`bazel run //path/to:my_test_debug`, then attach with VS Code or
`chrome://inspect`.

## Listing npm Deps

Test sources are checked for undeclared imports like any other `ts_compile`
sources: a module that only some dep's own deps provide fails the build with the
label to add ([Deps have to be direct](ts-compile.md#deps-have-to-be-direct)).

A `ts_compile` dep brings its store files into the runfiles: its compiled JS
value-imports the packages it declared, and `TsInfo.npm_files` carries the
dep's importer links and their store trees, so a test in one package runs
production code from another without repeating its npm deps. `deps` lists
what the test files import, each resolved through the test's own chain
([the chain](node-modules.md#the-chain)).
`bazel run //:gazelle` writes the list from tsgo's listing of the package: the
edges of the test files, the production sources and the declarations, the
vitest config's imports, and the nearest `package.json`'s `dependencies` and
`devDependencies`.
