# ts_dev_server

Starts a dev server for a TypeScript application. The server transforms
first-party source in memory, so Bazel is out of the edit-to-browser loop;
`bazel-bin` supplies what it cannot produce itself (`ts_codegen` output, the
npm tree, assets, passthrough `.d.ts`).

Vite is the default implementation;
`server = "@rules_typescript//oj:dev_server"` picks oj for that target. Both
read the same generated Vite config; see
[Choosing the Server](../guides/dev-server.md#choosing-the-server).

The dev server does not type-check; type errors come from the editor and
`bazel build`.

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
| `open` | `bool` | `False` | Open the browser automatically on start. Vite only: `True` against oj is an analysis-time error |
| `node_modules` | `label` | `None` | `node_modules` target providing the application's runtime deps, plus Vite on the Vite path; also what makes a bare npm import resolve; see [npm Resolution](#npm-resolution) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs` file. It resolves generated code out of `bazel-bin`, invalidates on a rebuild, and makes the restart decision. Without it `bazel-bin` is invisible to Vite |
| `server` | `label` | `@rules_typescript//vite:dev_server` | `DevServerInfo`-providing target choosing the implementation. `@rules_typescript//oj:dev_server` selects oj; see [Dev Server](../guides/dev-server.md#choosing-the-server) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a custom dev server that needs a bundler binary in runfiles. Neither shipped server does |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps; the dev server fails to start if the plugin cannot be loaded. An analysis-time error against oj, which applies Fast Refresh itself |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside it so its relative imports resolve |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. A framework plugin runs in the dev server this way; SvelteKit's and Solid Start's [cannot](../gazelle/overview.md#framework-detection); TanStack Start's both bundles and serves. Loaded from a copy in `bazel-bin`, which bounds what it may import |

## npm Resolution

Starting the server links the `node_modules` tree in at the workspace root and
removes the link on Ctrl-C, so a bare specifier resolves by the ordinary walk up
from the importer. SSR externalisation and `optimizeDeps.include` resolve
without going through the plugin container, so they need the link. A generated
`bazel:npm-resolve` plugin at `enforce: 'post'` covers importers the walk cannot
reach. Exports maps, conditions and subpaths stay Vite's. A package the tree
does not carry produces Vite's `Failed to resolve import`; the fix is adding it
to the `node_modules` target's `deps`.

An existing `node_modules` is never replaced: a real directory or a link to
another tree is an error naming both. In `.gitignore`, `node_modules` without a
trailing slash matches the symlink; `node_modules/` matches directories only.

## `vite_config` Imports

The rule loads a copy of the file in `bazel-bin`, so its own imports resolve
beside the Bazel npm tree. A bare npm specifier resolves through the tree the
`node_modules` attr built, provided that target is in the same Bazel package as
the dev server. A relative import resolves only if the module is declared in
`vite_config_srcs`; otherwise the server exits with
`[rules_typescript] Failed to load vite_config` naming the file.
`//tests/dev_server:vite_config_boundary_test` pins this; details in
[Dev Server](../guides/dev-server.md#vite_config-what-it-may-import).

## Restarts

One server process lives across every rebuild: `ibazel` SIGTERMs the launcher
and the launcher survives it. With `plugin` set, the restart decision is made
in that process, by comparing content digests of the inputs the generated
config was built from. A source edit and a `ts_codegen` rebuild do not restart
it; a change to the generated config, the npm tree or the toolchain node binary
does. See
[Dev Server](../guides/dev-server.md#watch-mode-with-ibazel).

## Edit-to-HMR Latency

The budget is 500 ms from save to browser update.
`//tests/dev_server:{dev,dev_with_plugin,dev_oj}_hmr_latency_test` measures the
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

Prints the resolved launcher config (the node binary, the vite entry, the
runfiles paths) and exits without starting the server.

See [Dev Server](../guides/dev-server.md) for the full guide.
