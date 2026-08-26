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

## CSS, CSS modules and assets

A stylesheet, a `*.module.css` and an imported asset are part of the module graph
and need no bundle-level configuration. They reach the bundler through the entry
point's `CssInfo`, `CssModuleInfo` and `AssetInfo` — the same providers
[`css_library`, `css_module` and `asset_library`](../rules/css-and-assets.md)
populate — and each rule copies its files into `bazel-bin` beside the compiled
`.js` that imports them, so the relative import resolves in the output tree.

The two modes differ in what comes out:

- **App mode** hashes every imported stylesheet and asset
  (`assets/index-C_rPVxYH.css`, `assets/big_logo-DWeKL6j3.svg`) and rewrites the
  references in the emitted HTML. An asset under Vite's 4096-byte
  `assetsInlineLimit` is inlined as a `data:` URI and gets no filename at all.
- **Lib mode** extracts all CSS into one `<bundle_name>.css` and never
  references it from the JS. Bazel declares that file explicitly — Vite writes
  it either way, and an undeclared output goes out with the sandbox — so the
  consumer of the library has to include it. What lib mode declares is that
  stylesheet, the entry `.js` and its map, and nothing else: an asset too large
  to inline is emitted as a loose file, is therefore undeclared, and goes out
  with the sandbox. App mode is the answer there — a library shipping hashed
  loose files is not much use to a downstream bundler anyway.

### Static files (`public_dir`)

`public_dir` names the files that must keep the name they were given — a
`robots.txt`, a favicon referenced from an HTML tag, anything fetched by a URL
the build never sees. Vite copies them into the output directory verbatim: no
hash, no transform, no reference rewriting.

```python
filegroup(
    name = "public",
    srcs = glob(["public/**"]),
)

ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    mode = "app",
    html = "index.html",
    public_dir = ":public",
    manifest = True,
)
```

The files are staged into a directory of their own under `bazel-bin` before Vite
sees them, because Vite copies a `publicDir` wholesale and the source package in
the sandbox also holds the sources, the HTML and the compiled outputs.

Anything *imported* from TypeScript belongs in an `asset_library` instead, where
it gets a content hash and a cacheable URL.

### The manifest

`manifest = True` writes `manifest.json` into the output directory, mapping each
input to the hashed file it became, plus the CSS and assets each chunk pulled in.
Vite's own `index.html` needs none of it — those references are already
rewritten. It is for a server that renders HTML itself and has to emit script and
link tags for filenames it did not choose.

Both attrs are app mode only and fail at analysis time in lib mode, which
declares its output filenames rather than hashing them.

## Framework plugins via `vite_config`

`vite_config` takes the config file, and `vite_config_srcs` the local modules it
imports. Both are staged into `bazel-bin` and the generated config loads the
staged copy, prepending its plugins to Bazel's. That is the hook TanStack Start's
and Remix's plugins go through — and the two frameworks Gazelle deliberately
generates nothing for are the ones this hook cannot serve
([which, and why](../gazelle/overview.md#framework-detection)):

```typescript
// vite.plugins.ts
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

import { myPlugin } from "./plugins/mine";

export default { plugins: [tanstackStart(), myPlugin()] };
```

```python
ts_bundle(
    name = "app",
    entry_point = "//src/app",
    bundler = ":vite",
    mode = "app",
    html = "index.html",
    vite_config = "vite.plugins.ts",
    vite_config_srcs = glob(["plugins/**/*.ts"]),
)
```

TypeScript is accepted, and so are the extensionless relative specifiers a
bundler-resolution config is written with, because the generated config goes
through Vite's own `loadConfigFromFile` — the same loader Vite runs on a root
config — rather than a plain dynamic `import()`, which reads neither.

Three things are worth knowing before planning a migration around this:

- **The modules it imports have to be declared.** `vite_config_srcs` is what
  stages them; without it only the entry config is staged and its relative
  imports fail, naming the file. A file outside the config's own package is an
  error rather than a silent flattening — it would have to stage above the
  staging root.
- **A bare npm specifier resolves through the Bazel tree, a relative one through
  the staged tree.** The staged copy sits beside the `node_modules` the build
  produced, so the framework package resolves without a source-tree
  `node_modules` — which is what used to make a checked-in config work only on a
  machine that had run `pnpm install`.
- **The config is a plugin list, not a program.** Anything the config computes
  from its surroundings — reading the repository, branching on an env var it
  expects the dev server to have set — runs in the output tree, not the source
  tree.

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
`public_dir`, `manifest`, `vite_config`, `staging_srcs` — see the
[ts_bundle reference](../rules/ts-bundle.md#attributes).

`ts_binary` only: `entry_file` (which `.js` is the entry when the target emits
several) and `node_modules` — see the
[ts_binary reference](../rules/ts-binary.md#attributes).

Setting `minify` on a `ts_binary` is an analysis error, not a no-op.
