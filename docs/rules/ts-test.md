# ts_test

Compiles TypeScript test files and runs them with vitest inside the Bazel sandbox.

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
`NpmPackageInfo`, plus their transitive npm deps. Pass it explicitly when `deps`
is a `select()` (which a macro cannot iterate) or when you need a tree the deps
do not describe.

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
| `visibility` | `string_list` | `None` | Visibility of the test and of the generated `ts_compile` targets — see [Generated targets](#generated-targets) |
| `target` | `string` | `"es2022"` | ECMAScript target for the internal `ts_compile` |
| `jsx_mode` | `string` | `"react-jsx"` | JSX mode for the internal `ts_compile` |
| `declarations` | `string` | `"tsgo"` | Declaration emitter for the internal `ts_compile`, `"tsgo"` or `"oxc"` |
| `lib` | `string_list` | `None` | `lib` set for the internal `ts_compile`. A worker test needs it: `webworker` is in no set `target` implies |
| `types` | `string_list` | `None` | Ambient type packages for the internal `ts_compile` — see [Ambient type packages](#ambient-type-packages) |
| `compiler_options` | `string_dict` | `None` | Anything else for the internal `ts_compile` |
| `tsconfig` | `label` | `None` | The `compilerOptions` baseline for the internal `ts_compile`; the three above override it |
| `path_aliases` | `string_dict` | `None` | Source-level alias prefixes for the internal `ts_compile` — see [Path aliases](#path-aliases) |
| `path_alias_srcs` | `label_list` | `None` | The files an alias resolves to, when they are not in the test's own `srcs` — see [Path aliases](#path-aliases) |
| `environment` | `string` | `""` | `test.environment` — `node`, `jsdom`, `happy-dom`, `edge-runtime`, or any custom vitest environment package. The package must be in `deps` |
| `coverage` | `bool` | `False` | Also instrument during plain `bazel test`. `bazel coverage` works on every target regardless |
| `config` | `label` or `dict` | `None` | A vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`) or an inline dict, **merged** into the generated config — see [A config file](#a-config-file) |
| `setup_files` | `label_list` | `[]` | `test.setupFiles`. `.ts`/`.tsx` entries are compiled with the same `deps` as the tests |
| `global_setup` | `label_list` | `[]` | `test.globalSetup`; compiled like `setup_files` |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, and files a `config` or setup entry imports |
| `globals` | `bool` | `False` | `test.globals` — global `describe`/`it`/`expect` |
| `reporters` | `string_list` | `[]` | `test.reporters`, e.g. `["default", "junit"]` |
| `coverage_thresholds` | `string_dict` | `{}` | `test.coverage.thresholds`, e.g. `{"lines": "80", "perFile": "true"}`. Values that look numeric or boolean are emitted as such |
| `coverage_provider` | `string` | `""` | `test.coverage.provider`: `"v8"` (vitest's default) or `"istanbul"` — see [Coverage](#coverage) |
| `snapshots` | `label_list` | `[]` | Checked-in `.snap` files, normally `glob(["__snapshots__/*.snap"])` — see [Snapshots](#snapshots) |
| `update_snapshots` | `bool` | `False` | Makes **this** target the executable updater — see [Snapshots](#snapshots) |

## Generated Targets

The macro generates a `ts_compile` target for `srcs`, one for the TypeScript
entries in `setup_files` and one for those in `global_setup`, plus a
`<name>.update_snapshots` executable.
The `ts_compile` targets take the test's `visibility`, defaulting to
`//visibility:public` when the test declares none, so an IDE tsconfig written by
`ts_refresh_tsconfig` can name them.

## Ambient Type Packages

A `types` entry may name an `exports` subpath, as in
`@cloudflare/vitest-pool-workers/types`. That is how an ambient module a package
ships behind such a subpath reaches the program: nothing imports the declaration,
and a tsconfig `types` entry resolves through a `node_modules` this ruleset does
not have, so the subpath is resolved from the manifest and the file put in the
program's `files`.

## Path Aliases

`path_aliases` and `path_alias_srcs` are forwarded to the generated
`ts_compile`, and carry the meaning they carry there. A package whose
`ts_compile` needs an alias needs it on the test too: the test files are a
program of their own, and `paths` is one key the `tsconfig` layer cannot
contribute to, so an alias set on the package target reaches nothing the test
compiles.

An alias into the code under test lives outside the test's `srcs`, so
`path_alias_srcs` has to name the files it resolves to — without them the alias
is an analysis error, the same one `ts_compile` raises. Those files join the
test's type program, so tsgo checks them here as well as in the target that owns
them; where the aliased target can carry a `module_name`, depending on it is the
cheaper boundary.

```starlark
ts_test(
    name = "app_test",
    srcs = ["app.test.ts"],
    path_aliases = {"@shared/": "packages/app/shared/"},
    path_alias_srcs = ["//packages/app/shared:sources"],
    deps = [":app", "@npm//:vitest"],
)
```

**Type-checking only.** oxc leaves an import specifier alone, so a compiled test
still names the alias at runtime, where vitest resolves it as a package and
fails with `Cannot find package`. A type-only import is erased and is
unaffected; a value import through an alias also needs the module reachable at
runtime, which today means depending on the target that produces it instead of
aliasing into its sources.

## The Generated vitest Config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It is an entry config
that layers four sources, lowest precedence first:

| Layer | Contents | Applies to workspace projects too? |
|-------|----------|---|
| 1. Bazel | `resolve.preserveSymlinks`, `test.coverage.allowExternal`, and the CSS-module plugin when a dep carries a `*.module.css` | yes |
| 2. user | the `config` attr: a config file or an inline dict | it supplies the projects |
| 3. attributes | `environment`, `setup_files`, `global_setup`, `globals`, `reporters`, `coverage_thresholds`, `coverage_provider` | yes |
| 4. snapshots | `test.resolveSnapshotPath`, and in update mode `test.dir`, `test.include` and `cacheDir` | no — root only |

Objects merge key by key; arrays concatenate base-first, matching vite's own
`mergeConfig`. A user `plugins` list therefore never displaces the CSS-module
plugin, and a user `setupFiles` list never displaces `setup_files`: the
attribute's entries run after the config's. Scalars from a later layer win, so
`environment` overrides an environment set inside `config`.

Layer 4 is root-only because `resolveSnapshotPath` is one of vitest's
non-project options: it is applied once, to the root config, and never merged
into a project.

`preserveSymlinks` in layer 1 is a default, not a contract. A DOM environment
resolves every module id to its realpath, which for a runfiles symlink walks
straight out of the test sandbox, so layer 1 turns it on. A pool that resolves
modules for a second runtime needs the opposite: under
`@cloudflare/vitest-pool-workers` a lexical path is a second module identity for
the same file, so a Workers config sets `resolve.preserveSymlinks: false` and the
user layer wins. Leaving it out fails as
`Cannot read properties of undefined (reading 'config')` from inside the pool
runner. `//tests/workers` is the worked example.

Two things sit outside the layering and outrank all of it, being the sandbox
contract: npm resolution into the runfiles tree (`NODE_PATH`, set by the
launcher) and coverage output paths (vitest CLI flags, so `bazel coverage` writes
lcov where Bazel expects it).

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

Vite's root is the package, so a relative path in the config names the package
and not the working directory the test runs in. The config file itself is staged
elsewhere -- beside the `node_modules` tree, where its own bare imports resolve
-- so a path relative to the config file is not the same thing;
`TS_TEST_PACKAGE_DIR` holds the same directory Vite is rooted at, for a path that
has to be absolute.

!!! warning "The array form needs vitest 3.2 or later"
    `test.projects` is the name `test.workspace` was renamed to in vitest 3.2;
    vitest 4 removed the old name and throws on it. The
    generated config emits `test.projects`, so a `config` that default-exports
    an array needs vitest 3.2 or later. Every other `config` shape works on any
    vitest 3 or 4: object, function, promise, inline dict.

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

## Coverage

`bazel coverage //path/to:my_test` works on any `ts_test` with no attribute set;
`@vitest/coverage-v8` must be in `node_modules`. `coverage = True` additionally
instruments plain `bazel test` runs.

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

Every target under test carries its own `InstrumentedFilesInfo`, because the
filter is applied where a target answers for its own label: the libraries in
`deps`, and the `ts_compile` the macro builds the test sources with.

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

Omitting `coverage_provider` does not report zeros: with only
`@vitest/coverage-istanbul` in `deps`, vitest falls back to its v8 default and
the run fails with
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
`TEST_TOTAL_SHARDS`. Set `shard_count` on the target and pass
`--noincompatible_check_sharding_support`: the runner never touches
`TEST_SHARD_STATUS_FILE`, which is how Bazel expects a test runner to advertise
sharding support, so without that flag a sharded run fails before any test
starts.

## Snapshots

Vitest resolves a `.snap` beside the test file it ran, which under Bazel is the
compiled `.js` in `bazel-out`. `ts_test` replaces that resolution with the path
the `.ts` source implies:

```
<package>/__snapshots__/<source file name>.snap
```

which is where a plain `vitest` run already keeps it. A repository adopting
`ts_test` renames nothing.

Reading them takes the `snapshots` attr, which is what puts the files in the
sandbox:

```python
ts_test(
    name = "widget_test",
    srcs = ["widget.test.ts"],
    snapshots = glob(["__snapshots__/*.snap"]),
    deps = [":widget", "@npm//:vitest"],
)
```

Without it the test cannot read the snapshot and fails: `ts_test` runs vitest in
read-only snapshot mode (`CI=true`), so no `bazel test` can write a `.snap` and
then pass on what it just wrote. `env = {"CI": "false"}` opts out of that, at the
cost of the guarantee.

Writing them uses the executable every `ts_test` declares alongside itself:

```bash
bazel run //path/to:widget_test.update_snapshots
```

It reuses the test's own compiled sources and writes under
`BUILD_WORKSPACE_DIRECTORY`, into your checkout next to the `.ts` file. Commit
the result. `--sandbox_writable_path` is no longer involved.

`update_snapshots = True` on a `ts_test` makes that target the updater and not a
test. It exists for an updater that has to stand alone. It compiles `srcs`
itself, so it cannot share a package with a `ts_test` over the same files: that
would be two `ts_compile` targets declaring the same `.js` outputs. The generated
`<name>.update_snapshots` shares the test's compile target, and is the normal
route.

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

A `ts_compile` dep contributes none of its own npm dependencies to the
auto-generated tree, so `deps` must list every npm package needed at runtime, by
the test files and by the production code under test, the way `go_test` requires
all direct imports. `bazel run //:gazelle` writes that list, collecting imports
from the test files and the production sources in the package.

The tree keys each resolution apart by name, version and peer set wherever one
name resolved more than once; see [the layout](node-modules.md#the-layout).

See [Testing with vitest](../guides/testing.md) for the full guide including
watch mode and build feedback.
