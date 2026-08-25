# ts_dev_server

Starts a Vite dev server for a TypeScript application. Vite transforms
first-party source in memory, so Bazel is out of the edit → browser loop;
`bazel-bin` supplies only what Vite cannot produce itself (`ts_codegen` output,
the npm tree, assets, passthrough `.d.ts`).

The dev server **does not type-check** — that is native Vite parity, and it
makes your editor and `bazel build` the things that report type errors.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_dev_server")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "dev_node_modules",
    deps = ["@npm//:vite"],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":dev_node_modules",
    port = 5173,
    plugin = "@rules_typescript//vite:vite_plugin_bazel",
)
```

```bash
bazel run //src/app:dev
ibazel run //src/app:dev   # codegen rebuilds and config-aware restarts
```

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start |
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and the application's runtime deps |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs` file. It resolves generated code out of `bazel-bin`, invalidates precisely on a rebuild, and makes the restart decision. Without it `bazel-bin` is invisible to Vite |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a non-Vite dev server. The Vite path resolves Vite from `node_modules` and does not need it |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps |
| `vite_config` | `label` | `None` | A `.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. This is how framework plugins run in the dev server — TanStack Start's and Remix's do; SvelteKit's and Solid Start's [cannot](../gazelle/overview.md#framework-detection) |

## Restarts

One Vite process lives across every rebuild: `ibazel` SIGTERMs the launcher and
the launcher deliberately survives it, so the restart decision is made in the
process, by comparing content digests of the inputs the generated config was
built from. A source edit and a `ts_codegen` rebuild do not restart; a change to
the generated config, the npm tree or the toolchain node binary does. See
[Dev Server](../guides/dev-server.md#watch-mode-with-ibazel-and-who-decides-to-restart).

## Diagnostics

```bash
bazel run //src/app:dev -- --dump-config
```

Prints the resolved launcher config — the node binary, the vite entry, the
runfiles paths — and exits without starting the server.

See [Dev Server](../guides/dev-server.md) for the full guide.
