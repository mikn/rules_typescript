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

`node_modules` is optional: when it is unset and `deps` is a plain list,
`ts_test` builds a per-target `node_modules` tree from every dep that provides
`NpmPackageInfo`, their transitive npm deps, and the npm closure of every
`ts_compile` dep. Pass it explicitly when `deps` is a `select()` (which a macro
cannot iterate) or when you need a tree the deps do not describe.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`/`.tsx` test files |
| `deps` | `label_list` | `[]` | `ts_compile` and `@npm//` targets the tests import |
| `node_modules` | `label` | auto | Explicit `node_modules` target; skips auto-generation entirely |
| `npm_workspace_name` | `string` | `"npm"` | Informational only; the auto tree is built by detecting `NpmPackageInfo`, not by matching label strings |
| `vitest` | `label` | `None` | Explicit vitest binary label (found in `node_modules` when absent) |
| `runtime` | `label` | `None` | Per-target JS runtime binary override |
| `env` | `string_dict` | `{}` | Extra environment variables for the runner |
| `size` | `string` | `"medium"` | Bazel test size |
| `timeout` | `string` | `None` | Bazel test timeout |
| `tags` | `string_list` | `[]` | Bazel tags |
| `visibility` | `string_list` | `None` | Visibility of the test and of the generated `ts_compile` targets; see [Generated targets](#generated-targets) |
| `target` | `string` | `"es2022"` | ECMAScript target for the internal `ts_compile` |
| `jsx_mode` | `string` | `"react-jsx"` | JSX mode for the internal `ts_compile` |
| `declarations` | `string` | `"tsgo"` | Declaration emitter for the internal `ts_compile`, `"tsgo"` or `"oxc"` |
| `lib` | `string_list` | `None` | `lib` set for the internal `ts_compile`. A worker test needs it: `webworker` is in no set `target` implies |
| `types` | `string_list` | `None` | Ambient type packages for the internal `ts_compile`; see [Ambient type packages](#ambient-type-packages) |
| `compiler_options` | `dict` | `None` | Anything else for the internal `ts_compile` |
| `tsconfig` | `label` | `None` | The `compilerOptions` baseline for the internal `ts_compile`; the three above override it |
| `path_aliases` | `string_dict` | `None` | Source-level alias prefixes for the internal `ts_compile`; see [Path aliases](#path-aliases) |
| `path_alias_srcs` | `label_list` | `None` | The files an alias resolves to, when they are not in the test's own `srcs`; see [Path aliases](#path-aliases) |
| `types_srcs` | `label_list` | `None` | The files a relative `types` entry resolves to; see [A `types` entry that names a declaration file](#a-types-entry-that-names-a-declaration-file) |
| `untyped_packages` | `string_list` | `None` | npm packages the generated `ts_compile` targets' type programs leave out; see [Keeping a package out of the program](#keeping-a-package-out-of-the-program) |
| `runner` | `string` | `"vitest"` | Which runner runs the compiled tests, `"vitest"` or `"node:test"`; see [The node:test runner](#the-nodetest-runner). Every attribute below this row except `data` is vitest's, and an analysis error under `"node:test"` |
| `environment` | `string` | `""` | `test.environment`: `node`, `jsdom`, `happy-dom`, `edge-runtime`, or any custom vitest environment package. The package must be in `deps` |
| `coverage` | `bool` | `False` | Also instrument during plain `bazel test`. `bazel coverage` works on every vitest target regardless |
| `config` | `label` or `dict` | `None` | A vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`) or an inline dict, **merged** into the generated config; see [A config file](#a-config-file) |
| `wrangler_config` | `label` | `None` | The wrangler config a Workers-pool `config` names through `wrangler.configPath`. A copy whose `main` names the compiled entry is staged at the file's own runfiles path; the file is not also in `data`. See [A Workers pool](#a-workers-pool) |
| `setup_files` | `label_list` | `[]` | `test.setupFiles`. `.ts`/`.tsx` entries are compiled with the same `deps` as the tests |
| `global_setup` | `label_list` | `[]` | `test.globalSetup`; compiled like `setup_files` |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, and files a `config` or setup entry imports |
| `globals` | `bool` | `False` | `test.globals`: global `describe`/`it`/`expect`, and the `types` entry that declares them; see [Globals](#globals) |
| `reporters` | `string_list` | `[]` | `test.reporters`, e.g. `["default", "junit"]` |
| `coverage_thresholds` | `string_dict` | `{}` | `test.coverage.thresholds`, e.g. `{"lines": "80", "perFile": "true"}`. Values that look numeric or boolean are emitted as such |
| `coverage_provider` | `string` | `""` | `test.coverage.provider`: `"v8"` (vitest's default) or `"istanbul"`; see [Coverage](#coverage) |
| `snapshots` | `label_list` | `[]` | Checked-in `.snap` files, normally `glob(["__snapshots__/*.snap"])`; see [Snapshots](#snapshots) |
| `update_snapshots` | `bool` | `False` | Makes **this** target the executable updater; see [Snapshots](#snapshots) |

## Generated Targets

The macro generates a `ts_compile` target for `srcs`, one for the TypeScript
entries in `setup_files` and one for those in `global_setup`, plus, on the
vitest runner, a `<name>.update_snapshots` executable.
The `ts_compile` targets take the test's `visibility`, defaulting to
`//visibility:public` when the test declares none, so an IDE tsconfig written by
`ts_refresh_tsconfig` can name them.

## Ambient Type Packages

A `types` entry may name an `exports` subpath, as in
`@cloudflare/vitest-pool-workers/types`. Nothing imports an ambient module a
package ships behind such a subpath, and there is no `node_modules` for a
tsconfig `types` entry to resolve through, so the subpath is resolved from the
manifest and the file put in the program's `files`.

## Globals

`globals = True` sets `test.globals`: vitest installs `describe`, `it`, `expect`
and the rest as globals, and a test file imports nothing. vitest publishes their
declarations behind its `vitest/globals` subpath, and the attribute adds that
entry to the internal `ts_compile`'s `types` as well. Without the entry the
test runs and does not compile: `TS2593` on `describe`, `TS2304` on `expect`.

The entry is resolved from the target's own `deps`, so `globals = True` needs
vitest listed there, and says so at analysis time when it is not:

```
ts_compile: compilerOptions.types entry "vitest/globals" on
@@//path:_my_test_compile resolves to nothing.
```

The runner finds vitest in the `node_modules` tree, which the `node_modules`
attr can supply on its own; the guard resolves the entry from `deps`. A
`globals = True` test that supplies vitest only through that attr has to list
it in `deps` as well. The injected entry is folded in last, after anything the
target wrote.

Under Gazelle the dep needs `# keep`: nothing in a `globals = True` test imports
vitest, and `deps` is a managed attribute, so a run rewrites the list without
the entry. `globals = True` itself survives; no directive writes it:

```python
ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    globals = True,
    deps = ["@npm//:vitest"],  # keep
)
```

## Path Aliases

`path_aliases` and `path_alias_srcs` are forwarded to the generated
`ts_compile`, with the meaning they have there. A package whose `ts_compile`
needs an alias needs it on the test too: the test files are a program of their
own, and `paths` is a key the `tsconfig` layer cannot contribute.

`ts_compile` accepts an alias only when a file it stages sits under the alias
directory; otherwise the alias is an analysis error. A test with a src under
that directory validates the alias on that src, and the aliased declarations
arrive on the dep edge. A test with none needs `path_alias_srcs` naming what the
alias resolves to: a `ts_compile` target, whose declarations land in the
bazel-bin mirror of the directory, or the files themselves, which join the
test's type program and are checked again there. Where the aliased target can
carry a `module_name`, depending on it is cheaper.

```starlark
ts_test(
    name = "app_test",
    srcs = ["app.test.ts"],
    path_aliases = {"@shared/": "packages/app/shared/"},
    path_alias_srcs = ["//packages/app/shared"],
    deps = [":app", "//packages/app/shared", "@npm//:vitest"],
)
```

Gazelle writes both. `path_aliases` carries the aliases the test files import
through; `path_alias_srcs` is written only when no src of the test sits under
the alias directory, and names the target the aliased import resolved to. Both
attributes are Gazelle's, so a value it did not derive survives the next run
only with a `# keep` on its line; see
[attributes Gazelle owns](../gazelle/directives.md#attributes-gazelle-owns).

An alias is type-checking only. oxc leaves an import specifier alone, so a
compiled test still names the alias at runtime, where vitest resolves it as a
package and fails with `Cannot find package`. A type-only import is erased and
unaffected. A value import through an alias needs the module reachable at
runtime: depend on the target that produces it.

## A `types` Entry That Names a Declaration File

An entry starting `./` or `../` names a file, not a package, and resolves
against the source files the type-check action stages. The test files are the
only `srcs` the generated `ts_compile` has, so a declaration file in another
package (the `worker-configuration.d.ts` a wrangler project keeps beside its
worker) has to be staged by something else. A dep whose own `srcs` hold it
does: a `.d.ts` in `srcs` is passed through and sits at the path the entry
names. `types_srcs` stages it without the dep edge, so the test does not also
consume that target's declarations:

```starlark
ts_test(
    name = "handler_test",
    srcs = ["handler.test.ts"],
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//worker:worker_types"],
    deps = [":src", "@npm//:vitest"],
)
```

Both attributes carry the meaning they carry on
[`ts_compile`](ts-compile.md#a-types-entry-that-names-a-declaration-file): an
entry no staged file sits at is an analysis error, and a `types_srcs` file no
entry names is one too. A declaration a `deps` entry already stages needs no
label here.

## Keeping a Package Out of the Program

`untyped_packages` is forwarded to every `ts_compile` the macro generates, the
one over `srcs` and the ones over the TypeScript entries of `setup_files` and
`global_setup`, with the meaning it has on
[`ts_compile`](ts-compile.md#keeping-a-package-out-of-the-program): a named
package gets no `paths` key and no `files` entry in that compile's tsconfig,
stays in `deps`, and no JavaScript moves.

The test files are a program of their own, and its `paths` map covers the npm
closure of every dep. A global-script package one first-party dep reaches
through its own npm closure leaks into the test program exactly as into a
library's, once a declaration file in the program imports it, and merges its
declarations ahead of anything the test's `types` entries put there: a worker
test whose `types` names the wrangler-generated `worker-configuration.d.ts`
sees `@cloudflare/workers-types`' `Headers` and `Cloudflare.Env` instead when
some dep of the worker depends on that package.

```starlark
ts_test(
    name = "handler_test",
    srcs = glob(["*.test.ts"]),
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//worker:worker_types"],
    untyped_packages = ["@cloudflare/workers-types"],
    deps = [":src", "@npm//:vitest"],
)
```

The two refusals hold as on `ts_compile`: an entry naming no package in the
test's closure, and a package named in both `untyped_packages` and `types`.

## The Generated vitest Config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It is an entry config
that layers four sources, lowest precedence first:

| Layer | Contents | Workspace projects |
|-------|----------|---|
| 1. Bazel | `root` (the `config`'s package; the test's own with an inline dict or none), `cacheDir` under `TEST_TMPDIR`, `resolve.preserveSymlinks`, `test.coverage.allowExternal`, the CSS-module plugin when a dep carries a `*.module.css`, and under a `config` whose `plugins` hold `@cloudflare/vitest-pool-workers` `preserveSymlinks: false` and the runfiles-imports plugin | yes |
| 2. user | the `config` attr: a config file or an inline dict | it supplies the projects |
| 3. attributes | `environment`, `setup_files`, `global_setup`, `globals`, `reporters`, `coverage_thresholds`, `coverage_provider` | yes |
| 4. snapshots | `test.resolveSnapshotPath`, and in update mode `test.dir`, `test.include` and `cacheDir` | no, root only |

Objects merge key by key; arrays concatenate base-first, matching vite's own
`mergeConfig`. A user `plugins` list therefore never displaces the CSS-module
plugin, and a user `setupFiles` list never displaces `setup_files`: the
attribute's entries run after the config's. Scalars from a later layer win, so
`environment` overrides an environment set inside `config`. Once the layers have
merged, a `setupFiles` or `globalSetup` entry naming a TypeScript source is
rewritten to its compiled sibling; see [Setup Files](#setup-files).

Layer 4 is root-only because `resolveSnapshotPath` is one of vitest's
non-project options: it is applied once, to the root config, and never merged
into a project.

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
runfiles tree (`NODE_PATH`, set by the launcher) and coverage output paths
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
`test.projects`; each project in it receives the Bazel layer and the attribute
layer too, because every project gets its own Vite server. Anything the config
imports relatively must be in `data`; it is not a build input otherwise.

Vite's root is the config's package, so a relative path in the config names the
directory the file sits in, as under plain `vitest`, whether the test is in that
package or one below it; with an inline dict or no config it is the test's
package. The config file itself is staged beside the `node_modules` tree, where
its bare imports resolve, so a path relative to the config file is a different
path. `TS_TEST_PACKAGE_DIR` holds the test's package directory, which the root
is resolved from, for a path that has to be absolute.

!!! warning "The array form needs vitest 3.2 or later"
    `test.projects` is the name `test.workspace` was renamed to in vitest 3.2;
    vitest 4 removed the old name and throws on it. The
    generated config emits `test.projects`, so a `config` that default-exports
    an array needs vitest 3.2 or later. Every other `config` shape (object,
    function, promise, inline dict) uses no version-sensitive key.

### An Inline Dict

```python
ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    config = {"test": {"testTimeout": 30000, "retry": 2}},
)
```

The dict occupies the same layer as a config file; pass one or the other.

### Setup Files

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    deps = [":button", "@npm//:react", "@npm//:happy-dom", "@npm//:vitest"],
    environment = "happy-dom",
    setup_files = ["setupTests.ts"],
)
```

`setup_files` entries run before every test file, which is where DOM polyfills
(`matchMedia`, `ResizeObserver`, `PointerEvent`) belong. They run in the order
listed: TypeScript entries first, compiled by the macro with the same `deps` as
the tests, then `.js`/`.mjs`/`.cjs` entries, which are passed through. All of
them run after any `setupFiles` the `config` attr contributes.
`global_setup` is `test.globalSetup`, which runs once around the whole run.

A `test.setupFiles` or `test.globalSetup` entry inside the `config` is resolved
against the root and loaded as written, and the runfiles hold no TypeScript
source: what a `deps` entry stages at that path is the compiled sibling. Once
the layers have merged, an entry ending in `.ts`, `.tsx`, `.mts` or `.cts` whose
file is absent while the `.js`, `.mjs` or `.cjs` beside it exists is
rewritten to that sibling, so `setupFiles: ["./test/vitest.setup.ts"]` in a
config at the package root runs `test/vitest.setup.js`; left as written, the
run fails with `Cannot find module '.../test/vitest.setup.ts'`. The `ts_compile`
over the source has to be in `deps`, and nothing imports a setup file, so
Gazelle writes no such dep: the entry is `# keep`. `//tests/setup_files_compiled`
is the example, with the config at the package root and beside the tests.

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
    lib = ["esnext", "webworker"],
    types = ["@cloudflare/vitest-pool-workers/types"],
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
directory. In a repository that is the source, `src/index.ts`, and the runfiles
do not hold it: `Cannot find module '.../src/index.ts' imported from
cloudflare:test-...`. A build action, `WranglerTestConfig`, copies the file and
patches `main` and every `env.<name>.main` to the compiled entry with wrangler's
`experimental_patchConfig` (`.ts` and `.tsx` to `.js`, `.mts` to `.mjs`, `.cts`
to `.cjs`; a `.js` is left as written). wrangler is the one in the test's
`node_modules` tree, resolved from the pool package, so the copy is patched by
the reader that parses it. The copy is staged at the source's runfiles path,
which is what `configPath` names, and under its own name beside the generated
config, which is what admits its realpath when a `?raw` import of the config
re-resolves it. Comments and every other key survive; the formatting is
wrangler's. A config naming no `main`, or a `.toml` holding `#` comments, fails
the action.

A runfiles file at the copy's path wins over it silently, with the unpatched
`main`. The file in `data` as well is an analysis error, `is staged through
wrangler_config; do not list it in data too.`, and the `asset_library` Gazelle
writes over the file is dropped from the runfiles when it is among the `deps`.
Every other `AssetInfo` file of the deps is in the runfiles, which is what a
wrangler `rules` module the worker imports needs. `//tests/workers_nested` is the
example; `//tests/workers`, with the config beside the tests and
`main: "src/index.js"` in `data`, is the same-package one.

What else a wrangler config names, and where each comes from under `ts_test`:

| Key | Source |
|---|---|
| `main`, `env.<name>.main` | the compiled entry, through the patched copy |
| `rules` modules (`**/*.txt`, `**/*.md`, ...) | an `asset_library` dep of the worker's `ts_compile` |
| `assets.directory` | its contents in `data`, at the same path relative to the config |
| `.dev.vars`, `.dev.vars.<env>` | read beside the config; in `data` when a test needs one |
| `compatibility_date`, `compatibility_flags`, `vars`, `kv_namespaces`, `r2_buckets`, `services`, `durable_objects`, `migrations` | inline values; miniflare emulates the bindings and nothing is staged |
| `durable_objects[].script_name` naming another worker | `miniflare.workers` in the config, as under plain `vitest` |
| `tsconfig`, `alias`, `define`, `no_bundle`, `build` | esbuild and deploy keys; the pool runs none of them |

## Coverage

`bazel coverage //path/to:my_test` works on any `ts_test` on the vitest runner
with no attribute set; `@vitest/coverage-v8` must be in `node_modules`.
`coverage = True` additionally instruments plain `bazel test` runs. A
`runner = "node:test"` target reports no coverage and says so.

`coverage_thresholds` is enforced only when coverage runs, and a run that misses
one fails: vitest exits non-zero with
`ERROR: Coverage for lines (50%) does not meet global threshold (90%)` after the
assertions themselves have passed. `//tests/vitest/thresholds` pins both
directions.

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

Every target under test carries its own `InstrumentedFilesInfo`: the libraries
in `deps`, and the `ts_compile` the macro builds the test sources with. The
filter is applied per target.

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

## The node:test Runner

A test written against [`node:test`][node-test] registers with node's runner,
not with vitest's collector, so vitest reports `0 test` for the file and fails
it as an empty suite. `runner = "node:test"` runs such a file under
`node --test` instead:

```python
ts_test(
    name = "scripts_test",
    srcs = ["cloudflare-account-token.test.ts"],
    runner = "node:test",
    deps = [":scripts", "@npm//:types_node"],
)
```

The compile attributes carry over unchanged: `lib`, `types`,
`compiler_options`, `tsconfig`, `path_aliases`, `path_alias_srcs`,
`types_srcs` and `untyped_packages` mean on a node:test target what they mean
above. An alias is type-checking only on either runner; see
[Path aliases](#path-aliases).

node:test takes no config file; it is configured by CLI flags and by the test
file itself. Every vitest attribute is an analysis error under it, naming the
ones set:

```
ts_test @@//scripts:scripts_test: runner "node:test" reads none of environment,
globals. Every one of them configures vitest, which this target does not run.
Drop them, or drop `runner` to run the test under vitest.
```

The rejected set is `config`, `coverage`, `coverage_provider`,
`coverage_thresholds`, `environment`, `global_setup`, `globals`, `reporters`,
`setup_files`, `snapshots`, `update_snapshots` and `vitest`. node:test has no
globals mode: nothing installs `describe` or `expect`, so `globals` is
rejected, and the `vitest/globals` `types` entry is not added. A dep providing
`CssModuleInfo` is rejected too: only the vitest runner installs the transform
that answers a `*.module.css` import. No `<name>.update_snapshots` target is
generated.

`--test_filter` reaches node as `--test-name-pattern` (a regular expression
over test names), sharding works as above, and the exit status is the test
result. Nothing writes a JUnit XML on either runner; Bazel synthesises
`test.xml` from the log.

### Relative `.ts` Specifiers

`import { x } from "./util.ts"` is legal TypeScript under
`allowImportingTsExtensions`, and oxc copies that specifier into the `.js`
verbatim. Only `util.js` is in the runfiles tree, so the runtime fails with

```
Error [ERR_MODULE_NOT_FOUND]: Cannot find module '.../util.ts'
```

The node:test runner installs an ESM resolver hook that retries a failed
relative resolution with `.ts`/`.tsx` rewritten to `.js`. It runs only after a
failed resolution, so a specifier node can resolve keeps resolving as before.
The source and the emit are unchanged: no `rewriteRelativeImportExtensions`
flag, and no edit to the `import`.

[node-test]: https://nodejs.org/api/test.html

## Snapshots

Vitest resolves a `.snap` beside the test file it ran, which under Bazel is the
compiled `.js` in `bazel-out`. `ts_test` replaces that resolution with the path
the `.ts` source implies:

```
<package>/__snapshots__/<source file name>.snap
```

the path a plain `vitest` run uses.

The `snapshots` attr puts the files in the sandbox:

```python
ts_test(
    name = "widget_test",
    srcs = ["widget.test.ts"],
    snapshots = glob(["__snapshots__/*.snap"]),
    deps = [":widget", "@npm//:vitest"],
)
```

Without it the test cannot read the snapshot and fails. `ts_test` runs vitest in
read-only snapshot mode (`CI=true`), so no `bazel test` writes a `.snap`.
`env = {"CI": "false"}` opts out.

Every vitest `ts_test` declares an executable that writes them:

```bash
bazel run //path/to:widget_test.update_snapshots
```

It reuses the test's own compiled sources and writes under
`BUILD_WORKSPACE_DIRECTORY`, into the checkout next to the `.ts` file. Commit
the result. `--sandbox_writable_path` is not involved.

`update_snapshots = True` on a `ts_test` makes that target the updater and not a
test, for an updater that stands alone. It compiles `srcs` itself, so it cannot
share a package with a `ts_test` over the same files: two `ts_compile` targets
would declare the same `.js` outputs. The generated `<name>.update_snapshots`
shares the test's compile target.

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

A `ts_compile` dep brings its npm closure into the auto-generated tree: its
compiled JS value-imports the packages it declared, and `TsDeclarationInfo`
carries that closure (`transitive_npm_packages`) beside the declarations, so a
test in one package runs production code from another without repeating its npm
deps. `deps` lists what the test files import; where the closure resolves a name
more than one way, the test's own dep is the resolution that sits flat.
`bazel run //:gazelle` writes the list, collecting imports from the test files
and the production sources in the package.

The tree keys each resolution apart by name, version and peer set wherever one
name resolved more than once; see [the layout](node-modules.md#the-layout).

See [Testing with vitest](../guides/testing.md) for the full guide including
watch mode and build feedback.
