# css_library, css_module, asset_library, json_library

Four rules put non-TypeScript files into the module graph. Each generates the
ambient declaration TypeScript needs (`allowArbitraryExtensions`) and propagates
its files through a provider, so a bundler or dev server downstream can find
them.

| Rule | Files | Import form | Provider |
|------|-------|-------------|----------|
| `css_library` | `.css` | `import "./button.css"` (side effect) | `CssInfo` |
| `css_module` | `.css` (Gazelle routes `*.module.css` here) | `import styles from "./Button.module.css"` | `CssModuleInfo` |
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
its dots turned into underscores (`Panel.module.css` → `Panel_module_css`).

## Imports Are Not Rewritten

`ts_compile` leaves the import alone. `Button.js` in `bazel-bin` still says
`import "./button.css"`, the carrier of both facts a consumer needs: that this
module requires that stylesheet, and in what order relative to the others. Node
cannot load it:

```
$ node bazel-bin/tests/css/Button.js
TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown file extension ".css"
```

A target with a `css_library`, `css_module` or `asset_library` dep is consumed
by [`ts_bundle`](ts-bundle.md), by [`ts_dev_server`](ts-dev-server.md), or by a
downstream bundler reading the published package.

## The files are copied into `bazel-bin`

`css_library`, `css_module` and `asset_library` copy their sources into
`bazel-bin` beside the compiled `.js` and put the copies, not the source files,
in the provider. The importer is that compiled `.js`, and
`import "./button.css"` resolves relative to the importer, so a stylesheet that
existed only in the source tree would not be where the bundler looks.

They are copies, not symlinks, because a bundler resolves a symlink to its real
path before resolving what the file itself imports: `@import "tailwindcss"`
reached through a symlink would look for a source-tree `node_modules` that a
Bazel build does not have.

## What the Declarations Promise

`css_library` emits an empty declaration; a side-effect import has no export
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

The action runs postcss-modules over the stylesheet, writes the export map it
produced to `<source>.exports.json`, and generates the declaration from that
map's keys, so the declared keys are exactly what the stylesheet exports.
`@keyframes` names, `#id` selectors and `@value` names are exports and are
declared, so `styles["panel-fade"]` type-checks.
`composes: … from "./other.module.css"` resolves, and fails on bad input:
postcss-modules errors on a name it cannot find, and the other file has to be in
`deps`.

The values are scoped names Bazel decides:

```
_<local name>_<first 8 hex of sha256(hash_prefix + stylesheet bytes)>
```

The name is a pure function of the local name and the stylesheet's own bytes: no
filename, no cwd, no line number, so a build in a different sandbox or output
base mints the same name. `ts_bundle`, `ts_dev_server` and `ts_test` install a
Bazel-owned Vite plugin that hands Vite that map, so the bundler's own
CSS-modules pass reproduces the names. Under `ts_test` the import resolves to
the same map, so a unit test asserts the string the bundle really emits.

Four attributes change the answer, and they belong on the rule that wrote the
declaration: `locals_convention`, `scope_behaviour`, `hash_prefix` and
`export_globals`. Setting one of them under `css.modules` in the bundler config
is a hard build failure naming the attribute to use, and so are
`css.modules.generateScopedName` and `css.modules = false`.

`asset_library` and `json_library` promise a `string` URL and the parsed JSON
shape respectively. JSON is not an `asset_library` extension: `json_library`
parses the file at build time and generates real property types, which an
ambient `string` declaration would throw away.

## In a Bundle

App mode hashes every imported stylesheet and asset and rewrites the references
in the HTML. Lib mode extracts all CSS into one declared `<bundle_name>.css` and
does not reference it from the JS, so the consumer has to include it.

Static files that must keep the name they were given — `robots.txt`, a favicon
named from an HTML tag — go in `ts_bundle`'s `public_dir` and not an
`asset_library`: those are copied verbatim, unhashed. See
[Bundling](../guides/bundling.md#css-css-modules-and-assets).
