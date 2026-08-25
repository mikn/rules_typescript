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
| npm packages | the `node_modules` tree | the `node_modules` tree |
| assets, passthrough `.d.ts` | from `bazel-bin` | from `bazel-bin` |

Generated code is recognised by having *no checked-in source*, rather than by a
list of paths that would drift away from what `ts_codegen` actually produces.

Each first-party `module_name` in the graph also becomes a `resolve.alias`
entry pointing at that package's source, so `import "@acme/ui"` and a relative
import of the same file are one module in Vite's graph instead of two copies of
it. That mapping is the same `TsModuleInfo` one `ts_compile` writes into its
tsconfig `paths` — not a second source of truth.

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
| `node_modules` | `label` | `None` | `node_modules` target providing Vite and the application's runtime deps |
| `plugin` | `label` | `None` | Compiled `vite-plugin-bazel` `.mjs`. Without it Vite serves first-party source and nothing else — `bazel-bin` is invisible |
| `bundler` | `label` | `None` | `BundlerInfo`-providing target, for a non-Vite dev server. The Vite path does not need it |
| `react_refresh` | `bool` | `False` | React Fast Refresh via `@vitejs/plugin-react`, so component state survives an HMR update. Requires `@npm//:vitejs_plugin-react` in the `node_modules` deps |
| `vite_config` | `label` | `None` | A `.mjs`/`.js` file default-exporting `{plugins: [...]}`, prepended to Bazel's plugins. This is how framework plugins run in the dev server — TanStack Start's and Remix's do; SvelteKit's and Solid Start's [cannot](../gazelle/overview.md#framework-detection) |

## Diagnostics

The launcher exits with a message rather than reaching for a host `node` or
`vite`: a missing JS runtime toolchain fails at analysis time, and a missing
`node_modules`/vite fails the launcher. To see what it resolved:

```bash
bazel run //src/app:dev -- --dump-config
```
