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
| `json_library` | `.json` | `import config from "./config.json"` (fully typed) | none |

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

Gazelle writes all four, one target per file, named after the file with its
dots turned into underscores (`Panel.module.css` → `Panel_module_css`).

## Attributes

### css_library

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.css` files |
| `deps` | `label_list` | `[]` | Other `css_library` targets (`CssInfo`). Their stylesheets join this target's `transitive_css_files` |

### css_module

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.css` files; Gazelle routes `*.module.css` here |
| `deps` | `label_list` | `[]` | Other `css_module` targets (`CssModuleInfo`). What `composes: … from "./other.module.css"` resolves against |
| `locals_convention` | `string` | `""` | postcss-modules `localsConvention`, which rewrites the exported keys: `camelCase`, `camelCaseOnly`, `dashes`, `dashesOnly`, `all` or `none`. `""` passes no option, so the keys are the class names as written |
| `scope_behaviour` | `string` | `""` | postcss-modules `scopeBehaviour`: `local` or `global`. `""` is `local` |
| `hash_prefix` | `string` | `""` | Salts the content hash in every scoped name: `sha256(hash_prefix + stylesheet bytes)` |
| `export_globals` | `bool` | `False` | postcss-modules `exportGlobals`: also export the names `:global(...)` leaves unscoped |

Any other value for `locals_convention` or `scope_behaviour` is an analysis-time
error listing the accepted ones.

### asset_library

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.svg .png .jpg .jpeg .gif .webp .woff .woff2 .ttf .eot .md .txt .jsonc` files. JSON goes to `json_library` |
| `deps` | `label_list` | `[]` | Other `asset_library` targets (`AssetInfo`) |
| `declaration_type` | `string_dict` | `{}` | Extension, leading dot included, to the TypeScript type an import of it resolves to. An extension left out keeps the `string` URL. See [When an Asset Is Not a URL](#when-an-asset-is-not-a-url) |

### json_library

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | `.json` files |
| `deps` | `label_list` | `[]` | Other `json_library` targets (`TsDeclarationInfo`) |

## Imports Are Not Rewritten

`ts_compile` leaves the import alone. `Button.js` in `bazel-bin` still says
`import "./button.css"`. The import records that this module requires that
stylesheet, and in what order relative to the others. Node cannot load it:

```
$ node bazel-bin/tests/css/Button.js
TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown file extension ".css" for /…/bazel-bin/tests/css/button.css
```

A target with a `css_library`, `css_module` or `asset_library` dep is consumed
by [`ts_bundle`](ts-bundle.md), by [`ts_dev_server`](ts-dev-server.md), or by a
downstream bundler reading the published package.

## Copies in `bazel-bin`

`css_library`, `css_module` and `asset_library` copy their sources into
`bazel-bin` beside the compiled `.js` and put the copies, not the source files,
in the provider. The importer is the compiled `.js`, and `import "./button.css"`
resolves relative to it.

They are copies, not symlinks. A bundler resolves a symlink to its real path
before resolving what the file imports, so `@import "tailwindcss"` reached
through a symlink would look for a source-tree `node_modules`.

## What the Declarations Promise

`css_library` emits an empty declaration.

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
`composes: … from "./other.module.css"` resolves when the other file is in
`deps`. postcss-modules errors on a name it cannot find.

The values are scoped names Bazel decides:

```
_<local name>_<first 8 hex of sha256(hash_prefix + stylesheet bytes)>
```

The name depends on the local name and the stylesheet bytes only: no filename,
no cwd, no line number. A build in a different sandbox or output base mints the
same name. `ts_bundle`, `ts_dev_server` and `ts_test` install a Bazel-owned Vite
plugin that hands Vite that map, so the bundler's CSS-modules pass emits the
same names. Under `ts_test` the import resolves to the same map.

Four attributes change the names: `locals_convention`, `scope_behaviour`,
`hash_prefix` and `export_globals`. They are set on the `css_module` target.
Setting one of them under `css.modules` in the bundler config fails the build
naming the attribute to use; so do `css.modules.generateScopedName` and
`css.modules = false`.

`asset_library` declares a `string` URL by default and `json_library` the parsed
JSON shape. `.json` is not an `asset_library` extension: `json_library` parses
the file at build time and generates property types. The parse accepts the
JSON-with-comments dialect TypeScript accepts, so a `.json` file with comments or
trailing commas types like any other. `.jsonc` stays with `asset_library`: no
bundler parses that extension as JSON, so the import yields a URL.

## When an Asset Is Not a URL

A bundler that does not transform a file hands back a URL string. A project
running an svgr-style plugin gets a React component from `.svg` instead.
`declaration_type` declares that, keyed by extension:

```python
asset_library(
    name = "logo_svg",
    srcs = ["logo.svg"],
    declaration_type = {
        ".svg": 'import("react").FC<import("react").SVGProps<SVGElement>>',
    },
)
```

Match the expression to the `declare module "*.svg"` the project already has:
`SVGProps` is invariant in its element, so `FC<SVGProps<SVGElement>>` satisfies
a consumer expecting `SVGSVGElement` while the reverse does not.

An extension left out keeps `string`, so one target can retype its `.svg` and
leave its `.png` alone. A key that is not an `asset_library` extension is an
analysis failure listing the ones that are.

Gazelle writes one `asset_library` per asset file. The directive
[`# gazelle:ts_asset_declaration_type`](../gazelle/directives.md#declare-what-an-asset-extension-imports-as)
writes this attribute on every one of them: one directive per extension,
applying to a directory and below.

The expression is inserted verbatim, so every name in it has to resolve from
inside a generated declaration: write `import("pkg").Type`, not a top-level
import, and keep `pkg` in the deps of the `ts_compile` target that imports the
asset.

The expression is not checked. The generated file is a `.d.ts` and the ruleset
compiles with `skipLibCheck`, so a name that does not resolve widens the import
to `any` and every use of it type-checks. `--//ts:lib_check` turns `skipLibCheck`
off for the whole build; `compiler_options = {"skipLibCheck": False}` turns it
off on one target. Either surfaces the error:

```
bazel-out/k8-fastbuild/bin/web/assets/logo.svg.d.ts(4,22): error TS2304: Cannot find name 'Fc'.
```

The generated file's first line names the target and the attribute that wrote
the type, so the error leads back to the BUILD file.

A `declare module "*.svg"` in the project does not apply. TypeScript prefers
the concrete `logo.svg.d.ts` this rule writes beside the asset over a wildcard
pattern, so the generated declaration wins. `declaration_type` is the way to
change it. An alias reaches the same declaration: `path_aliases` names a source
directory, and the rule maps the prefix onto that directory and its `bazel-bin`
mirror, where the generated declaration lands. Only a declaration the target
depends on is in the sandbox, so the dep decides what an aliased import resolves
to.

## In a Bundle

App mode hashes every imported stylesheet and asset and rewrites the references
in the HTML. Lib mode extracts all CSS into one declared `<bundle_name>.css` and
does not reference it from the JS, so the consumer has to include it.

Static files that must keep their name (`robots.txt`, a favicon named from an
HTML tag) go in `ts_bundle`'s `public_dir`, not an `asset_library`. `public_dir`
files are copied verbatim, unhashed. See
[Bundling](../guides/bundling.md#css-css-modules-and-assets).
