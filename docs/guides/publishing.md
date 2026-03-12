# Publishing Packages

`ts_npm_publish` assembles a publishable npm package from a `ts_compile` target. It collects `.js`, `.js.map`, and `.d.ts` outputs, merges them with a `package.json` template, and produces a staging directory and a tarball ready for `npm publish`.

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

Two outputs are produced:

| Output | Description |
|--------|-------------|
| `lib_pkg_pkg/` | Staging directory with all files at package root |
| `lib_pkg_pkg.tar` | Tarball with `package/` prefix (ready for `npm publish`) |

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

The `package.json` template should specify `"main"`, `"types"`, and `"exports"` fields pointing to the compiled output files. The rule does not modify these fields — they must already reference the correct filenames.

```json
{
  "name": "@myorg/my-lib",
  "version": "0.0.0",
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

When `version` is set on the rule, the `"version"` field in `package.json` is overwritten with the specified value. Set it to `""` to use whatever is in the template.
