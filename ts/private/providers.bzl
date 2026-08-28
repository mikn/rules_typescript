"""Provider definitions for rules_typescript.

A `direct` field carries only what the target itself produces. A rule that just
forwards a dep's files -- ts_compile relative to CSS or assets, say -- leaves the
direct field empty and puts the closure in the transitive one; a consumer that
wants everything reachable reads the transitive field.
"""

JsInfo = provider(
    doc = "Provider for JavaScript compilation outputs.",
    fields = {
        "js_files": "depset of File: .js files this target produces -- compiled output plus any JavaScript src staged as-is.",
        "js_map_files": "depset of File: .js.map source map files this target produces.",
        "transitive_js_files": "depset of File: Transitive closure of all .js files from this target and its deps.",
        "transitive_js_map_files": "depset of File: Transitive closure of all .js.map files.",
    },
)

TsDeclarationInfo = provider(
    doc = "Provider for TypeScript declaration outputs (.d.ts files).",
    fields = {
        "declaration_files": "depset of File: declaration files this target produces, plus the ambient ones it passes through from srcs.",
        "transitive_declaration_files": "depset of File: Transitive closure of all .d.ts files from this target and its deps.",
    },
)

TsConfigInfo = provider(
    doc = """A tsconfig.json and the files it extends.

Starlark cannot read the file to follow its `extends` chain, so a ts_config
target declares the chain and every file in it becomes an action input.
""",
    fields = {
        "tsconfig": "File: The tsconfig.json this target declares.",
        "deps_tsconfigs": "depset of File: Every file `tsconfig` extends, transitively.",
    },
)

NpmPackageInfo = provider(
    doc = "Provider for npm package targets.",
    fields = {
        "package_name": "string: npm package name (e.g., 'react').",
        "package_version": "string: npm package version.",
        "peer_id": "string: a filesystem-safe token naming the peer set this resolution was made against, empty for a package pnpm resolved only one way. Two snapshots can share name@version and differ only here, and they are two different dependency graphs, so anything keying a package by name and version alone merges them.",
        "package_dir": "File: The package.json file at the root of the extracted package.",
        "all_files": "depset of File: All files in this package (package.json + .js + .d.ts + other assets). Used by node_modules rule for runtime.",
        "js_files": "depset of File: JavaScript files in this package.",
        "declaration_files": "depset of File: TypeScript declaration files (.d.ts) in this package.",
        "direct_deps": "list of NpmPackageInfo: the packages this one depends on directly, each under the name this package imports it by. The flattened transitive closure cannot answer which version an individual package resolved to, which is what a node_modules tree needs to place two versions of one name.",
        "transitive_deps": "depset of NpmPackageInfo: Transitive npm dependencies.",
        "transitive_package_dirs": "depset of File: package.json files for this package and all transitive deps.",
        "exports_types_file": "File or None: The specific .d.ts entry point from package.json exports['.']['types'], or None if not specified. Used by ts_compile to build more precise tsconfig paths entries.",
        "subpath_types": "dict of string to File: each non-root `exports` subpath designating a declaration, mapped to that file. A consumer names one in compiler_options[\"types\"] to put it in its tsconfig `files` -- the only route by which an ambient module a package ships behind a subpath reaches the program, since tsconfig `types` resolves through a node_modules this ruleset does not have.",
        "ambient_types_file": "File or None: On an @types/* package, the entry-point .d.ts a consumer lists in tsconfig `files` to bring its globals and `declare module` blocks into the program. None on every other package.",
    },
)

CssInfo = provider(
    doc = "Provider for CSS file outputs.",
    fields = {
        "css_files": "depset of File: .css files this target itself produces; empty on a target that only forwards them.",
        "transitive_css_files": "depset of File: Transitive closure of all .css files.",
    },
)

CssModuleInfo = provider(
    doc = "Provider for CSS Module outputs (.module.css files with typed class names).",
    fields = {
        "css_files": "depset of File: .module.css files this target itself produces; empty on a target that only forwards them.",
        "transitive_css_files": "depset of File: Transitive closure of all .module.css files.",
        "exports_files": "depset of File: <source>.exports.json, the scoped-name map postcss-modules produced for each direct src. Its keys are what the .d.ts declares and its values are the class names the bundler must emit.",
        "transitive_exports_files": "depset of File: Transitive closure of all .exports.json maps.",
    },
)

