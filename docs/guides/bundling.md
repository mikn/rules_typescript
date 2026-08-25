# Bundling

`ts_binary` and `ts_bundle` collect transitive `.js` outputs and hand them to a pluggable bundler.

`ts_bundle` requires a `bundler`: there is no bundler-less mode, because an
artifact that only looks like a bundle is worse than a build error.

## Basic Usage (No Bundling)

`ts_binary` without a `bundler` runs the entry point `.js` directly — nothing is bundled.

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary")

ts_binary(
    name = "app",
    entry_point = "//src/app",
)
```

```bash
bazel run //:app
```

## With Vite

```python
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_bundle")

node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    vite = "@npm//:vite",
    node_modules = ":node_modules",
)

ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    format = "esm",
    sourcemap = True,
    minify = True,
    external = ["react", "react-dom"],
)
```

## Chunk splitting

`split_chunks = True` gives third-party code its own chunk, so a first-party edit
leaves the vendor chunk's content hash untouched. The output becomes a directory
rather than a single file. Lib mode only — app mode does its own splitting.

It is emitted as `build.rollupOptions.output.manualChunks`, the spelling every
Vite generation from 6 onward honours (on Vite 8, rolldown maps it onto its own
`advancedChunks`). The vendor-splitting *plugin* this used to emit was removed in
Vite 7.

Two things about the split chunk's filename, since a deploy script or a test may
want to find it: lib mode derives the extension from the nearest
`package.json#type`, which nothing in a Bazel output tree declares, so the chunk
can land as `.mjs`; and its name carries a content hash. Locate it by exclusion —
the entry is `<bundle_name>.<format>.js`, the chunk is whatever else is in the
directory — not by a literal name.

`minify` interacts with this. `True` means "the running Vite's own default
minifier" (esbuild on 6, oxc on 8) rather than naming one, because both esbuild
and terser are optional peers and so absent from a tree built from
`deps = ["@npm//:vite"]`. `False` additionally pins `output.minify: false`:
`build.minify: false` on its own still runs the bundler's dead-code pass, which
re-emits every chunk from its AST and discards whatever a plugin's `renderChunk`
returned.

## Framework plugins via `vite_config`

`vite_config` takes **one** `.mjs`/`.js` file that default-exports
`{plugins: [...]}` (and optionally `root`). The generated config imports it at
run time and prepends its plugins to Bazel's. That is the hook TanStack Start's
and Remix's plugins go through — and the two frameworks Gazelle deliberately
generates nothing for are the ones this hook cannot serve
([which, and why](../gazelle/overview.md#framework-detection)):

```javascript
// vite.plugins.mjs
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

export default { plugins: [tanstackStart()] };
```

```python
ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    mode = "app",
    html = "index.html",
    vite_config = "vite.plugins.mjs",
)
```

Two limits are worth knowing before you plan a migration around this:

- **One file, no local imports.** The attr is `allow_single_file`, so a config
  that imports plugin modules of its own — a repository's real
  `vite.config.ts`, split across helper files — cannot be expressed as one
  staged file. Nothing collects those imports into the action's inputs, and a
  relative import from the staged config resolves next to the generated config
  in the output tree, not next to the source you wrote.
- **The plugin package has to be resolvable from where the file is imported.**
  `ts_bundle` imports the config by exec-root path, and Node realpaths it back
  into the source tree, so the framework package must be resolvable from *there* —
  which for a checked-in config means a source-tree `node_modules`. This
  repository's own `examples/tanstack-app` hits exactly that: its app-mode
  bundle is excluded from CI with the reason in the workflow, while the SPA
  target builds. Point the attr at a **generated** file under `bazel-out` and the
  problem goes away, because that file sits beside the hermetic npm tree.

`ts_dev_server`'s `vite_config` attr does not have the second limit: it loads a
copy of the file in `bazel-bin`, so a bare npm specifier resolves through the
`node_modules` tree, and the boundary that remains — no relative imports — is
[stated and tested](dev-server.md#vite_config-what-it-may-import). `ts_bundle`
has not been given the same staging yet, so the two attrs differ on this one
point.

If your Vite configuration is a program rather than a plugin list, this attr is
not enough today, and there is no supported way to hand `ts_bundle` a
multi-file config.

## Custom Bundler (BundlerInfo Interface)

Any Bazel rule that returns `BundlerInfo` can plug into `ts_bundle` and `ts_binary`. This lets you bring your own bundler — esbuild, Rolldown, webpack — without modifying `rules_typescript`.

```python
load("@rules_typescript//ts:defs.bzl", "BundlerInfo")

def _my_bundler_impl(ctx):
    return [BundlerInfo(
        bundler_binary = ctx.file.binary,
        config_file = None,                 # optional static config
        runtime_deps = depset([]),           # files needed at bundle time
        use_generated_config = False,        # set True for Vite-style config
    )]

my_bundler = rule(
    implementation = _my_bundler_impl,
    attrs = {
        "binary": attr.label(
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)
```

### BundlerInfo Invocation Modes

**Mode 1 — Standard CLI** (`use_generated_config = False`, the default)

`ts_bundle` invokes the bundler binary with:

```
<bundler_binary>
  --entry  <path/to/entry.js>
  --out-dir <output/dir>
  --format esm|cjs|iife
  [--external <pkg>]...
  [--sourcemap]
  [--config <config_file>]   (only when config_file is set)
```

Output is expected at `<out-dir>/<bundle_name>.js` (and `.js.map` if `--sourcemap`).

**Mode 2 — Generated config** (`use_generated_config = True`)

`ts_bundle` generates a `vite.config.mjs` containing all bundle options and invokes:

```
<bundler_binary> <absolute_path_to_vite.config.mjs> <entry_path> <out_dir>
```

| Format | Output file |
|--------|-------------|
| `esm` | `<bundle_name>.es.js` |
| `cjs` | `<bundle_name>.cjs.js` |
| `iife` | `<bundle_name>.iife.js` |

### BundlerInfo Fields

| Field | Type | Description |
|-------|------|-------------|
| `bundler_binary` | `File` | The executable that performs bundling |
| `config_file` | `File` or `None` | Optional static config passed via `--config` (mode 1 only) |
| `runtime_deps` | `depset of File` | Files the bundler needs at runtime |
| `use_generated_config` | `bool` | When `True`, use mode 2 (generated vite.config.mjs) |

## Attributes

The two rules are not aliases and their attribute sets differ. Shared:

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `entry_point` | `label` | required | `ts_compile` target providing `JsInfo` |
| `bundler` | `label` | required for `ts_bundle`, optional for `ts_binary` | Target providing `BundlerInfo` |
| `bundle_name` | `string` | rule name | Output file name (without `.js`) |
| `format` | `string` | `"esm"` | Output format: `esm`, `cjs`, `iife` |
| `sourcemap` | `bool` | `True` | Emit source map |
| `external` | `string_list` | `[]` | Module specifiers to leave external |
| `define` | `string_dict` | `{}` | Global constant replacements |

`ts_bundle` only: `minify`, `split_chunks`, `env_vars`, `mode`, `html`,
`vite_config`, `staging_srcs` — see the
[ts_bundle reference](../rules/ts-bundle.md#attributes).

`ts_binary` only: `entry_file` (which `.js` is the entry when the target emits
several) and `node_modules` — see the
[ts_binary reference](../rules/ts-binary.md#attributes).

Setting `minify` on a `ts_binary` is an analysis error, not a no-op.
