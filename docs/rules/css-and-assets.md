# css_library, css_module, asset_library, json_library

Four rules put non-TypeScript files into the module graph. Each generates the
ambient declaration TypeScript needs (`allowArbitraryExtensions`) and propagates
its files through a provider, so a bundler or dev server downstream can find
them.

| Rule | Files | Import form | Provider |
|------|-------|-------------|----------|
| `css_library` | `.css` | `import "./button.css"` (side effect) | `CssInfo` |
| `css_module` | `.css` (Gazelle routes `*.module.css` here) | `import styles from "./Button.module.css"` | `CssModuleInfo` |
| `asset_library` | `.svg .png .jpg .jpeg .gif .webp .woff .woff2 .ttf .eot .md .txt .jsonc` | `import logo from "./logo.svg"` (URL string by default; see [`declaration_type`](#when-an-asset-is-not-a-url)) | `AssetInfo` |
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

`asset_library` promises a `string` URL by default and `json_library` the parsed
JSON shape. JSON is not an `asset_library` extension: `json_library`
parses the file at build time and generates real property types, which an
ambient `string` declaration would throw away. The parse reads the
JSON-with-comments dialect TypeScript itself accepts, so a `.json` file carrying
comments or trailing commas types like any other. `.jsonc` goes the other way
and stays with `asset_library`: no bundler parses that extension as JSON, so the
import yields a URL and not a value.

## When an Asset Is Not a URL

A URL string is what a bundler hands back when it does not transform the file.
A project running an svgr-style plugin gets a React component from `.svg`
instead, and `declaration_type` is how it says so, keyed by extension:

```python
asset_library(
    name = "logo_svg",
    srcs = ["logo.svg"],
    declaration_type = {
        ".svg": 'import("react").FC<import("react").SVGProps<SVGSVGElement>>',
    },
)
```

An extension left out keeps `string`, so one target can retype its `.svg` and
leave its `.png` alone. A key that is not an `asset_library` extension is an
analysis failure listing the ones that are.

Gazelle writes one `asset_library` per asset file, so on a repo of any size this
attribute is written by
[`# gazelle:ts_asset_declaration_type`](../gazelle/directives.md#declare-what-an-asset-extension-imports-as)
rather than by hand: one directive per extension, applying to a directory and
below.

The expression is inserted verbatim, so any name it uses has to resolve from
inside a generated declaration: write `import("pkg").Type` rather than a
top-level import, and keep `pkg` in the *consuming* `ts_compile` target's deps.

**The expression is unchecked, and a name that does not resolve is silent.**
The generated file is a `.d.ts` and this ruleset compiles with `skipLibCheck`,
so a typo does not error — the import widens to `any` and every use of it
type-checks. Build the consuming target with
`compiler_options = {"skipLibCheck": False}` to surface it:

```
bazel-out/k8-fastbuild/bin/web/assets/logo.svg.d.ts(4,22): error TS2304: Cannot find name 'Fc'.
```

The generated file's first line names the target and the attribute that wrote
the type, so the error leads back to the BUILD file.

**A `declare module "*.svg"` in the project does not do this job.** TypeScript
prefers the concrete `logo.svg.d.ts` this rule writes beside the asset over any
wildcard pattern, so the generated declaration wins and the project's ambient
never applies — `declaration_type` is the supported way to change the answer.
The exception is an asset reached through a `path_aliases` alias rather than a
relative import: the alias resolves into the source tree, where no generated
declaration sits beside the asset, so a wildcard ambient decides that import and
`declaration_type` does not reach it.

## In a Bundle

App mode hashes every imported stylesheet and asset and rewrites the references
in the HTML. Lib mode extracts all CSS into one declared `<bundle_name>.css` and
does not reference it from the JS, so the consumer has to include it.

Static files that must keep the name they were given — `robots.txt`, a favicon
named from an HTML tag — go in `ts_bundle`'s `public_dir` and not an
`asset_library`: those are copied verbatim, unhashed. See
[Bundling](../guides/bundling.md#css-css-modules-and-assets).
