# Bundling

`ts_binary` and `ts_bundle` collect transitive `.js` outputs and hand them to a pluggable bundler.

`ts_bundle` requires a `bundler`: there is no bundler-less mode, because an
artifact that only looks like a bundle is worse than a build error.

## Basic Usage (No Bundling)

`ts_binary` without a `bundler` runs the entry point `.js` directly — nothing is bundled.

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary")

ts_binary(
    name = "app",
    entry_point = "//src/app",
)
```

```bash
bazel run //:app
```

## With Vite

```python
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_bundle")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    vite = "@npm//:vite",
    node_modules = ":node_modules",
)

ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    format = "esm",
    sourcemap = True,
    minify = True,
    external = ["react", "react-dom"],
)
```

## Custom Bundler (BundlerInfo Interface)

Any Bazel rule that returns `BundlerInfo` can plug into `ts_bundle` and `ts_binary`. This lets you bring your own bundler — esbuild, Rolldown, webpack — without modifying `rules_typescript`.

```python
load("@rules_typescript//ts:defs.bzl", "BundlerInfo")

def _my_bundler_impl(ctx):
    return [BundlerInfo(
        bundler_binary = ctx.file.binary,
        config_file = None,                 # optional static config
        runtime_deps = depset([]),           # files needed at bundle time
        use_generated_config = False,        # set True for Vite-style config
    )]

my_bundler = rule(
    implementation = _my_bundler_impl,
    attrs = {
        "binary": attr.label(
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)
```

### BundlerInfo Invocation Modes

**Mode 1 — Standard CLI** (`use_generated_config = False`, the default)

`ts_bundle` invokes the bundler binary with:

```
<bundler_binary>
  --entry  <path/to/entry.js>
  --out-dir <output/dir>
  --format esm|cjs|iife
  [--external <pkg>]...
  [--sourcemap]
  [--config <config_file>]   (only when config_file is set)
```

Output is expected at `<out-dir>/<bundle_name>.js` (and `.js.map` if `--sourcemap`).

**Mode 2 — Generated config** (`use_generated_config = True`)

`ts_bundle` generates a `vite.config.mjs` containing all bundle options and invokes:

```
<bundler_binary> <absolute_path_to_vite.config.mjs> <entry_path> <out_dir>
```

| Format | Output file |
|--------|-------------|
| `esm` | `<bundle_name>.es.js` |
| `cjs` | `<bundle_name>.cjs.js` |
| `iife` | `<bundle_name>.iife.js` |

### BundlerInfo Fields

| Field | Type | Description |
|-------|------|-------------|
| `bundler_binary` | `File` | The executable that performs bundling |
| `config_file` | `File` or `None` | Optional static config passed via `--config` (mode 1 only) |
| `runtime_deps` | `depset of File` | Files the bundler needs at runtime |
| `use_generated_config` | `bool` | When `True`, use mode 2 (generated vite.config.mjs) |

## Attributes

The two rules are not aliases and their attribute sets differ. Shared:

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target providing `JsInfo` |
| `bundler` | `label` | required for `ts_bundle`, optional for `ts_binary` | Target providing `BundlerInfo` |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |

`ts_bundle` only: `minify`, `split_chunks`, `env_vars`, `mode`, `html`,
`vite_config`, `staging_srcs` — see the
[ts_bundle reference](../rules/ts-bundle.md#attributes).

`ts_binary` only: `entry_file` (which `.js` is the entry when the target emits
several) and `node_modules` — see the
[ts_binary reference](../rules/ts-binary.md#attributes).

Setting `minify` on a `ts_binary` is an analysis error, not a no-op.
