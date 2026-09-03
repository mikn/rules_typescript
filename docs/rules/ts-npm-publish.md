# ts_npm_publish

Assembles a publishable npm package from a `ts_compile` target. Collects `.js`,
`.js.map`, and `.d.ts` outputs, merges them with a `package.json` template, and
produces a staging directory and a tarball ready for `npm publish`.

`main`, `types` and `exports` are auto-filled from the entry point when the
template does not declare them; `version` is replaced when the attr is set. See
[Publishing Packages](../guides/publishing.md#packagejson-template).

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
| `<name>_pkg/package/` | Staging directory; the `package/` level is what npm expects inside the tarball |
| `<name>_pkg.tar` | Tarball with `package/` prefix (ready for `npm publish`) |

## Provider

The target returns `NpmPublishInfo`, loaded from
`@rules_typescript//ts:defs.bzl`:

| Field | Type | Description |
|-------|------|-------------|
| `pkg_dir` | `File` | The `<name>_pkg/package/` directory |
| `tarball` | `File` | `<name>_pkg.tar`. Every build produces it |
| `package_json` | `File` | The final `package.json` inside the directory |

`DefaultInfo` carries the directory and the tarball. See
[Providers](providers.md#npmpublishinfo).

See [Publishing Packages](../guides/publishing.md) for the full guide.
