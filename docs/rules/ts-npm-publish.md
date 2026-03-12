# ts_npm_publish

Assembles a publishable npm package from a `ts_compile` target. Collects `.js`, `.js.map`, and `.d.ts` outputs, merges them with a `package.json` template, and produces a staging directory and a tarball ready for `npm publish`.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_npm_publish")

ts_npm_publish(
    name = "lib_pkg",
    package = ":lib",
    package_json = ":package.json",
    version = "1.2.3",
)
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `package` | `label` | required | `ts_compile` target providing `JsInfo` and `TsDeclarationInfo` |
| `package_json` | `label` | required | `package.json` template file |
| `version` | `string` | `""` | If set, overrides the `version` field in `package.json` |

## Outputs

| Output | Description |
|--------|-------------|
| `<name>_pkg/` | Staging directory with all files at package root |
| `<name>_pkg.tar` | Tarball with `package/` prefix (ready for `npm publish`) |

See [Publishing Packages](../guides/publishing.md) for the full guide.
