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
`NpmPackageInfo`, plus their transitive npm deps. Pass it explicitly only when
`deps` is a `select()` (which a macro cannot iterate) or when you need a tree
the deps do not describe.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.ts`/`.tsx` test files |
| `deps` | `label_list` | `[]` | `ts_compile` and `@npm//` targets the tests import |
| `node_modules` | `label` | auto | Explicit `node_modules` target; skips auto-generation entirely |
| `npm_workspace_name` | `string` | `"npm"` | Informational only. The auto `node_modules` tree is built by detecting `NpmPackageInfo`, not by matching label strings, so a non-default npm repo name needs nothing here |
| `vitest` | `label` | `None` | Explicit vitest binary label (found in `node_modules` when absent) |
| `runtime` | `label` | `None` | Per-target JS runtime binary override |
| `env` | `string_dict` | `{}` | Extra environment variables for the runner |
| `size` | `string` | `"medium"` | Bazel test size |
| `timeout` | `string` | `None` | Bazel test timeout |
| `tags` | `string_list` | `[]` | Bazel tags |
| `visibility` | `string_list` | `None` | Visibility of the test **and** of the `ts_compile` targets the macro generates from `srcs`/`setup_files`/`global_setup`. Those default to `//visibility:public` so an `ts_refresh_tsconfig` can name them |
| `target` | `string` | `"es2022"` | ECMAScript target for the internal `ts_compile` |
| `jsx_mode` | `string` | `"react-jsx"` | JSX mode for the internal `ts_compile` |
| `declarations` | `string` | `"tsgo"` | Declaration emitter for the internal `ts_compile` |
| `lib` | `string_list` | `None` | `lib` set for the internal `ts_compile`. A worker test needs it: `webworker` is in no set `target` implies |
| `types` | `string_list` | `None` | Ambient type packages for the internal `ts_compile`. An entry may name an `exports` subpath (`@cloudflare/vitest-pool-workers/types`), which is how an ambient module a package ships behind one reaches the program — nothing imports such a declaration, and tsconfig `types` resolves through a `node_modules` this ruleset does not have, so the subpath is resolved from the manifest and the file put in `files` |
| `compiler_options` | `string_dict` | `None` | Anything else for the internal `ts_compile` |
| `environment` | `string` | `""` | `test.environment` — `node`, `jsdom`, `happy-dom`, `edge-runtime`, or any custom vitest environment package. The package must be in `deps` |
| `coverage` | `bool` | `False` | Also instrument during plain `bazel test`. `bazel coverage` works on every target regardless |
| `config` | `label` or `dict` | `None` | A vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`) or an inline dict. **Merged**, not substituted. A file is staged beside the generated `node_modules` tree so its own bare imports resolve, so a path it needs to name comes from the `TS_TEST_PACKAGE_DIR` environment variable rather than from its own location |
| `setup_files` | `label_list` | `[]` | `test.setupFiles`. `.ts`/`.tsx` entries are compiled with the same `deps` as the tests |
| `global_setup` | `label_list` | `[]` | `test.globalSetup`; compiled like `setup_files` |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, and files a `config` or setup entry imports |
| `globals` | `bool` | `False` | `test.globals` — global `describe`/`it`/`expect` |
| `reporters` | `string_list` | `[]` | `test.reporters`, e.g. `["default", "junit"]` |
| `coverage_thresholds` | `string_dict` | `{}` | `test.coverage.thresholds`, e.g. `{"lines": "80", "perFile": "true"}`. Values that look numeric or boolean are emitted as such |
| `coverage_provider` | `string` | `""` | `test.coverage.provider`: `"v8"` (vitest's default) or `"istanbul"`. The matching `@vitest/coverage-*` package must be in `deps`. A pool that runs the tests in a second runtime needs `"istanbul"` — see [Coverage](#coverage) |
| `snapshots` | `label_list` | `[]` | Checked-in `.snap` files, normally `glob(["__snapshots__/*.snap"])`. Listing them is what puts them inside the sandbox, and so what makes a stale one fail — see [Snapshots](#snapshots) |
| `update_snapshots` | `bool` | `False` | Makes **this** target the executable updater rather than a test. Rarely needed: every `ts_test` already declares `<name>.update_snapshots` |

## The generated vitest config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It is an *entry* config
that layers four sources, lowest precedence first:

| Layer | Contents | Applies to workspace projects too? |
|-------|----------|---|
| 1. Bazel | `resolve.preserveSymlinks`, `test.coverage.allowExternal`, and the CSS-module mock plugin when a dep provides `CssModuleInfo` | yes |
| 2. user | the `config` attr — a config file or an inline dict | it *is* the projects |
| 3. attributes | `environment`, `setup_files`, `global_setup`, `globals`, `reporters`, `coverage_thresholds`, `coverage_provider` | yes |
| 4. snapshots | `test.resolveSnapshotPath`, and in update mode `test.dir`, `test.include` and `cacheDir` | no — root only |

Objects merge key by key; arrays concatenate base-first, matching vite's own
`mergeConfig`. So a user `plugins` list never displaces the CSS-module mock, and
a user `setupFiles` list never displaces `setup_files` — the attribute's entries
run after the config's. Scalars from a later layer win, which is why
`environment` overrides an environment set inside `config`.

Layer 4 is root-only because `resolveSnapshotPath` is one of vitest's
non-project options; it is applied once, to the root config, and never merged
into a project. `preserveSymlinks` in layer 1 is the default rather than
the contract: a DOM environment resolves every module id to its realpath, which
for a runfiles symlink walks straight out of the test sandbox, so layer 1 turns
it on. A pool that resolves modules for a second runtime needs the opposite —
under `@cloudflare/vitest-pool-workers` a lexical path is a second module
identity for the same file — so a Workers config sets
`resolve.preserveSymlinks: false` and the user layer wins. Leaving it out fails
as `Cannot read properties of undefined (reading 'config')` from inside the pool
runner; `//tests/workers` is the worked example.

