# Dev Server

`ts_dev_server` starts a Vite dev server for a TypeScript application. `bazel
run //src/app:dev` builds the target once and then **Bazel is out of the inner
loop**: Vite transforms your first-party source in memory, so a save reaches the
browser without a Bazel analysis-and-action cycle in between.

## Setup

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

Vite itself comes from the `node_modules` tree — the rule does not fetch it —
so that attribute is what makes the target runnable.

```bash
bazel run //src/app:dev     # start it
ibazel run //src/app:dev    # same, plus codegen rebuilds and config-aware restarts
```

## What is served from where

Dev and production want opposite things from the same import, and get them.

| | `ts_bundle` (`vite build`) | `ts_dev_server` (`vite dev`) |
|---|---|---|
| first-party `.ts` | Bazel compiles it; the plugin redirects imports to `bazel-bin` | **served as source**, transformed by Vite in memory |
| `ts_codegen` output | from `bazel-bin` | from `bazel-bin` |
| npm packages | the `node_modules` tree | the `node_modules` tree, via the `bazel:npm-resolve` plugin |
| assets, passthrough `.d.ts` | from `bazel-bin` | from `bazel-bin` |

Generated code is recognised by having *no checked-in source*, rather than by a
list of paths that would drift away from what `ts_codegen` actually produces.

Each first-party `module_name` in the graph also becomes a `resolve.alias`
entry pointing at that package's source, so `import "@acme/ui"` and a relative
import of the same file are one module in Vite's graph instead of two copies of
it. That mapping is the same `TsModuleInfo` one `ts_compile` writes into its
tsconfig `paths` — not a second source of truth.

### How a bare npm specifier resolves

Vite has no search-path option. It resolves `import "zod"` by walking up from the
importer looking for a `node_modules` directory, and above a checked-in source
file there is never one — the npm tree is a Bazel output somewhere else entirely.
(`resolve.modules` is webpack's; Vite ignores it, so a config that sets it
configures nothing.)

So the rule installs a plugin, `bazel:npm-resolve`, at `enforce: 'pre'`. For a
bare specifier it looks for `<tree>/<package>/package.json`; if that file exists
it hands the id straight back to Vite's own resolver with that manifest as the
importer, which does have a `node_modules` above it. Two consequences worth
knowing:

- **Exports maps, conditions and subpaths stay Vite's.** The plugin decides
  *where* to look, never *what* a specifier means, so `import "zod/v4"` or a
  package with a conditional `exports` behaves in dev exactly as it does in a
  `ts_bundle`.
- **A package the tree does not carry still fails.** The plugin returns nothing
  and you get Vite's ordinary `Failed to resolve import` — add the package to the
  `node_modules` target's `deps`, not to the config.

The `node_modules` attr is therefore what makes npm imports work at all in dev, in
addition to supplying Vite itself.

## The dev server does not type-check

This is native parity: Vite has never type-checked. It does mean that during
development your editor and `bazel build` are the only things reporting type
errors — a type error no longer blocks the browser update. If you rely on the
dev server to catch type errors today, set up
[IDE integration](../getting-started/ide-setup.md) before you rely on this
instead.

## The plugin, and what it buys

The `plugin` attribute wires `vite-plugin-bazel`. It is what:

- resolves generated code out of `bazel-bin` (without it, `bazel-bin` — and so
  every `ts_codegen` output — is invisible to Vite);
- invalidates precisely on a rebuild, so a codegen change arrives as an HMR
  update;
- makes the restart decision described below.

**Gazelle** sets `plugin = "@rules_typescript//vite:vite_plugin_bazel"` when it
first generates a `ts_dev_server` target. The attribute is only set on first
generation and can be removed if you do not want it.

## React Fast Refresh

`react_refresh = True` loads `@vitejs/plugin-react`, which preserves component
state across an HMR update. The package has to be in the `node_modules` tree:

```python
node_modules(
    name = "dev_node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:vitejs_plugin-react",
    ],
)

ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":dev_node_modules",
    react_refresh = True,
)
```

The entry point comes from that package's own `exports` map rather than from a
path into its `dist/`, which is a layout the package owns and reorganises between
majors. If the plugin cannot be loaded the dev server **fails to start**, naming
the target and the dep to add — it does not come up without Fast Refresh.

!!! note "The tree has to be named `node_modules`"
    `@vitejs/plugin-react` resolves the `react-refresh` runtime by Node's own
    walk-up, which only looks in directories called `node_modules`. A tree named
    anything else loses Fast Refresh with no error.

## `vite_config`: what it may import

`vite_config` takes one `.mjs`/`.js` file default-exporting `{plugins: [...]}`,
and the rule loads a **copy of it in `bazel-bin`** rather than your source file.
That is deliberate: Node resolves a runfiles symlink before it resolves that
file's own imports, so a config loaded from the source tree would resolve its
imports through a source-tree `node_modules` — which this ruleset does not have,
and which no part of the build graph would know about if it did.

The copy is what draws the boundary, and
`//tests/dev_server:vite_config_boundary_test` pins every side of it:

- A **bare npm specifier** resolves, through the tree the `node_modules` attr
  built. That target must be in the same Bazel package as the dev server — it is
  the directory Node finds walking up from the copy. Gazelle generates them
  together, so this is automatic unless you moved one.
