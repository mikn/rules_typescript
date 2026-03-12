# ts_binary

Produces a runnable JavaScript output by collecting transitive `.js` outputs from a `ts_compile` target and optionally invoking a bundler.

`ts_binary` is the stable public name for the bundling rule. It is functionally identical to `ts_bundle`.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary")

ts_binary(
    name = "app",
    entry_point = "//src/app",
    format = "esm",
    sourcemap = True,
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

## Without a Bundler

Without a `bundler` target, `ts_binary` concatenates all `.js` files in dependency order. Useful during development and for keeping the build graph valid.

## With Vite

See [Bundling with Vite](../guides/bundling.md) for a complete example with `vite_bundler`.

## Custom Bundler

Any rule returning `BundlerInfo` can plug in. See [Bundling — Custom Bundler](../guides/bundling.md#custom-bundler-bundlerinfo-interface).
