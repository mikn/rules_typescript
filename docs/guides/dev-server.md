# Dev Server

`ts_dev_server` starts a Vite development server that serves compiled JavaScript from `bazel-bin`. Designed to be used with `ibazel` for watch-mode development.

## Setup

```python
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_dev_server")

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

## How It Works

The `plugin` attribute wires `vite-plugin-bazel`, which intercepts `.ts` import resolution and redirects to compiled `.js` files in `bazel-bin`. When run with `ibazel`, the plugin triggers component-level HMR updates on each rebuild instead of full-page reloads.

**Gazelle** automatically sets `plugin = "@rules_typescript//vite:vite_plugin_bazel"` when generating a new `ts_dev_server` target. The attribute is only set on first generation and can be freely removed if you do not use Vite.

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

## Watch Mode with ibazel

Install ibazel:

```bash
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest
```

Run the dev server with file watching:

```bash
ibazel run //src/app:dev
```

ibazel rebuilds affected `ts_compile` targets on every source change, then signals Vite's HMR socket to reload the changed modules in the browser without a full page reload.
