# ts_binary

Produces a **runnable** target from a `ts_compile` entry point. Without a
bundler it runs that target's entry `.js` on the JS runtime; with one it bundles
first and runs the bundle.

`ts_binary` and [`ts_bundle`](ts-bundle.md) are separate rules with overlapping
attributes, not aliases. Reach for `ts_binary` when you want `bazel run`; reach
for `ts_bundle` when you want a bundle as a build artifact — it requires a
`bundler` and adds the app-mode, HTML, `vite_config` and `staging_srcs` surface
that framework builds need.

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
| `entry_file` | `string` | `""` | Which source's `.js` is the entry when the target emits several, e.g. `"main.ts"`. `index.js` is used by convention when unset |
| `bundler` | `label` | `None` | Target providing `BundlerInfo`. When set, the bundle is what runs |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |
| `node_modules` | `label` | `None` | `node_modules` target for packages the program needs at runtime |

`minify` and `split_chunks` are `ts_bundle` attributes; `ts_binary` does not
accept them.

## Without a Bundler

Without a `bundler`, `ts_binary` runs the entry point's own `.js` file on the JS
runtime, with the transitive `.js` outputs in its runfiles — the imports resolve
as written. It does not concatenate anything.

## With Vite

See [Bundling with Vite](../guides/bundling.md) for a complete example with `vite_bundler`.

## Custom Bundler

Any rule returning `BundlerInfo` can plug in. See [Bundling — Custom Bundler](../guides/bundling.md#custom-bundler-bundlerinfo-interface).
