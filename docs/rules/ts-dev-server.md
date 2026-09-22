# ts_dev_server

Starts a dev server for a TypeScript application. The server transforms
first-party source in memory, so Bazel is out of the edit-to-browser loop;
`bazel-bin` supplies what it cannot produce itself (`ts_codegen` output, the
npm tree, assets, data srcs, passthrough `.d.ts`).

oj 0.2.5 is the default implementation at `@rules_typescript//oj:dev_server`.
Set `server = "@rules_typescript//vite:dev_server"` to use Vite. Other
implementations return `DevServerInfo`. See
[Bringing Your Own Server](../guides/dev-server.md#bringing-your-own-server).

The dev server does not type-check; type errors come from the editor and
`bazel build`.

Gazelle generates a `dev` target for a tsconfig program with non-test sources
and an application entry: a package index.html or a listed main.ts[x] or
app.ts[x]. Gazelle manages `entry_point`, `node_modules`, `plugin` and
`visibility`, and removes stale generated dev targets. It preserves `server`,
`port`, `host`, `open` and values protected by `# keep`.

oj provides native React Fast Refresh. Leave `react_refresh` false on the oj
path. Its serve root and cache location come from the launcher.

## Usage

This example selects Vite explicitly. Omit `server` to use oj.

```python
load("@rules_typescript//ts:defs.bzl", "ts_dev_server")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "dev_node_modules",
    deps = ["@npm//:vite"],
    hoist = "//:node_modules/.pnpm/node_modules",
)

ts_dev_server(
    name = "dev",
    server = "@rules_typescript//vite:dev_server",
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

The server runs from the package containing `ts_dev_server` and serves its
`index.html` at `/`. The `entry_point` may belong to another package. Bazel
output paths and npm resolution remain relative to the workspace.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start. An analysis-time error against a server whose provider lists `server.open` in `ignored_config_fields` |
| `node_modules` | `label` | `None` | The importer's [`node_modules`](node-modules.md) target linking the application's runtime deps, plus Vite on the Vite path; also what makes a bare npm import resolve; see [npm Resolution](#npm-resolution) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs` file. It resolves generated code out of `bazel-bin`, invalidates on a rebuild, and makes the restart decision. Without it `bazel-bin` is invisible to Vite |
| `server` | `label` | `@rules_typescript//oj:dev_server` | `DevServerInfo`-providing target choosing the implementation; see [Dev Server](../guides/dev-server.md#bringing-your-own-server) |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps; the dev server fails to start if the plugin cannot be loaded. An analysis-time error against a server whose provider sets `native_react_refresh` |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside it so its relative imports resolve. A file outside the config's package is an analysis-time error |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. A framework's Vite plugin runs in the dev server this way. Loaded from a copy in `bazel-bin`, which bounds what it may import |

## npm Resolution

The following resolution details describe the Vite implementation.

Starting the server links the importer's `node_modules` in at the workspace
root and removes the link on Ctrl-C, so a bare specifier resolves by the
ordinary walk up from the importer, and a package's own imports from its
realpath in the store. SSR externalisation and `optimizeDeps.include` resolve
without going through the plugin container, so they need the link. A generated
`bazel:npm-resolve` plugin at `enforce: 'post'` covers importers the walk cannot
reach. Exports maps, conditions and subpaths stay Vite's. A package the importer
does not link produces Vite's `Failed to resolve import`; the fix is adding it
to the `node_modules` target's `deps`.

An existing `node_modules` is never replaced: a real directory or a link to
another one is an error naming both. In `.gitignore`, `node_modules` without a
trailing slash matches the symlink; `node_modules/` matches directories only.

## `vite_config` Imports

Select the Vite server for Vite plugins and framework configuration.

The rule loads a copy of the file in `bazel-bin`, so its own imports resolve
beside the importer's `node_modules`. A bare npm specifier resolves through the
links of the `node_modules` target, provided that target is in the same Bazel
package as the dev server. A relative import resolves only if the module is declared in
`vite_config_srcs`; otherwise the server exits with
`[rules_typescript] Failed to load vite_config` naming the file.
`//tests/dev_server:vite_config_boundary_test` pins this; details in
[Dev Server](../guides/dev-server.md#vite_config-what-it-may-import).

## Restarts

The plugin-driven restart behavior below describes the Vite implementation.

One server process lives across every rebuild: `ibazel` SIGTERMs the launcher
and the launcher survives it. With `plugin` set, the restart decision is made
in that process, by comparing content digests of the inputs the generated
config was built from. A source edit and a `ts_codegen` rebuild do not restart
it; a change to the generated config, the npm tree or the toolchain node binary
does. See
[Dev Server](../guides/dev-server.md#watch-mode-with-ibazel).

## Edit-to-HMR Latency

For the Vite implementation, the budget is 500 ms from save to browser update.
`//tests/dev_server:{dev,dev_with_plugin}_hmr_latency_test` measures the
server's share of it by holding a WebSocket open as a browser would and saving a
file. The browser's own re-execution is outside the measurement. The suite
asserts that the median stays inside the budget and logs the distribution on
every run. See
[Dev Server](../guides/dev-server.md#edit-to-hmr-latency) for those logs and how
to run a longer sample.

## Diagnostics

```bash
bazel run //src/app:dev -- --dump-config
```

Prints the resolved launcher config (the selected server, runtime and
runfiles paths) and exits without starting the server.

See [Dev Server](../guides/dev-server.md) for the full guide.
