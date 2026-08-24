# ts_bundle

Produces a bundled JavaScript output by collecting transitive `.js` outputs and invoking a pluggable bundler. The `bundler` attr is required.

`ts_bundle` produces a bundle as a build artifact and requires a `bundler`.
[`ts_binary`](ts-binary.md) is a separate, runnable rule: it shares many
attributes but its `bundler` is optional (without one it runs the entry `.js`
directly) and it does not accept `minify`, `split_chunks`, `mode`, `html`,
`vite_config` or `staging_srcs`.

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
| `bundler` | `label` | required | Target providing `BundlerInfo` |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `minify` | `bool` | `True` | Minify the bundle |
| `split_chunks` | `bool` | `False` | Enable chunk splitting (Vite mode only; output is a directory) |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |
| `env_vars` | `string_dict` | `{}` | Sugar over `define`: `{"VITE_API_URL": "…"}` becomes `import.meta.env.VITE_API_URL` |
| `mode` | `string` | `"lib"` | `"lib"` (single JS output) or `"app"` (HTML application; requires `html`) |
| `html` | `label` | `None` | HTML entry point for `mode = "app"`; the output is a directory of hashed assets |
| `vite_config` | `label` | `None` | A `.mjs`/`.js` file default-exporting `{plugins: [...]}`. Its plugins run before Bazel's, which is how framework plugins (TanStack Start, Remix, SvelteKit, Solid Start) get in. Vite bundlers only |
| `staging_srcs` | `label_list` | `[]` | Sources copied into a writable staging directory before Vite runs, for framework plugins that scan route files and write codegen next to them |

See [Bundling](../guides/bundling.md) for the complete guide.
