# ts_dev_server

Starts a dev server for a TypeScript application. The server transforms
first-party source in memory, so Bazel is out of the edit → browser loop;
`bazel-bin` supplies only what the server cannot produce itself (`ts_codegen`
output, the npm tree, assets, passthrough `.d.ts`).

Which server that is comes from the `server` attr — Vite by default, oj as the
second shipped implementation — and one generated config drives either. See
[Choosing the server](#choosing-the-server).

The dev server **does not type-check** — that is native Vite parity, and it
makes your editor and `bazel build` the things that report type errors.

## Usage

```python
load("@rules_typescript//ts:defs.bzl", "ts_dev_server")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
    port = 5173,
    plugin = "@rules_typescript//vite:vite_plugin_bazel",
)
```

```bash
bazel run //src/app:dev
ibazel run //src/app:dev   # codegen rebuilds and config-aware restarts
```

The app is then at `http://localhost:5173/src/app/` — Vite's root is the
workspace root, so the URL carries the package path and `/` is a 404.

Two things the target will not run without. `node_modules` has no default, and
Gazelle generates the rule without it, so a freshly generated target exits with
`ts_dev_server: … has no node_modules attr`. And that target has to be *named*
`node_modules`: the tree is materialised at its own name, and Vite 8's entry
imports `rolldown` by bare specifier, which Node answers only from a directory
with that name ([detail](../guides/dev-server.md#setup)).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start |
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and the application's runtime deps. Also what makes a bare npm import resolve at all — see [npm resolution](#npm-resolution) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs` file. It resolves generated code out of `bazel-bin`, invalidates precisely on a rebuild, and makes the restart decision. Without it `bazel-bin` is invisible to Vite |
| `server` | `label` | `//vite:dev_server` | Which implementation serves this target, as a `DevServerInfo`-providing target. `@rules_typescript//oj:dev_server` selects oj — see [Choosing the server](#choosing-the-server) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a non-Vite dev server. The Vite path resolves Vite from `node_modules` and does not need it |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps; the dev server fails to start if the plugin cannot be loaded |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. This is how a framework plugin runs in the dev server; SvelteKit's and Solid Start's [cannot go through it at all](../gazelle/overview.md#framework-detection), and TanStack Start's loads but the dev server [does not serve yet](../guides/dev-server.md#the-tanstack-start-dev-server-does-not-work-yet). Loaded from a copy in `bazel-bin`, which bounds what it may import |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside the copy so its relative imports resolve. A file outside the config's own package is an analysis-time error |

## npm resolution

A bare specifier in dev-served source resolves through the `node_modules` tree,
via a generated `bazel:npm-resolve` plugin at `enforce: 'pre'`. Vite has no
search-path option — it walks up from the importer looking for a `node_modules`
directory, and above a checked-in source file there is none, because the tree is a
Bazel output elsewhere. The plugin locates `<tree>/<package>/package.json` and
hands the id back to Vite's own resolver anchored there, so exports maps,
conditions and subpaths are interpreted by Vite rather than reimplemented by the
rule. A package the tree does not carry produces Vite's ordinary
`Failed to resolve import`; add it to the `node_modules` target's `deps`.

## CSS modules

A `*.module.css` served here has to carry the class names `css_module` already
generated a `.d.ts` from, or the dev loop and the build disagree about every one
of them. Every generated config installs
`//ts/private/css:css_module_vite_plugin`, which hands Vite the naming function
`css_module` used. There is no export map to read here — the server serves the
source tree, and `<source>.exports.json` is a `bazel-bin` output — so the name is
recomputed from the stylesheet's bytes. That is a pure function of exactly those
bytes, so it lands on the same name; editing the stylesheet moves the served name
and the next build's map together, and what goes stale in between is the `.d.ts`
key set, which is what "the dev server does not typecheck" already means.

`//tests/dev_server:dev_css_module_test` pins it: the names a running server
serves are compared against the export map the rule wrote.

Under **oj** this does not work, and the gap is pinned rather than papered over.
oj loads the plugin — its host reports `rules-typescript:css-modules[pre]`
active — but adopts no `css.*` from a Vite config, so the `generateScopedName`
the plugin's `config()` hook returns is dropped and oj's own CSS-modules pass
names the classes after the **file**: `panel-module_panel_4JHZ6q` where the
`.d.ts` was generated from `_panel_ced6ef19`. The property *set* is still right
— oj reads the same stylesheet — so a `styles.panel` type-checks and resolves;
the string it resolves to is another server's, and it is path-derived, which is
the dependence `css_module`'s content hash exists to avoid.
`//tests/dev_server:dev_oj_css_module_test` asserts exactly that, so oj gaining
the capability fails the lane and says to delete the branch.

Closing it needs either oj to adopt `css.modules`, or the ruleset to emit the
scoped stylesheet itself and take the naming away from the server — which would
put Bazel back in the dev loop this rule exists to keep it out of.

## Choosing the server

`server` takes a `DevServerInfo`, so the implementation is a per-target choice
and swapping it back is the same one-line edit:

```python
ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",   # oj needs no @npm//:vite in here
    server = "@rules_typescript//oj:dev_server",
)
```

**One generated config drives either server.** That is what makes the swap cheap:
oj adopts Vite's config format, so nothing about the target changes but the attr.
The provider carries what cannot be papered over — where the served directory
comes from (oj takes it from argv, not from `root`), whether the executable is a
`File` or a path inside the npm tree, whether the server applies Fast Refresh
itself, and the config fields it does not read.

That last field is enforced: a target whose configuration depends on a field the
selected server ignores is an **analysis-time error** naming both, rather than a
server that quietly does something else. `open = True` against oj fails, and so
does `react_refresh = True` — oj applies Fast Refresh itself, so stacking
`@vitejs/plugin-react` on top would instrument every component twice.

oj is a native binary built from a crate `MODULE.bazel` pins, at 0.1.6 and with no
patch. Earlier versions served a module only when a plugin `load` hook returned its
contents, so a resolver plugin — which maps a bare specifier to a path and leaves
the contents to the server — got a 404 for every module it resolved correctly;
Rollup's contract is that a `resolveId` result naming a real file *is* the module
and a `load` returning nothing means read it from disk. Reaching a `node_modules`
tree outside the app root is exactly what a resolver plugin is for, so without that
half `import "react"` does not resolve under oj at all. Fixed upstream in
[raphamorim/oj#108](https://github.com/raphamorim/oj/pull/108).

Details, and the HMR numbers for both:
[Dev Server § Choosing the server](../guides/dev-server.md#choosing-the-server).

## What a `vite_config` may import

The rule loads a **copy of the file in `bazel-bin`**, not your source file, so its
own imports resolve beside the Bazel npm tree instead of in the source tree. A
bare npm specifier resolves through the tree the `node_modules` attr built,
provided that target is in the same Bazel package as the dev server. A relative
import resolves only when the module is declared in `vite_config_srcs`, which is
what stages it beside the copy; an undeclared sibling is not there, and the server
exits with `[rules_typescript] Failed to load vite_config` naming the file.
Pinned by `//tests/dev_server:vite_config_boundary_test`; details in
[Dev Server](../guides/dev-server.md#vite_config-what-it-may-import).

Of the loaded object, this rule reads `plugins` and nothing else. Any other key
fails the load naming itself — including `root`, which `ts_bundle` does read, so a
config shared between the two has to leave it out. See
[Bundling § Keys the generated config reads](../guides/bundling.md#keys-the-generated-config-reads).

## Restarts

One server process lives across every rebuild: `ibazel` SIGTERMs the launcher and
the launcher deliberately survives it, so the restart decision is made in the
process, by comparing content digests of the inputs the generated config was
built from. A source edit and a `ts_codegen` rebuild do not restart; a change to
the generated config, the npm tree or the toolchain node binary does. See
[Dev Server](../guides/dev-server.md#watch-mode-with-ibazel-and-who-decides-to-restart).

## Edit-to-HMR latency

The goal is under 500 ms from save to browser update, and
`//tests/dev_server:{dev,dev_with_plugin,dev_oj}_hmr_latency_test` measures the
server's share of it by holding a WebSocket open as a browser would and saving a
file: single-digit milliseconds under Vite, low double digits under oj. The
suite asserts only that the median stays inside the whole budget. See
[Dev Server](../guides/dev-server.md#edit-to-hmr-latency) for the numbers and how
to run a longer sample.

## Diagnostics

```bash
bazel run //src/app:dev -- --dump-config
```

Prints the resolved launcher config — the node binary, the vite entry, the
runfiles paths — and exits without starting the server.

See [Dev Server](../guides/dev-server.md) for the full guide.