Two things sit outside the layering and outrank all of it, because they are the
sandbox contract rather than configuration: npm resolution into the runfiles
tree (`NODE_PATH`, set by the launcher) and coverage output paths (vitest CLI
flags, so `bazel coverage` writes lcov where Bazel expects it).

To see what the launcher resolved — the node binary, the vitest entry, the
`node_modules` tree, the shard split:

```bash
bazel run //path/to:my_test -- --dump-config
```

Read the config that actually ran:

```bash
bazel build //path/to:my_test --output_groups=vitest_config
```

### A config file

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
imports relatively must be in `data` — it is not a build input otherwise.

!!! warning "The array form needs vitest 3.2 or later"
    `test.projects` is the name `test.workspace` was renamed to in vitest 3.2;
    vitest 4 removed the old name and throws on it rather than ignoring it. The
    generated config emits `test.projects`, so a `config` that default-exports
    an array needs vitest 3.2 or later. Every other `config` shape works on any
    vitest 3 or 4 — object, function, promise, inline dict.

### An inline dict

```python
ts_test(
    name = "math_test",
    srcs = ["math.test.ts"],
    deps = [":math", "@npm//:vitest"],
    config = {"test": {"testTimeout": 30000, "retry": 2}},
)
```

The dict occupies the same layer as a config file; pass one or the other.

### Setup files

```python
ts_test(
    name = "component_test",
    srcs = ["Button.test.tsx"],
    deps = [":button", "@npm//:react", "@npm//:happy-dom", "@npm//:vitest"],
    environment = "happy-dom",
    setup_files = ["setupTests.ts"],
)
```

`setup_files` entries run before every test file — this is where DOM polyfills
(`matchMedia`, `ResizeObserver`, `PointerEvent`) belong. TypeScript entries are
compiled by the macro with the same `deps` as the tests; `.js`/`.mjs`/`.cjs`
entries are passed through. `global_setup` is the same mechanism for
`test.globalSetup`, which runs once around the whole run.

## Coverage

