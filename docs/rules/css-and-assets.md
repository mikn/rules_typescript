# css_library, css_module, asset_library, json_library

Four rules put non-TypeScript files into the module graph. Each one generates the
ambient declaration TypeScript needs (`allowArbitraryExtensions`), and each one
propagates its files through a provider so that a bundler or dev server
downstream can find them.

| Rule | Files | Import form | Provider |
|------|-------|-------------|----------|
| `css_library` | `.css` | `import "./button.css"` (side effect) | `CssInfo` |
| `css_module` | `*.module.css` | `import styles from "./Button.module.css"` | `CssModuleInfo` |
| `asset_library` | `.svg .png .jpg .jpeg .gif .webp .woff .woff2 .ttf .eot` | `import logo from "./logo.svg"` (URL string) | `AssetInfo` |
| `json_library` | `.json` | `import config from "./config.json"` (fully typed) | — |

```python
load("@rules_typescript//ts:defs.bzl", "asset_library", "css_library", "css_module", "ts_compile")

css_library(
    name = "button_css",
    srcs = ["button.css"],
)

css_module(
    name = "panel_module_css",
    srcs = ["Panel.module.css"],
)

asset_library(
    name = "logo_svg",
    srcs = ["logo.svg"],
)

ts_compile(
    name = "ui",
    srcs = ["Button.tsx", "Panel.tsx"],
    deps = [":button_css", ":logo_svg", ":panel_module_css"],
)
```

Gazelle writes all four for you, one target per file, named after the file with
its dots turned into underscores (`Panel.module.css` → `panel_module_css`).

## A dep on any of them means the consumer bundles

`ts_compile` does not rewrite the import. `Button.js` in `bazel-bin` still says
`import "./button.css"`, because that import is the only carrier of both facts a
consumer needs: that this module requires that stylesheet, and in what order
relative to the others. Node cannot load it:

```
$ node bazel-bin/tests/css/Button.js
TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown file extension ".css"
```

That is not a bug to route around — it is what "this is bundler-targeted code"
looks like. A target with a `css_library`, `css_module` or `asset_library` dep is
consumed by [`ts_bundle`](ts-bundle.md), by
[`ts_dev_server`](ts-dev-server.md), or by a downstream bundler reading the
published package. Running its `.js` under bare `node` is not among the options.

## The files are copied into `bazel-bin`

Each rule copies its sources into `bazel-bin` beside the compiled `.js` and puts
the copies — not the source files — in its provider. The importer is that
compiled `.js`, and `import "./button.css"` resolves relative to the importer, so
a stylesheet that existed only in the source tree would not be where the bundler
looks.

They are copies rather than symlinks because a bundler resolves a symlink to its
real path before resolving what the file itself imports: `@import "tailwindcss"`
reached through a symlink would look for a source-tree `node_modules` that a
Bazel build does not have.

## What the declarations promise

`css_library` emits an empty declaration — a side-effect import has no export
surface to describe.

`css_module` emits the keys:

```typescript
declare const styles: {
  readonly panel: string;
  readonly panelTitle: string;
  readonly "panel-footer": string;
};
export default styles;
```

Keys come from selectors: declaration values, comments, strings, at-rule
preludes, `@keyframes` bodies and `:global(…)` groups contribute none, while
`:local(…)` — the explicit spelling of the default — does. The *values* are
scoped names, and nothing in Bazel decides them: Vite runs postcss-modules,
which derives each one from the CSS text. Under `ts_test` the import is mocked
with a proxy that returns the semantic name instead, so a unit test asserts
`"panel"` rather than a hash it cannot predict.

One key set divergence is known: postcss-modules also scopes and exports
`@keyframes` names, and the generated declaration omits them, so
`styles["panel-fade"]` exists at runtime and does not type-check.
`//tests/vite_bundle:bundle_assets_test` pins this by comparing the declaration
against the export map postcss-modules actually produced.

`asset_library` and `json_library` promise a `string` URL and the parsed JSON
shape respectively. JSON is deliberately not an `asset_library` extension:
`json_library` parses the file at build time and generates real property types,
which an ambient `string` declaration would throw away.

## In a bundle

App mode hashes every imported stylesheet and asset and rewrites the references
in the HTML. Lib mode extracts all CSS into one declared `<bundle_name>.css`
and does not reference it from the JS, so the consumer has to include it.

For static files that must keep the name they were given — `robots.txt`, a
favicon named from an HTML tag — use `ts_bundle`'s `public_dir` instead of an
`asset_library`: those are copied verbatim, unhashed. See
[Bundling](../guides/bundling.md#css-css-modules-and-assets).
