# ts_bundle

Produces a bundled JavaScript output by collecting transitive `.js` outputs and invoking a pluggable bundler. The `bundler` attr is required. Vite is the only bundler this ruleset ships; another one plugs in through [`BundlerInfo`](../guides/bundling.md#custom-bundler-bundlerinfo-interface).

[`ts_binary`](ts-binary.md) is a separate, runnable rule. Its `bundler` is
optional (without one it runs the entry `.js` directly), and it does not accept
`minify`, `split_chunks`, `env_vars`, `mode`, `html`, `public_dir`, `manifest`,
`vite_config`, `vite_config_srcs` or `staging_srcs`. It has two of its own
instead: `entry_file` and `node_modules`.

## Usage

Declare all three at the workspace root. The bundler's `node_modules` tree needs
every npm package the bundled graph imports, not just Vite, and it has to sit in
a directory that is an ancestor of the compiled `.js` doing the importing. See
[Bundling](../guides/bundling.md#where-the-bundlers-node_modules-has-to-sit).

```python
# BUILD.bazel, at the workspace root
load("@rules_typescript//ts:defs.bzl", "ts_bundle")
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:zod",
    ],
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
| `minify` | `bool` | `True` | `True` selects the running Vite's own default minifier (esbuild on 6, oxc on 8) without naming one; `False` also pins `output.minify` |
| `split_chunks` | `bool` | `False` | Third-party code in its own chunk, via `build.rollupOptions.output.manualChunks`. Vite bundlers only; in lib mode the output becomes a directory |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |
| `env_vars` | `string_dict` | `{}` | Sugar over `define`: `{"VITE_API_URL": "…"}` becomes `import.meta.env.VITE_API_URL` |
| `mode` | `string` | `"lib"` | `"lib"` (single JS output) or `"app"` (HTML application; requires `html`) |
| `html` | `label` | `None` | HTML entry point for `mode = "app"`; the output is a directory of hashed assets |
| `public_dir` | `label` | `None` | Static files Vite copies into the output directory verbatim: no hash, no transform. `mode = "app"` only |
| `manifest` | `bool` | `False` | Write `manifest.json` into the output directory, mapping each input to the hashed file it became. `mode = "app"` only |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, whose plugins run before Bazel's. Vite bundlers only |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged together with it. A file outside the config's package is an analysis-time error |
| `staging_srcs` | `label_list` | `[]` | Sources copied into a writable staging directory before Vite runs, for framework plugins that scan route files and write codegen next to them |

See [chunk splitting](../guides/bundling.md#chunk-splitting) for `minify` and
`split_chunks`, and
[static files](../guides/bundling.md#static-files-public_dir) for `public_dir`
and `manifest`.

The `vite_config` plugins run first, which is how a framework plugin gets in:
TanStack Start's and Remix's do, SvelteKit's and Solid Start's do not. See
[framework detection](../gazelle/overview.md#framework-detection). Its local
imports need `vite_config_srcs`; see
[Bundling § Framework plugins](../guides/bundling.md#framework-plugins-via-vite_config).

Gazelle generates `staging_srcs` on a framework root and recomputes it every run,
so a label it cannot derive needs a `# keep` on its line. See
[attributes Gazelle owns](../gazelle/directives.md#attributes-gazelle-owns).

Of the loaded `vite_config`, this rule reads `plugins` and `root`. Any other key
fails the build naming itself; see
[Bundling § Keys the generated config reads](../guides/bundling.md#keys-the-generated-config-reads).
`ts_dev_server` reads `plugins` only, so a config carrying `root` builds here and
fails there.

A `.css`, a `*.module.css` and an asset reach the bundler the same way the `.js`
does — through the entry point's `CssInfo`, `CssModuleInfo` and `AssetInfo`. See
[css_library, css_module, asset_library](css-and-assets.md) for what each one
promises, and [Bundling](../guides/bundling.md) for the complete guide.
