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
| `target` | `string` | `"es2022"` | ECMAScript target for the internal `ts_compile` |
| `jsx_mode` | `string` | `"react-jsx"` | JSX mode for the internal `ts_compile` |
| `declarations` | `string` | `"tsgo"` | Declaration emitter for the internal `ts_compile` |
| `environment` | `string` | `""` | `test.environment` — `node`, `jsdom`, `happy-dom`, `edge-runtime`, or any custom vitest environment package. The package must be in `deps` |
| `coverage` | `bool` | `False` | Also instrument during plain `bazel test`. `bazel coverage` works on every target regardless |
| `config` | `label` or `dict` | `None` | A vitest config file (`.ts`/`.mts`/`.cts`/`.js`/`.mjs`/`.cjs`) or an inline dict. **Merged**, not substituted |
| `setup_files` | `label_list` | `[]` | `test.setupFiles`. `.ts`/`.tsx` entries are compiled with the same `deps` as the tests |
| `global_setup` | `label_list` | `[]` | `test.globalSetup`; compiled like `setup_files` |
| `data` | `label_list` | `[]` | Extra runfiles: fixtures, and files a `config` or setup entry imports |
| `globals` | `bool` | `False` | `test.globals` — global `describe`/`it`/`expect` |
| `reporters` | `string_list` | `[]` | `test.reporters`, e.g. `["default", "junit"]` |
| `coverage_thresholds` | `string_dict` | `{}` | `test.coverage.thresholds`, e.g. `{"lines": "80", "perFile": "true"}`. Values that look numeric or boolean are emitted as such |
| `update_snapshots` | `bool` | `False` | Produces an executable (not a test) that runs `vitest run --update`. **Currently broken** — see [Snapshot updating](#snapshot-updating) |

## The generated vitest config

A config is always generated and always passed with `--config`, so vitest never
auto-discovers a stray config out of the runfiles tree. It is an *entry* config
that layers three sources, lowest precedence first:

| Layer | Contents |
|-------|----------|
| 1. Bazel | the CSS-module mock plugin, when a dep provides `CssModuleInfo` |
| 2. user | the `config` attr — a config file or an inline dict |
| 3. attributes | `environment`, `setup_files`, `global_setup`, `globals`, `reporters`, `coverage_thresholds` |

Objects merge key by key; arrays concatenate base-first, matching vite's own
`mergeConfig`. So a user `plugins` list never displaces the CSS-module mock, and
a user `setupFiles` list never displaces `setup_files` — the attribute's entries
run after the config's. Scalars from a later layer win, which is why
`environment` overrides an environment set inside `config`.

Two things sit outside the layering and outrank all of it, because they are the
sandbox contract rather than configuration: npm resolution into the runfiles
tree (`NODE_PATH`, set by the runner script) and coverage output paths (vitest
CLI flags, so `bazel coverage` writes lcov where Bazel expects it).

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
either. An array is read as a vitest workspace definition and becomes
`test.workspace`; each project in it receives the Bazel layer and the attribute
layer too, because every workspace project gets its own Vite server. Anything
the config imports relatively must be in `data` — it is not a build input
otherwise.

Note: vitest 3.2 renamed `test.workspace` to `projects`. The pinned vitest is
3.0.9 and the new spelling is unhandled.

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

## Running tests

```bash
bazel test //path/to:math_test
```

## Sharding

`ts_test` distributes test files across shards using `TEST_SHARD_INDEX` and
`TEST_TOTAL_SHARDS`. Set `shard_count` on the target and pass
`--test_sharding_strategy=explicit`.

## Snapshot updating

`update_snapshots = True` is meant to produce an executable that writes
snapshots back into the source tree:

```bash
bazel run //path/to:update_snapshots
```

It does not work today: no test in the repository covers it, and it is recorded
as broken in [CHANGELOG.md](../changelog.md#known-gaps). Until it is fixed,
make the snapshot directory writable in the sandbox instead:

```bash
bazel test //path/to:my_test \
  --sandbox_writable_path=$(pwd)/src/components/__snapshots__
```

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

The auto-generated `node_modules` tree is built from the *direct* deps that
provide `NpmPackageInfo`, plus their transitive npm deps. A `ts_compile` dep
does not contribute its own npm dependencies to it. So list every npm package
needed at runtime — by the test files *and* by the production code under test —
in `ts_test`'s `deps`, the way `go_test` requires all direct imports. Gazelle
does this automatically: it collects imports from both the test files and the
production sources in the package.

See [Testing with vitest](../guides/testing.md) for the full guide including
watch mode and build feedback.
