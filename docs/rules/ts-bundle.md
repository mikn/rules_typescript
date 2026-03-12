# ts_bundle

Produces a bundled JavaScript output by collecting transitive `.js` outputs and invoking a pluggable bundler.

`ts_bundle` is the canonical name for the bundling rule. `ts_binary` is an alias with identical behaviour.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_bundle")
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")

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

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target providing `JsInfo` |
| `bundler` | `label` | `None` | Target providing `BundlerInfo` |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `minify` | `bool` | `True` | Minify the bundle |
| `split_chunks` | `bool` | `False` | Enable chunk splitting (Vite mode only; output is a directory) |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |

See [Bundling](../guides/bundling.md) for the complete guide.
