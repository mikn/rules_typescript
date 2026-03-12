# ts_dev_server

Starts a Vite development server serving compiled JavaScript from `bazel-bin`. Designed to be used with `ibazel` for watch-mode development with HMR.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_dev_server")

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    port = 5173,
    plugin = "@rules_typescript//vite:vite_plugin_bazel",
)
```

```bash
ibazel run //src/app:dev
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start |
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and other runtime deps |
| `plugin` | `label` | `None` | Compiled `.mjs` file for `vite-plugin-bazel` (enables HMR with ibazel) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target (optional; dev mode uses Vite natively) |

See [Dev Server](../guides/dev-server.md) for the full guide.
