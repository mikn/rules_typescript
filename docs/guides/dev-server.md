# Dev Server

`ts_dev_server` starts a dev server for a TypeScript application. `bazel run
//src/app:dev` builds the target once. The server then transforms first-party
source in memory, so a save reaches the browser without a Bazel build.

Vite is the default implementation and oj is selected per target; both read the
same generated Vite config. See [Choosing the server](#choosing-the-server).

## Setup

Gazelle generates the `ts_dev_server` target next to the `ts_compile` it serves,
with `plugin` set and `node_modules` empty; nothing in the source tree says
which tree the app resolves against. The first `bazel run` then stops before
Vite starts:

```
ts_dev_server: @@//src/app:dev has no node_modules attr, so the app's own
dependencies are not in runfiles.
```

Add the tree once; Gazelle leaves the attr alone from then on.

```python
load("@rules_typescript//ts:defs.bzl", "ts_dev_server")
load("@rules_typescript//npm:defs.bzl", "node_modules")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        # every npm package the app imports, too — see below
    ],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
    port = 5173,
    plugin = "@rules_typescript//vite:vite_plugin_bazel",
)
```

Vite itself comes from that tree; the rule does not fetch it.

```bash
bazel run //src/app:dev     # start it
ibazel run //src/app:dev    # same, plus codegen rebuilds and config-aware restarts
```

Open the app at its package path: the serve root is the workspace root, so
`http://localhost:5173/` is a 404 and the app is one directory deeper.

```
http://localhost:5173/src/app/          # the package holding index.html
```

## Choosing the Server

`ts_dev_server` takes a `DevServerInfo`, and the implementation is a per-target
choice. oj ([raphamorim/oj](https://github.com/raphamorim/oj), a Rust-native
build tool) is the second shipped one:

```python
ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",   # oj needs no @npm//:vite in here
    server = "@rules_typescript//oj:dev_server",
)
```

oj has no npm package and no release binary. `MODULE.bazel` pins the crate at
a git revision (upstream v0.1.8 plus the commits its comment lists) through its
`oj_crates` extension, and Bazel builds it from source, so the first build of a
target selecting it is a Rust compile. The binary is native; the toolchain Node
is still on PATH, since oj's plugin host is a Node process.

The provider declares two differences. oj takes the directory it serves from a
positional argument, not from the config's `root`. A field one server does not
read is an analysis-time error on a target that sets the attr reaching it:
`open = True` against oj fails naming both. `react_refresh = True` fails the
same way; oj applies Fast Refresh itself, and a second instance would instrument
every component twice.

!!! note "oj 0.1.6"
    Until oj 0.1.6, `oj_server` served a module only when a plugin `load` hook
    returned its contents. The resolver plugin maps a bare specifier to a path in
    the Bazel tree and leaves the contents to the server, so it got a 404 for
    every module it resolved correctly, and `import "react"` did not resolve under
    oj. Rollup's contract is that a `resolveId` result naming a real file is
    the module, and a `load` returning nothing means read it from disk. Fixed
    upstream in
    [raphamorim/oj#108](https://github.com/raphamorim/oj/pull/108), released as
    0.1.6; the revision `MODULE.bazel` pins is past it.

Bringing your own is a rule returning `DevServerInfo`. A server shipping as an
npm package sets `server_in_tree` (a path inside the `node_modules` tree, since a
file inside a TreeArtifact has no label at analysis time); a native binary sets
`server_binary`. Exactly one. The provider is not exported from
`@rules_typescript//ts:defs.bzl`: it loads from
`@rules_typescript//ts/private:providers.bzl`, a path
[COMPATIBILITY.md](../compatibility.md#volatile) lists as volatile. Its eight
fields, with the values the two shipped servers return for each, are in
[Providers](../rules/providers.md#devserverinfo).

## What Is Served from Where

| | `ts_bundle` (`vite build`) | `ts_dev_server` |
|---|---|---|
| first-party `.ts` | Bazel compiles it; the plugin redirects imports to `bazel-bin` | served as source, transformed by the server in memory |
| `ts_codegen` output | from `bazel-bin` | from `bazel-bin` |
| npm packages | the `node_modules` tree | the `node_modules` tree, linked in at the workspace root |
| assets, passthrough `.d.ts` | from `bazel-bin` | from `bazel-bin` |

Generated code is recognised by the absence of a checked-in source file.

Each first-party `module_name` in the graph becomes a `resolve.alias` entry
pointing at that package's source, so `import "@acme/ui"` and a relative import
of the same file are one module in Vite's graph. The mapping is the same
`TsModuleInfo` that `ts_compile` writes into its tsconfig `paths`.

### How a Bare npm Specifier Resolves

Vite has no search-path option: it resolves `import "zod"` by walking up from the
importer looking for a `node_modules` directory, and above a checked-in source
file there is none; the npm tree is a Bazel output elsewhere. (`resolve.modules`
is a webpack option; Vite ignores it.)

The launcher links the tree in as `<workspace>/node_modules` when the dev server
starts, and removes the link on Ctrl-C. Every resolver then finds the packages
by that walk, including the two no plugin reaches:

- **SSR externalisation.** Whether a package is external is decided on the raw
  specifier before the plugin container sees it. A package that does not resolve
  is treated as not-external and inlined, so a CJS entry like
  `react/jsx-runtime` is evaluated as ESM: `module is not defined`, on every
  request.
- **`optimizeDeps.include`.** Resolved with no importer, walking up from `root`.
  A framework plugin names the dependencies the browser needs pre-bundled from
  CJS here.

An existing `node_modules` is never replaced. A real directory (a `pnpm install`)
or a link to a different tree stops the dev server with a message naming it. Two
npm trees cannot both be at the workspace root, so two dev servers using
different `node_modules()` targets cannot run at once.

Add `node_modules` to `.gitignore` without a trailing slash: `node_modules/`
matches a directory, and this is a symlink.

A plugin, `bazel:npm-resolve`, stays behind it at `enforce: 'post'` as a
fallback: it locates `<tree>/<package>/package.json` and hands the id back to the
resolver anchored there, for an importer the walk cannot reach and for a server
that does no walk of its own. Exports maps, conditions and subpaths stay the
resolver's either way, so `import "zod/v4"` and a conditional `exports` behave in
dev as they do in a `ts_bundle`.

A package the tree does not carry produces Vite's `Failed to resolve import` at
the moment the browser asks for the module; add it to the `node_modules` target's
`deps`.

oj serves from the same generated config and the same link.

## Type Checking

The dev server does not type-check; neither does `vite dev`. Type errors come
from the editor and `bazel build`, and do not block the browser update. See
[IDE integration](../getting-started/ide-setup.md).

## CSS Modules

Under Vite, a `*.module.css` served by the dev server carries the same class
names `css_module` generated its `.d.ts` from. The dev server installs the
CSS-modules plugin unconditionally, with no attribute and no `vite_config`, so
`styles.button` in a served module is the string the `.d.ts` declares and the
string a `ts_test` asserts on.

Serving a source tree, there is no `<file>.exports.json` beside the stylesheet, so
the name is recomputed; it is a pure function of the same bytes and lands on the
same answer. See
[css_module](../rules/css-and-assets.md#what-the-declarations-promise).

Under oj the names differ. oj loads the plugin but adopts no `css.*` from the
config, so the `generateScopedName` the plugin's `config()` hook returns is
dropped and oj's own CSS-modules pass names each class after the file
(`panel-module_panel_…`). The keys match the `.d.ts`; the strings do not.
`//tests/dev_server:dev_oj_css_module_test` pins the gap.

A plugin in a `vite_config` that sets `css.modules.generateScopedName` or
`css.modules = false` fails naming the `css_module` attribute to use instead, in
the dev server as in a bundle. A `css` key in the config itself fails earlier,
as a key the generated config does not read. A framework plugin that resolves
the config once per environment does not trip the check.

## vite-plugin-bazel

The `plugin` attribute wires `vite-plugin-bazel`, which:

- resolves generated code out of `bazel-bin`; without it every `ts_codegen`
  output is invisible to Vite;
- invalidates on a rebuild, so a codegen change arrives as an HMR update;
- makes the restart decision under
  [Watch Mode with ibazel](#watch-mode-with-ibazel).

Gazelle sets `plugin = "@rules_typescript//vite:vite_plugin_bazel"` on the first
generation of a `ts_dev_server` target only; it can be removed.

Nothing in `//tests/dev_server` starts oj with it.

## React Fast Refresh

`react_refresh = True` loads `@vitejs/plugin-react`, which preserves component
state across an HMR update. The package has to be in the `node_modules` tree:

```python
node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:vitejs_plugin-react",
    ],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
    react_refresh = True,
)
```

The entry point comes from the package's own `exports` map, so a `dist/`
reorganisation between majors does not move it. If the plugin cannot be loaded,
the dev server fails to start naming the target and the dep to add.

The attribute is Vite's: `react_refresh = True` against oj is an analysis-time
error, since oj applies Fast Refresh itself.

`@vitejs/plugin-react` finds its `react-refresh` runtime by the same walk-up Vite
uses for `rolldown`, so the target has to be [named `node_modules`](#setup).

## `vite_config`: What It May Import

`vite_config` takes one `.ts`, `.mts`, `.mjs` or `.js` file default-exporting
`{plugins: [...]}`, whose plugins are prepended to Bazel's. A framework plugin
runs in the dev server through it: TanStack Start's does (see
[below](#tanstack-start)); SvelteKit's and Solid Start's
[do not](../gazelle/overview.md#framework-detection).

The rule loads a copy of the file in `bazel-bin`: Node resolves a runfiles
symlink before the file's own imports, so a source-tree config would resolve
them through a source-tree `node_modules`, which this ruleset does not have.
`//tests/dev_server:vite_config_boundary_test` pins the bare import and the
undeclared relative one;
`//tests/dev_server:dev_with_composed_user_config_behaviour_test` pins the
declared one:

- A bare npm specifier resolves through the tree the `node_modules` attr built.
  That target must be in the same Bazel package as the dev server, the directory
  Node finds walking up from the copy.
- A relative import resolves only if the module is declared in
  `vite_config_srcs`, which stages it beside the copy. An undeclared sibling is
  not there, and the dev server exits with
  `[rules_typescript] Failed to load vite_config: …` naming the file.

TypeScript works, as do the extensionless relative specifiers a
bundler-resolution config is written with, because the generated config loads the
file through Vite's own `loadConfigFromFile`.

`ts_bundle` stages its config the same way, so the two attrs accept the same
imports. They differ in what they read from it: `ts_dev_server` reads `plugins`
only, `ts_bundle` reads `plugins` and `root`, and any other key fails the load
naming itself. A config carrying `root` builds under `ts_bundle` and fails under
the dev server. See
[bundling](bundling.md#keys-the-generated-config-reads).

## Watch Mode with ibazel

```bash
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest
ibazel run //src/app:dev
```

`ibazel run` SIGTERMs the launcher after every rebuild and the launcher survives
it, so one Vite process lives across every rebuild. The restart-or-keep decision
is made inside that process:

| What changed | Handled by | Restarts Vite? |
|---|---|---|
| first-party source | Vite transform → HMR | no; Bazel is not involved |
| `ts_codegen` output | the plugin's `bazel-bin` watcher | no |
| the generated Vite config | `ConfigWatcher` | yes |
| the npm tree, or the Vite version in it | `ConfigWatcher` | yes, with a warning |
| the toolchain node binary | `ConfigWatcher` | yes, with a warning |

The generated config exports `bazelConfigInputs`: for each input, a path, its
content digest, and whether an in-process restart can fix a change to it. The
digest is over content, because Bazel rewrites outputs on every action and an
mtime says nothing. Only a new `bazel run` replaces a node binary or an npm tree,
hence the warning on the last two rows.

Vite restarts on a change to its own config file but has no concept of the thing
that generates it; `ConfigWatcher` watches those inputs.

Both watchers are `vite-plugin-bazel`'s, so a target without the `plugin` attr
keeps its server process across rebuilds and compares no digests.

## Edit-to-HMR Latency

The design goal is under 500 ms from save to browser update:

```
save ──▶ watcher notices ──▶ transform ──▶ HMR frame ──▶ browser re-executes
         └───────────────── measured ──────────────┘     └── not measured ──┘
```

`//tests/dev_server:{dev,dev_with_plugin,dev_oj}_hmr_latency_test` measures the
left-hand side: each starts its `ts_dev_server` as `bazel run` does, holds a
WebSocket open on the server's HMR endpoint as a browser would, saves a file,
and times the frame that comes back and the fetch that follows it.

```bash
# the numbers, on every run
bazel test //tests/dev_server:dev_hmr_latency_test --test_output=all --test_arg=-test.v

# a longer sample, for comparing a change against main
bazel test //tests/dev_server/... --test_env=HMR_ITERATIONS=100 \
    --test_output=all --test_arg=-test.v
```

The tests run in the ordinary suite. The only assertion is that the median stays
inside the whole 500 ms budget, some forty times the observed median. At that
ceiling what trips it is HMR falling back to a rebuild, a watcher gone to
polling, or the transform moving off the warm path.

Each run logs what it measured: min, median, p90 and max for both halves of the
loop, the cold first edit on its own, and which HMR message the server chose. The
fixture is two modules and the sample is twelve saves; `HMR_ITERATIONS` sets the
count. The three targets on one machine compare Vite, Vite with the plugin, and
oj.

The HMR message differs by server. Vite treats an explicit
`import.meta.hot.accept()` as a boundary and sends a scoped update. oj applies
React Fast Refresh itself and picks its own boundaries. The `what the server
sent` line in the log says which message arrived.

!!! note "Two saves inside 50 ms"
    Vite's watcher (chokidar) drops a second change to the same path within
    50 ms of the one it emitted; it is not deferred, it never arrives. A script
    that writes in a loop appears to hang. The benchmark spaces its samples for
    this reason.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start. Vite only: `True` against oj is an analysis-time error |
| `node_modules` | `label` | `None` | `node_modules` target providing the application's runtime deps, plus Vite on the Vite path; also what makes a bare npm import resolve; see [above](#how-a-bare-npm-specifier-resolves) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs`; see [above](#vite-plugin-bazel) |
| `server` | `label` | `@rules_typescript//vite:dev_server` | `DevServerInfo`-providing target choosing the implementation. `@rules_typescript//oj:dev_server` selects oj; see [above](#choosing-the-server) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a custom dev server that needs a bundler binary in runfiles. Neither shipped server does |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`; requires `@npm//:vitejs_plugin-react` in the `node_modules` deps, and fails against oj; see [above](#react-fast-refresh) |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside it. A file outside the config's package is an analysis-time error |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins; see [above](#vite_config-what-it-may-import) |

## TanStack Start

Start's plugin loads through `vite_config` and the bundle builds
(`//tests/integration:tanstack_test`). Gazelle generates the Start dev server at
the workspace root beside the `ts_bundle`, since it needs the same
`vite_config`. Serving it depends on the `<workspace>/node_modules` link the
launcher makes: Vite's SSR module runner decides whether `react/jsx-runtime` is
external on the raw specifier, walking up from the importer, and without the link
it inlines React's CJS entry, which evaluates `module` in an ESM context and
answers every request with 500. `examples/tanstack-app/README.md` covers the
example, under Vite and under oj.

## Diagnostics

The launcher uses no host `node` or `vite`. A missing JS runtime toolchain fails
at analysis time; a missing `node_modules`, or a missing vite on the Vite path,
fails the launcher with a message. To see what it resolved:

```bash
bazel run //src/app:dev -- --dump-config
```