AssetInfo = provider(
    doc = """Provider for static asset files (images, SVGs, fonts, JSON).

asset_library targets propagate asset files through the dependency graph so
that bundlers (e.g. Vite) can include them in the output bundle. Each asset
file also gets a generated ambient .d.ts declaration so that TypeScript accepts
'import logo from \"./logo.svg\"' without type errors.
""",
    fields = {
        "asset_files": "depset of File: asset files this target itself produces; empty on a target that only forwards them.",
        "transitive_asset_files": "depset of File: Transitive closure of all asset files.",
    },
)

DevServerInfo = provider(
    doc = """How to start one dev server implementation.

The shipped implementations are Vite (`//vite:dev_server`) and oj
(`//oj:dev_server`); `ts_dev_server(server = ...)` picks between them per
target, and a third is any rule returning this provider.

Two things differ between implementations and neither can be papered over.
A server shipping as an npm package has no `File` to point at -- its executable
is a path inside the `node_modules` tree artifact, which Starlark cannot address
at analysis time -- so it sets `server_in_tree` and leaves `server_binary` None.
A native binary is the other way round; exactly one of the two must be set.

`config_dialect` names the config the server is handed. Both shipped servers
read Vite's, because oj adopts `vite.config`. They do not read all of it, and a
field one of them drops is not always a field it does without: oj takes the
serve root from argv instead. `argv` covers the second case and
`ignored_config_fields` the first.
""",
    fields = {
        "server_binary": "File or None: the server executable, for a server that is a build artifact. None when the server ships inside the npm tree, in which case server_in_tree names it instead.",
        "server_in_tree": "string: the server executable's path relative to the root of the node_modules tree, for a server that ships as an npm package. Empty when server_binary is set.",
        "argv": "list of string: the command line after the executable. `{config}` expands to the generated config's path and `{root}` to the directory being served; a server taking either somewhere other than where the other one takes it says so here rather than in the launcher.",
        "config_dialect": "string: which config format this server is handed. Only \"vite\" is generated today; a server reading its own format declares its own dialect, and the generator has to learn it before that server can be selected.",
        "runs_in_js_runtime": "bool: True when the executable is JavaScript and the toolchain Node runs it, False for a native binary. A native server still gets the toolchain Node on PATH -- oj's plugin host is a Node process, so a native server is not necessarily a Node-free one.",
        "ignored_config_fields": "list of string: dotted config paths this server does not honour, e.g. [\"server.open\"]. A target whose configuration depends on one of these fails at analysis time naming the field and the server, rather than starting a server that quietly does something else.",
        "native_react_refresh": "bool: True when the server applies React Fast Refresh itself. `react_refresh = True` then fails rather than stacking @vitejs/plugin-react on top of a transform that already ran.",
        "runtime_deps": "depset of File: everything the server needs in runfiles beyond the generated config and the npm tree.",
    },
)

BundlerInfo = provider(
    doc = """Information about a JavaScript bundler.

BundlerInfo provides a pluggable bundler abstraction. The shipped implementation
uses Vite (via vite/bundler.bzl), but users can bring their own bundler by
creating a rule that returns this provider.

Two invocation modes are supported:

Mode 1 — Standard CLI (use_generated_config = False, the default):
  The bundler binary is invoked with:
    --entry <path_to_entry.js>
    --out-dir <output_dir>
    --format esm|cjs|iife
    --external <pkg>         (may be repeated)
    --sourcemap              (flag, no value)
    --config <config_file>   (optional)

Mode 2 — Generated config (use_generated_config = True):
  ts_bundle generates a vite.config.mjs and invokes the bundler with:
    <generated_config_path>
  (single positional argument — the absolute path to the generated config)
  The bundler binary is responsible for running the actual bundler
  (e.g., `node vite.js build --config <config>`).
  Output filenames follow Vite's lib mode convention:
    <bundle_name>.<format>.js  (e.g., app.es.js for esm, app.umd.cjs for iife)
""",
    fields = {
        "bundler_binary": "File: The bundler CLI executable.",
        "config_file": "File or None: Optional static bundler config file passed via --config (mode 1 only).",
        "runtime_deps": "depset of File: Additional files needed by the bundler at runtime.",
        "use_generated_config": "bool: When True, ts_bundle generates a vite.config.mjs and passes its path as the sole argument to bundler_binary (mode 2). Default False.",
    },
)