- A **relative import resolves only if the module is declared** in
  `vite_config_srcs`, which is what stages it beside the copy. An **undeclared**
  sibling is not there, and the dev server exits with
  `[rules_typescript] Failed to load vite_config: …` naming the file, rather than
  starting on half a config.

`vite_config` accepts TypeScript, and the extensionless relative specifiers a
bundler-resolution config is written with, because the generated config loads it
through Vite's own `loadConfigFromFile` rather than a plain dynamic `import()`.

`ts_bundle` stages its config the same way, so the two attrs no longer differ —
see [bundling](bundling.md#framework-plugins-via-vite_config).

## Watch mode with ibazel, and who decides to restart

```bash
go install github.com/bazelbuild/bazel-watcher/cmd/ibazel@latest
ibazel run //src/app:dev
```

`ibazel run` SIGTERMs the launcher after every rebuild, and the launcher
deliberately survives that: one Vite process lives across every rebuild. So the
restart-or-keep decision is not ibazel's — it is made inside that process:

| What changed | Handled by | Restarts Vite? |
|---|---|---|
| first-party source | Vite transform → HMR | no — Bazel is not involved at all |
| `ts_codegen` output | the plugin's `bazel-bin` watcher | no |
| the generated Vite config | `ConfigWatcher` | yes |
| the npm tree, or the Vite version in it | `ConfigWatcher` | yes, with a warning |
| the toolchain node binary | `ConfigWatcher` | yes, with a warning |

The generated config exports `bazelConfigInputs`: for each input, a path, the
digest that identifies it, and whether an in-process restart can actually fix a
change to it. Content digests, not timestamps — Bazel rewrites outputs on every
action, so an mtime says nothing about whether anything changed. The warning on
the last two rows is there because only a new `bazel run` really replaces a node
binary or an npm tree.

Vite restarts itself when its *own* config file changes but has no concept of
the thing that generates that config, because natively nothing does. That is
what `ConfigWatcher` adds.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target for the application entry point |
| `port` | `int` | `5173` | Dev server port |
| `host` | `string` | `"localhost"` | Dev server host. Set to `"0.0.0.0"` to bind on all interfaces |
| `open` | `bool` | `False` | Open the browser automatically on start |
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and the application's runtime deps. Also what makes a bare npm import resolve at all — see [above](#how-a-bare-npm-specifier-resolves) |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs`. Without it Vite serves first-party source and nothing else — `bazel-bin` is invisible |
| `server` | `label` | `//vite:dev_server` | Which implementation serves this target, as a `DevServerInfo`-providing target. `@rules_typescript//oj:dev_server` selects oj — see [below](#choosing-the-server) |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a non-Vite dev server. The Vite path does not need it |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps; the dev server fails to start if the plugin cannot be loaded |
| `vite_config_srcs` | `label_list` | `[]` | The local modules `vite_config` imports, staged beside it so its relative imports resolve |
| `vite_config` | `label` | `None` | A `.ts`/`.mts`/`.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. This is how framework plugins run in the dev server — TanStack Start's and Remix's do; SvelteKit's and Solid Start's [cannot](../gazelle/overview.md#framework-detection). Loaded from a copy in `bazel-bin`, which bounds what it may import |

## Choosing the server

`ts_dev_server` takes a `DevServerInfo`, and the implementation is a per-target
choice. Vite is the default; oj ([raphamorim/oj](https://github.com/raphamorim/oj),
a Rust-native build tool) is the second shipped one, and reads the same generated
config:

```python
ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",   # oj needs no @npm//:vite in here
    server = "@rules_typescript//oj:dev_server",
)
```

Two differences are structural rather than incidental, and the provider declares
both rather than leaving the launcher to guess. oj takes the directory it serves
from a positional argument, not from the config's `root`. And a field one server
does not read is an **analysis-time error** on a target that set the attr
reaching it — `open = True` against oj fails naming both, rather than starting a
server that quietly does something else. `react_refresh` is the same: oj applies
Fast Refresh itself, so setting it would instrument every component twice.

!!! note "oj carries one patch"
    `oj_server` served a module only when a plugin `load` hook returned its
    contents, so the resolver plugin — which maps a bare specifier to a path in
    the Bazel tree and leaves the contents to the server — got a 404 for every
    module it resolved correctly. Rollup's contract is that a `resolveId` result
    naming a real file *is* the module, and a `load` returning nothing means read
    it from disk. `oj/patches/` implements that half, applied through
    `crate.annotation(patches = ...)`, and it is upstreamable as written. Without
    it `import "react"` does not resolve under oj at all.

Bringing your own is a rule returning `DevServerInfo`. A server shipping as an
npm package sets `server_in_tree` (a path inside the `node_modules` tree, since a
file inside a TreeArtifact has no label at analysis time); a native binary sets
`server_binary`. Exactly one.

## Diagnostics

The launcher exits with a message rather than reaching for a host `node` or
`vite`: a missing JS runtime toolchain fails at analysis time, and a missing
`node_modules`/vite fails the launcher. To see what it resolved:

```bash
bazel run //src/app:dev -- --dump-config
```