`bazel coverage //path/to:my_test` works on any `ts_test` with no attribute
set; `@vitest/coverage-v8` must be in `node_modules`. `coverage = True`
additionally instruments plain `bazel test` runs. `coverage_thresholds` is only
enforced when coverage runs — and its enforcement is untested, so treat a
passing build as unproven rather than as evidence the threshold held.

A test whose pool runs the tests in a second runtime — a
`@cloudflare/vitest-pool-workers` test in workerd — needs the other provider.
v8 coverage is counters read back out of Node's inspector, which workerd has no
equivalent of; istanbul instruments at transform time, before the code crosses
the boundary. Set `coverage_provider = "istanbul"` and put
`@vitest/coverage-istanbul`, pinned to the same version as `vitest`, in `deps`:

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

Forgetting it does not report zeros: with only `@vitest/coverage-istanbul` in
`deps`, vitest falls back to its v8 default and the run fails with
`MISSING DEPENDENCY  Cannot find dependency '@vitest/coverage-v8'`.

Coverage is reported against the compiled `.js` in `bazel-out`, not the `.ts`
source, so `SF:` paths and line numbers are the compiler's. That is true of
every `ts_test`, not only the pooled ones.

## Running tests

```bash
bazel test //path/to:math_test
```

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`. Set `shard_count` on the target and pass
`--test_sharding_strategy=explicit`.

## Snapshots

Vitest resolves a `.snap` beside the test file it ran, which under Bazel is the
compiled `.js` in `bazel-out`. `ts_test` replaces that resolution with the path
the `.ts` source implies:

```
<package>/__snapshots__/<source file name>.snap
```

which is where a plain `vitest` run already keeps it. A repository adopting
`ts_test` renames nothing.

**Reading** them takes the `snapshots` attr — that is what puts the files in the
sandbox:

```python
ts_test(
    name = "widget_test",
    srcs = ["widget.test.ts"],
    snapshots = glob(["__snapshots__/*.snap"]),
    deps = [":widget", "@npm//:vitest"],
)
```

Without it the test cannot read the snapshot, and it fails rather than passing:
`ts_test` runs vitest in read-only snapshot mode (`CI=true`), so no `bazel test`
can write a `.snap` and then pass on what it just wrote. Set `env = {"CI":
"false"}` to opt out of that, at the cost of the guarantee.

**Writing** them uses the executable every `ts_test` declares alongside itself:

```bash
bazel run //path/to:widget_test.update_snapshots
```

It reuses the test's own compiled sources and writes under
`BUILD_WORKSPACE_DIRECTORY`, i.e. into your checkout, next to the `.ts` file.
Commit the result. `--sandbox_writable_path` is no longer involved.

`update_snapshots = True` on a `ts_test` makes *that* target the updater instead
of a test. It exists for an updater that has to stand alone, and it compiles
`srcs` itself — so it cannot share a package with a `ts_test` over the same
files, which would be two `ts_compile` targets declaring the same `.js` outputs.
The generated `<name>.update_snapshots` avoids that by sharing the test's
compile target, which is why it, not this attr, is the normal route.

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

## Listing npm deps

Test sources are checked for undeclared imports like any other `ts_compile`
sources: a module that only some dep's own deps provide fails the build with
the label to add. `bazel run //:gazelle` writes it — Gazelle collects imports
from the test files and the production sources in the package. See
[Deps have to be direct](ts-compile.md#deps-have-to-be-direct).

The auto-generated `node_modules` tree is built from the *direct* deps that
provide `NpmPackageInfo`, plus their transitive npm deps. It places every
*resolution* those made — name, version and peer set — keyed apart wherever one
name resolved more than once ([layout](node-modules.md#the-layout)). A `ts_compile` dep
does not contribute its own npm dependencies to it. So list every npm package
needed at runtime — by the test files *and* by the production code under test —
in `ts_test`'s `deps`, the way `go_test` requires all direct imports. Gazelle
does this automatically: it collects imports from both the test files and the
production sources in the package.

See [Testing with vitest](../guides/testing.md) for the full guide including
watch mode and build feedback.
