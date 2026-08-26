# ts_bundle

Produces a bundled JavaScript output by collecting transitive `.js` outputs and invoking a pluggable bundler. The `bundler` attr is required.

`ts_bundle` produces a bundle as a build artifact and requires a `bundler`.
[`ts_binary`](ts-binary.md) is a separate, runnable rule: it shares many
attributes but its `bundler` is optional (without one it runs the entry `.js`
directly) and it does not accept `minify`, `split_chunks`, `env_vars`, `mode`,
`html`, `public_dir`, `manifest`, `vite_config`, `vite_config_srcs` or
`staging_srcs`. It has two of its own instead: `entry_file` and `node_modules`.

## Usage

Declare all three at the workspace root. The bundler's `node_modules` tree needs
every npm package the bundled graph imports, not just Vite, and it has to sit in
a directory that is an ancestor of the compiled `.js` doing the importing
([why](../guides/bundling.md#where-the-bundlers-node_modules-has-to-sit)).

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
| `minify` | `bool` | `True` | `True` selects the running Vite's own default minifier rather than naming one (esbuild on 6, oxc on 8 — naming `esbuild` would pick an optional peer that is not in the tree). `False` also pins `output.minify`, so a plugin's `renderChunk` output survives the dead-code pass |
| `split_chunks` | `bool` | `False` | Give third-party code its own chunk, via `build.rollupOptions.output.manualChunks`. Vite bundlers and lib mode only; the output becomes a directory ([detail](../guides/bundling.md#chunk-splitting)) |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |
| `env_vars` | `string_dict` | `{}` | Sugar over `define`: `{"VITE_API_URL": "…"}` becomes `import.meta.env.VITE_API_URL` |
| `mode` | `string` | `"lib"` | `"lib"` (single JS output) or `"app"` (HTML application; requires `html`) |
| `html` | `label` | `None` | HTML entry point for `mode = "app"`; the output is a directory of hashed assets |
| `public_dir` | `label` | `None` | Static files Vite copies into the output directory verbatim — no hash, no transform. `mode = "app"` only ([detail](../guides/bundling.md#static-files-public_dir)) |
| `manifest` | `bool` | `False` | Write `manifest.json` into the output directory, mapping each input to the hashed file it became. `mode = "app"` only |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`. Its plugins run before Bazel's, which is how a framework plugin gets in — TanStack Start's and Remix's do; SvelteKit's and Solid Start's [cannot](../gazelle/overview.md#framework-detection). Vite bundlers only |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports. The config and these are staged together under `bazel-bin`, each at its path relative to the config's package, so a relative import resolves there as it does in the source tree. Without it only the config is staged and its relative imports fail, naming the file; a file outside the config's package is an analysis-time error |
| `staging_srcs` | `label_list` | `[]` | Sources copied into a writable staging directory before Vite runs, for framework plugins that scan route files and write codegen next to them |

Of the loaded `vite_config`, this rule reads `plugins` and `root`. Any other key
fails the build naming itself, rather than producing a bundle that quietly
ignored half its configuration — see
[Bundling § Keys the generated config reads](../guides/bundling.md#keys-the-generated-config-reads).
`ts_dev_server` reads `plugins` only, so a config carrying `root` builds here and
fails there.

A `.css`, a `*.module.css` and an asset reach the bundler the same way the `.js`
does — through the entry point's `CssInfo`, `CssModuleInfo` and `AssetInfo`. See
[css_library, css_module, asset_library](css-and-assets.md) for what each one
promises, and [Bundling](../guides/bundling.md) for the complete guide.
