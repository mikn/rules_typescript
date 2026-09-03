# Publishing Packages

`ts_npm_publish` assembles a publishable npm package from a `ts_compile` target:
`.js`, `.js.map` and `.d.ts` outputs, merged with a `package.json` template, into
a staging directory and a tarball `npm publish` accepts.

Publishing itself is not a rule. It needs credentials, is not reproducible, and
must not happen because something in the build graph changed.

## Setup

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_npm_publish")

ts_compile(
    name = "lib",
    srcs = ["index.ts", "math.ts"],
    visibility = ["//visibility:public"],
)

ts_npm_publish(
    name = "lib_pkg",
    package = ":lib",
    package_json = ":package.json",
    version = "1.2.3",
)
```

```bash
bazel build //:lib_pkg
```

## Outputs

| Output | Description |
|--------|-------------|
| `lib_pkg_pkg/package/` | Staging directory; the `package/` level is npm's convention |
| `lib_pkg_pkg.tar` | Tarball with the `package/` prefix, ready for `npm publish` |

## Publishing

Publish directly from the Bazel output:

```bash
npm publish $(bazel cquery --output=files //:lib_pkg | grep '\.tar$')
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `package` | `label` | required | `ts_compile` target providing `JsInfo` and `TsDeclarationInfo` |
| `package_json` | `label` | required | `package.json` template file |
| `version` | `string` | `""` | If set, overrides the `version` field in `package.json` |

## package.json Template

The template is read as JSON and re-emitted as pretty-printed JSON, so its
formatting is not preserved.

Three fields, `main`, `types` and `exports`, are filled in only when the
template does not declare them, from the entry point the rule identifies
(`index.js`/`index.d.ts`, or the single `.js` output when there is exactly one).
A declared value is kept verbatim; an empty one (`"main": ""`) suppresses the
fill.

`version` is different: when the rule's `version` attr is set it replaces
whatever the template has. Leave the attr at `""` to keep the template's.

A template can be as short as this:

```json
{
  "name": "@myorg/my-lib",
  "version": "0.0.0"
}
```

which publishes as:

```json
{
  "name": "@myorg/my-lib",
  "version": "1.2.3",
  "main": "./index.js",
  "types": "./index.d.ts",
  "exports": {
    ".": {
      "import": "./index.js",
      "types": "./index.d.ts"
    }
  }
}
```
