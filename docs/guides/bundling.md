# Bundling

`ts_binary` and `ts_bundle` collect transitive `.js` outputs and hand them to a
pluggable bundler. `ts_bundle` requires a `bundler` and has no bundler-less mode.

A Cloudflare Worker is bundled by wrangler. See
[ts_worker_dry_run](../rules/ts-worker-dry-run.md) for building one and checking
that it still deploys, and
[Testing § Cloudflare Workers](testing.md#cloudflare-workers) for running its
tests inside workerd.

## With Vite

Declare the three targets at the **workspace root**. See
[where the bundler's tree has to sit](#where-the-bundlers-node_modules-has-to-sit).

```python
# BUILD.bazel, at the workspace root
load("@rules_typescript//vite:bundler.bzl", "vite_bundler")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_bundle")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:vite",
        "@npm//:zod",
    ],
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

### Where the bundler's `node_modules` has to sit

That tree supplies Vite and every npm package the bundled graph imports; a
`ts_compile` dep does not carry its own npm packages into it. A missing package
fails the build, naming the file that imported it:

```
Error: [vite]: Rolldown failed to resolve import "zod" from
"…/bazel-out/k8-fastbuild/bin/src/lib/math.js".
```

The tree is materialised at the target's own name under its package in
`bazel-bin`, and rolldown resolves a bare specifier by Node's
walk-up from the importer, so the tree has to sit in an ancestor directory of
every compiled `.js` that imports one. A tree in `//src/app` does not serve an
import in `//src/lib`; one at the workspace root serves the whole repository.

`external` is the other way out, for a specifier you want left as an import for
whoever consumes the bundle.

## Running Without a Bundler

`ts_binary` with no `bundler` runs the entry point's own `.js` on the JS runtime,
with the transitive `.js` in its runfiles. The imports resolve as written;
nothing is bundled or concatenated.

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

## Chunk Splitting

`split_chunks = True` gives third-party code its own chunk, so a first-party edit
leaves the vendor chunk's content hash untouched. Vite bundlers only; a bundler
invoked through the standard CLI interface ignores the attribute.

Both modes honour it. In lib mode the single-file output becomes a directory. In
app mode the output is a directory already, and Vite already splits on dynamic
imports; `split_chunks` adds the rule that everything under `node_modules` lands
in one chunk.

It is emitted as `build.rollupOptions.output.manualChunks`, which every Vite
generation from 6 onward honours. The vendor-splitting plugin it used to emit was
removed in Vite 7.

Lib mode derives the extension from the nearest `package.json#type`, which
nothing in a Bazel output tree declares, so the chunk can land as `.mjs`, and its
name carries a content hash. Locate it by exclusion: the entry is
`<bundle_name>.es.js` (or `.cjs.js`, `.iife.js`), the chunk is whatever else is
in the directory.

`minify = True` selects the running Vite's own default minifier (esbuild on 6,
oxc on 8) without naming one: esbuild and terser are optional peers, absent from
a tree built from `deps = ["@npm//:vite"]`. `False` also pins
`output.minify: false`, since `build.minify: false` alone still runs the
dead-code pass, which re-emits every chunk from its AST and drops a plugin's
`renderChunk` output.

## CSS, CSS modules and assets

A stylesheet, a `*.module.css` and an imported asset need no bundle-level
configuration. They reach the bundler through the entry point's `CssInfo`,
`CssModuleInfo` and `AssetInfo`, the providers [`css_library`, `css_module` and
`asset_library`](../rules/css-and-assets.md) populate. Each rule copies its files
into `bazel-bin` beside the compiled `.js` that imports them.

The two modes differ in what comes out:

- **App mode** hashes every imported stylesheet and asset
  (`assets/index-C_rPVxYH.css`, `assets/big_logo-DWeKL6j3.svg`) and rewrites the
  references in the emitted HTML. An asset under Vite's 4096-byte
  `assetsInlineLimit` is inlined as a `data:` URI and gets no filename at all.
- **Lib mode** extracts all CSS into one `<bundle_name>.css`, never referenced
  from the JS, so the library's consumer includes it. Bazel declares that file
  explicitly: Vite writes it either way, and an undeclared output goes out with
  the sandbox. The declared outputs are that stylesheet, the entry `.js` and its
  map. An asset too large to inline is a loose file, undeclared, and goes out
  with the sandbox; app mode declares a directory and keeps it.

### Static files (`public_dir`)

`public_dir` names the files that must keep the name they were given: a
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

They are staged into a directory of their own under `bazel-bin` first: Vite
copies a `publicDir` wholesale, and the source package in the sandbox also holds
the sources, the HTML and the compiled outputs. Anything imported from
TypeScript belongs in an `asset_library`, where it gets a content hash and a
cacheable URL.

`publicDir` is one directory, named by the declaration and not by whichever files
a glob matched. Three shapes fail at analysis time, each naming the file:

- a file outside the package of the `public_dir` label;
- a file directly in that package, where the package itself would become the URL
  root; put the files in `public/` and glob that;
- files in more than one directory under the package; split them, or point
  `public_dir` at a single directory.

### The Manifest

`manifest = True` writes `manifest.json` into the output directory, mapping each
input to the hashed file it became, plus the CSS and assets each chunk pulled in.
It is for a server that renders HTML itself and has to emit script and link tags
for filenames it did not choose. Vite's own `index.html` needs none of it.

Both attrs are app mode only and fail at analysis time in lib mode, which
declares its output filenames.

## Framework plugins via `vite_config`

`vite_config` takes the config file, `vite_config_srcs` the local modules it
imports. Both are staged into `bazel-bin`, and the generated config loads the
staged copy and prepends its plugins to Bazel's. TanStack Start's plugin goes
through that hook, and Remix's when a client-only bundle is what you want. Two
frameworks do not fit through it: SvelteKit, which has
[a rule of its own](../rules/sveltekit-build.md), and Solid Start, which has no
rule and no bundle target at all. See
[framework detection](../gazelle/overview.md#framework-detection).

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
bundler-resolution config is written with: the generated config loads yours
through Vite's own `loadConfigFromFile`, the loader Vite runs on a root config. A
plain dynamic `import()` reads neither, and is the fallback only for a `.mjs`
config when Vite is not in the tree.

Three constraints:

- `vite_config_srcs` stages each module at its path relative to the config's
  package, so a relative import resolves in the staged tree as it does in the
  source tree. Without it only the entry config is staged, and its relative
  imports fail naming the file. A file outside the config's own package is an
  error: it would stage above the staging root.
- A bare npm specifier resolves through the Bazel tree, a relative one through
  the staged tree. The staged copy sits beside the `node_modules` the build
  produced, so a framework package resolves without a source-tree
  `node_modules`.
- The config supplies plugins and runs in the output tree. Anything it computes
  from its surroundings sees `bazel-bin`: an env var it expects the dev server to
  have set, a file it reads from the repository.

### Keys the Generated Config Reads

The generated config reads a fixed set of keys out of yours, so a config that
computes the build is not expressible:

| Rule | Keys it reads |
|---|---|
| `ts_bundle` | `plugins`, `root` |
| `ts_dev_server` | `plugins` |

Any other key makes the load throw, naming the keys it found and the keys it
honours:

```
[rules_typescript] ts_bundle: the vite_config sets define, resolve, which the generated config does not read. Only plugins, root reach the build; the rest would be
silently discarded. Move what you need into a plugin, or open an
issue for the option.
```

`define`, `resolve.alias`, `build.target` and `optimizeDeps` are the options a
real framework config sets; the build stops on them. Where an attribute owns the
option, use it: `define`, `env_vars`, `external`, `minify`, `split_chunks`,
`public_dir`, `manifest`. The check runs where the config is loaded, not at
analysis time, because only the loaded object says which keys it has.

A config carrying `root` builds under `ts_bundle` and fails under
`ts_dev_server`, which takes its serve root from the target. Under oj, `root` is
in the provider's `ignored_config_fields` for a different reason: oj takes the
served directory from argv.

## Custom Bundler (BundlerInfo Interface)

Any Bazel rule that returns `BundlerInfo` can plug into `ts_bundle` and
`ts_binary`. This lets you bring your own bundler — esbuild, Rolldown, webpack —
without modifying `rules_typescript`.

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
  [--define <key>=<value>]...
  [--config <config_file>]   (only when config_file is set)
```

Output is expected at `<out-dir>/<bundle_name>.js` (and `.js.map` if `--sourcemap`).

`public_dir` and `manifest` are Vite options and fail at analysis time on a
target whose bundler is invoked this way.

**Mode 2 — Generated config** (`use_generated_config = True`)

`ts_bundle` generates a `vite.config.mjs` containing all bundle options and
invokes the binary with six positional arguments, all execroot-relative. The
trailing three are passed as empty strings when absent, so the positions never
shift:

```
<bundler_binary> \
  <generated vite.config.mjs> \
  <entry .js> \
  <output dir> \
  <html file>          (app mode only; "" otherwise) \
  <staging manifest>   ("" when there are no staging_srcs) \
  <lib-mode stylesheet>  ("" in app mode)
```

Lib mode declares the output by name inside `<name>_bundle/`, following Vite's
own lib convention:

| Format | Output file |
|--------|-------------|
| `esm` | `<bundle_name>.es.js` |
| `cjs` | `<bundle_name>.cjs.js` |
| `iife` | `<bundle_name>.iife.js` |

App mode, and lib mode with `split_chunks = True`, declare that directory
itself, because the hashed filenames are not known at analysis time.

### BundlerInfo Fields

| Field | Type | Description |
|-------|------|-------------|
| `bundler_binary` | `File` | The executable that performs bundling |
| `config_file` | `File` or `None` | Optional static config passed via `--config` (mode 1 only) |
| `runtime_deps` | `depset of File` | Files the bundler needs at runtime |
| `use_generated_config` | `bool` | When `True`, use mode 2 (generated vite.config.mjs) |

## Attributes

The two attribute sets differ. Shared:

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
`public_dir`, `manifest`, `vite_config`, `vite_config_srcs`, `staging_srcs`. See
the [ts_bundle reference](../rules/ts-bundle.md#attributes).

`ts_binary` only: `entry_file` (which `.js` is the entry when the target emits
several) and `node_modules`. See the
[ts_binary reference](../rules/ts-binary.md#attributes).

Setting `minify` on a `ts_binary` fails the build; it is not a silent no-op.
