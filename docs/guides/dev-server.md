# Dev Server

`ts_dev_server` starts a dev server for a TypeScript application. `bazel run
//src/app:dev` builds the target once and then leaves Bazel out of the inner
loop: the server transforms first-party source in memory, so a save reaches the
browser without a Bazel analysis-and-action cycle in between.

Vite is the default implementation and oj is selected per target; both read the
same generated Vite config. See [Choosing the server](#choosing-the-server).

## Setup

Gazelle generates the `ts_dev_server` target next to the `ts_compile` it serves,
with `plugin` set and `node_modules` empty — nothing in the source tree says which
tree the app resolves against. The first `bazel run` then stops before Vite
starts:

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
`=0.1.6` through its `oj_crates` extension and Bazel builds it from source, so
the first build of a target selecting it is a Rust compile. The binary is
native; the toolchain Node is still on PATH, since oj's plugin host is a Node
process.

The provider declares two structural differences. oj takes the directory it
serves from a positional argument, not from the config's `root`. And a field one
server does not read is an analysis-time error on a target that set the attr
reaching it: `open = True` against oj fails naming both. `react_refresh` is the
same — oj applies Fast Refresh itself, so setting it would instrument every
component twice.

!!! note "oj 0.1.6"
    Until oj 0.1.6, `oj_server` served a module only when a plugin `load` hook
    returned its contents. The resolver plugin maps a bare specifier to a path in
    the Bazel tree and leaves the contents to the server, so it got a 404 for
    every module it resolved correctly, and `import "react"` did not resolve under
    oj at all. Rollup's contract is that a `resolveId` result naming a real file is
    the module, and a `load` returning nothing means read it from disk. Fixed
    upstream in
    [raphamorim/oj#108](https://github.com/raphamorim/oj/pull/108); `MODULE.bazel`
    pins 0.1.6 and carries no patch.

Bringing your own is a rule returning `DevServerInfo`. A server shipping as an
npm package sets `server_in_tree` (a path inside the `node_modules` tree, since a
file inside a TreeArtifact has no label at analysis time); a native binary sets
`server_binary`. Exactly one.

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
file there is never one — the npm tree is a Bazel output elsewhere.
(`resolve.modules` is a webpack option; Vite ignores it.)

So the launcher puts the tree on that walk. Starting a dev server links it in as
`<workspace>/node_modules`, and removes the link on Ctrl-C. From there every
resolver finds the packages the way it would outside Bazel, including the two
that no plugin can reach:

- **SSR externalisation.** Whether a package is external is decided on the raw
  specifier before the plugin container ever sees it. A package that does not
  resolve is treated as not-external and inlined, so a CJS entry like
  `react/jsx-runtime` ends up evaluated as ESM — `module is not defined`, on
  every request.
- **`optimizeDeps.include`.** Resolved with no importer at all, walking up from
  `root`. This is what a framework plugin uses to name the dependencies the
  browser needs pre-bundled from CJS.

An existing `node_modules` is never replaced. A real directory (a `pnpm install`)
or a link to a different tree makes the dev server stop and say so rather than
pick one; two npm trees cannot both be at the workspace root, so two dev servers
using different `node_modules()` targets cannot run at once.

Add `node_modules` to `.gitignore` **without a trailing slash** — `node_modules/`
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

The dev server does not type-check, and neither does native `vite dev`. Type
errors come from your editor and `bazel build`, and no longer block the browser
update. Set up [IDE integration](../getting-started/ide-setup.md) if you relied
on a dev server to report them.

## CSS Modules

A `*.module.css` served by the dev server carries the same class names
`css_module` generated its `.d.ts` from. The dev server installs the CSS-modules
plugin unconditionally — no attribute, no `vite_config` — so `styles.button` in a
served module is the string the `.d.ts` declares and the string a `ts_test`
asserts on.

Serving a source tree, there is no `<file>.exports.json` beside the stylesheet, so
the name is recomputed; it is a pure function of the same bytes and lands on the
same answer. See
[css_module](../rules/css-and-assets.md#what-the-declarations-promise).

Setting `css.modules.generateScopedName` or `css.modules = false` in a
`vite_config` is a hard failure naming the `css_module` attribute to use instead,
as it is in a bundle. A framework plugin that resolves the config once per
environment is not mistaken for such an override.

## vite-plugin-bazel

The `plugin` attribute wires `vite-plugin-bazel`, which:

- resolves generated code out of `bazel-bin` (without it, `bazel-bin` — and so
  every `ts_codegen` output — is invisible to Vite);
- invalidates precisely on a rebuild, so a codegen change arrives as an HMR
  update;
- makes the restart decision described below.

**Gazelle** sets `plugin = "@rules_typescript//vite:vite_plugin_bazel"` on first
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

The entry point comes from that package's own `exports` map, which survives the
`dist/` reorganisations it makes between majors. If the plugin cannot be loaded
the dev server fails to start, naming the target and the dep to add.

The attribute is Vite's: `react_refresh = True` against oj is an analysis-time
error, since oj applies Fast Refresh itself.

`@vitejs/plugin-react` finds its `react-refresh` runtime by the same walk-up Vite
uses for `rolldown`, a second reason the target has to be
[named `node_modules`](#setup).

## `vite_config`: what it may import

`vite_config` takes one `.ts`, `.mts`, `.mjs` or `.js` file default-exporting
`{plugins: [...]}`, whose plugins are prepended to Bazel's. This is how a
framework plugin runs in the dev server: SvelteKit's and Solid Start's
[cannot go through it at all](../gazelle/overview.md#framework-detection), and
TanStack Start's loads but does not yet serve (see [below](#tanstack-start)).

The rule loads a copy of the file in `bazel-bin`: Node resolves a runfiles
symlink before that file's own imports, so a source-tree config would resolve its
imports through a source-tree `node_modules`, which this ruleset does not have.
The copy draws the boundary, and
`//tests/dev_server:vite_config_boundary_test` covers every side of it:

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
naming itself. A config carrying `root` therefore builds under `ts_bundle` and
fails under the dev server. See
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
| first-party source | Vite transform → HMR | no — Bazel is not involved at all |
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
left-hand side, with nothing stubbed: each starts its `ts_dev_server` as `bazel
run` does, holds a WebSocket open on the server's HMR endpoint as a browser
would, saves a file, and times the frame that comes back and the fetch that
follows it.

```bash
# the numbers, on every run
bazel test //tests/dev_server:dev_hmr_latency_test --test_output=all --test_arg=-test.v

# a longer sample, for comparing a change against main
bazel test //tests/dev_server/... --test_env=HMR_ITERATIONS=100 \
    --test_output=all --test_arg=-test.v
```

The tests run in the ordinary suite, and the only assertion is that the median
stays inside the whole 500 ms budget — some forty times what it measures today.
At that ceiling what trips it is HMR falling back to a rebuild, a watcher gone to
polling, or the transform moving off the warm path.

Each run logs what it measured: min, median, p90 and max for both halves of the
loop, the cold first edit on its own, and which HMR message the server chose. The
fixture is two modules and the sample is twelve saves; `HMR_ITERATIONS` sets the
count. Running the three targets on one machine is how Vite, Vite with the
plugin, and oj compare.

The HMR message differs by server. Vite treats an explicit
`import.meta.hot.accept()` as a boundary and sends a scoped update. oj applies
React Fast Refresh itself and picks its own boundaries. The `what the server
sent` line in the log says which message arrived.

!!! note "Two saves inside 50 ms"
    Vite's watcher (chokidar) drops a second change to the same path within
    50 ms of the one it emitted — it is not deferred, it never arrives. A script
    that writes in a loop will appear to hang; the benchmark spaces its samples
    for this reason.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start. Vite only: `True` against oj is an analysis-time error |
| `node_modules` | `label` | `None` | `node_modules` target providing the application's runtime deps, plus Vite on the Vite path; also what makes a bare npm import resolve — see [above](#how-a-bare-npm-specifier-resolves) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs` — see [above](#vite-plugin-bazel) |
| `server` | `label` | `@rules_typescript//vite:dev_server` | `DevServerInfo`-providing target choosing the implementation. `@rules_typescript//oj:dev_server` selects oj — see [above](#choosing-the-server) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a custom dev server that needs a bundler binary in runfiles. Neither shipped server does |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`; requires `@npm//:vitejs_plugin-react` in the `node_modules` deps, and fails against oj — see [above](#react-fast-refresh) |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside it |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins — see [above](#vite_config-what-it-may-import) |

## TanStack Start

Start's plugin loads through `vite_config` and the bundle builds
(`//tests/integration:tanstack_test`), but `bazel run` on a Start dev server does
not serve: Vite's SSR module runner inlines `react/jsx-runtime` out of the Bazel
npm tree instead of externalising it, and React's CJS entry then evaluates
`module` in an ESM context. Every request answers 500, and nothing in this rule
works around it. `bazel build` of the bundle is unaffected;
`examples/tanstack-app/README.md` has the trace.

## Diagnostics

The launcher never reaches for a host `node` or `vite`: a missing JS runtime
toolchain fails at analysis time, and a missing `node_modules` — or a missing
vite on the Vite path — fails the launcher with a message. To see what it
resolved:

```bash
bazel run //src/app:dev -- --dump-config
```
