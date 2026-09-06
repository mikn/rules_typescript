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
        "declaration_files": "depset of File: declaration files this target produces, plus the ambient ones it passes through from srcs; on an npm package, its module entry too when that is a `.ts`, so a consumer's action stages it.",
        "transitive_declaration_files": "depset of File: Transitive closure of all .d.ts files from this target and its deps.",
        "transitive_npm_packages": "depset of NpmPackageInfo: the npm packages whose declarations are in transitive_declaration_files, so a consumer can write the tsconfig `paths` key each of them takes. A dep's emitted .d.ts imports the packages the dep declared and resolves them in the consumer's program, where nothing but a `paths` key answers a bare specifier. An npm package target names its transitive_deps; the package itself arrives through its NpmPackageInfo.",
        "global_entry_files": "depset of File: a generated .d.ts referencing the srcs of this target whose declarations are global -- a .d.ts with no top-level import or export. A consumer lists it in its tsconfig `files`, the route an @types/* package's globals already take. It is a file of references rather than the declarations themselves because Starlark cannot read a source to tell a global .d.ts from a module one, so an action decides and this names its answer at analysis time. Only a src that ts_compile's public_globals names is referenced; every other one stays in the program of the target that owns it, and a target that names none provides no entry at all.",
        "transitive_global_entry_files": "depset of File: Transitive closure of global_entry_files. A global is global to the whole program, and an intermediate library between the declaration and the code using it does not change that, so the closure travels rather than the direct set.",
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
        "package_dir": "File or None: The package.json file at the root of the extracted package. It doubles as the tsconfig `paths` anchor, so a package that was never extracted from a tarball leaves it None -- a pnpm workspace member is typed off the target that compiles it, through TsModuleInfo, and a second anchor pointing at a directory holding only a generated manifest would resolve the same name to nothing.",
        "package_root": "string: exec-root-relative directory the files in `all_files` hang off -- where `package_dir` sits for an extracted tarball, the compiling target's output directory for a workspace member. A file outside it stages at the package root under its basename.",
        "all_files": "depset of File: All files in this package (package.json + .js + .d.ts + other assets). Used by node_modules rule for runtime.",
        "js_files": "depset of File: JavaScript files in this package.",
        "json_files": "depset of File: .json files in this package. A package is free to publish data its consumers import -- `lucide-static/tags.json` -- and `resolveJsonModule` finds nothing unless the file is in the sandbox, which the declaration depsets never put it in.",
        "declaration_files": "depset of File: TypeScript declaration files (.d.ts) in this package, plus the module entry when it is a `.ts`.",
        "direct_deps": "list of NpmPackageInfo: the packages this one depends on directly, each under the name this package imports it by. The flattened transitive closure cannot answer which version an individual package resolved to, which is what a node_modules tree needs to place two versions of one name.",
        "transitive_deps": "depset of NpmPackageInfo: Transitive npm dependencies.",
        "transitive_package_dirs": "depset of File: package.json files for this package and all transitive deps.",
        "exports_types_file": "File or None: the declaration the package's own metadata designates for a `compilerOptions.types` entry or a `/// <reference types>` directive -- the `exports` root, `typings`, `types`, `main`, then `index.d.ts`, declarations only. `types_entry_file` in ts_compile.bzl resolves the entry to it.",
        "module_entry_file": "File or None: the file a bare import of the package resolves to, in TypeScript's order for a bare specifier: the same walk with `.ts` and `.tsx` taken ahead of the `.d.ts` beside a `.js` target, and `index.ts`, `index.tsx` ahead of `index.d.ts`. ts_compile and tsconfig_aspect pin the package's `paths` key to it; a `.ts` one is in `declaration_files` so the compile action stages it. @cloudflare/workers-types ships `index.ts` for this role and `index.d.ts` for the other.",
        "subpath_types": "dict of string to File: each non-root `exports` subpath designating a declaration, mapped to that file. A consumer names one in compiler_options[\"types\"] to put it in its tsconfig `files`, since tsconfig `types` resolves through a node_modules this ruleset does not have; a subpath the map leaves unnamed is looked for among `declaration_files` instead, by `types_entry_file` in ts_compile.bzl.",
        "subpath_patterns": "dict of string to string: each one-star `exports` subpath, mapped to the package-relative pattern it designates with the star and any suffix kept (`./utils/*` to `dist/types/utils/*.d.ts`), or to the one file every match resolves to when the target has no star. ts_compile and tsconfig_aspect write it as the first value of the `<name><subpath>` `paths` key, ahead of the wildcard's layout guesses: TypeScript substitutes the matched star into the whole value and reads no `exports` map for a `paths` match.",
        "type_references": "dict of string to list of string: for each declaration this package designates -- its entry, its `exports` subpaths -- keyed by exec path, the packages its `/// <reference types=...>` directives name, collected through the `/// <reference path=...>` siblings the file pulls in. TypeScript resolves the directive through typeRoots and a node_modules walk from the referencing file, never through `paths`, so a consumer that lists the file in tsconfig `files` resolves each name against `direct_deps` and lists the answer too -- @types/bun is exactly one such directive, forwarding to bun-types.",
        "ambient_types_file": "File or None: On an @types/* package, the entry-point .d.ts a consumer lists in tsconfig `files` to bring its globals and `declare module` blocks into the program. None on every other package.",
        "types_package_dir": "File or None: the package.json at the root of the @types/* package paired with this one, or None when nothing is paired. npm resolves `x` from `node_modules/@types/x` as a package, so an answer derived from one of its declaration files instead names whichever directory that file sits in.",
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

The shipped implementation is Vite (`//vite:dev_server`), the default of
`ts_dev_server(server = ...)`; another is any rule returning this provider.

Two things differ between implementations and neither can be papered over.
A server shipping as an npm package has no `File` to point at -- its executable
is a path inside the `node_modules` tree artifact, which Starlark cannot address
at analysis time -- so it sets `server_in_tree` and leaves `server_binary` None.
A native binary is the other way round; exactly one of the two must be set.

`config_dialect` names the config the server is handed; only Vite's is
generated. A server need not read all of it, and a field it drops is not always
a field it does without: one taking the serve root from argv instead says so in
`argv`, and one ignoring a field says so in `ignored_config_fields`.
""",
    fields = {
        "server_binary": "File or None: the server executable, for a server that is a build artifact. None when the server ships inside the npm tree, in which case server_in_tree names it instead.",
        "server_in_tree": "string: the server executable's path relative to the root of the node_modules tree, for a server that ships as an npm package. Empty when server_binary is set.",
        "argv": "list of string: the command line after the executable. `{config}` expands to the generated config's path and `{root}` to the directory being served; a server taking either somewhere other than where the other one takes it says so here rather than in the launcher.",
        "config_dialect": "string: which config format this server is handed. Only \"vite\" is generated today; a server reading its own format declares its own dialect, and the generator has to learn it before that server can be selected.",
        "runs_in_js_runtime": "bool: True when the executable is JavaScript and the toolchain Node runs it, False for a native binary. A native server still gets the toolchain Node on PATH: one whose plugin host is a Node process is not a Node-free one.",
        "ignored_config_fields": "list of string: dotted config paths this server does not honour, e.g. [\"server.open\"]. A target whose configuration depends on one of these fails at analysis time naming the field and the server, rather than starting a server that quietly does something else.",
        "native_react_refresh": "bool: True when the server applies React Fast Refresh itself. `react_refresh = True` then fails rather than stacking @vitejs/plugin-react on top of a transform that already ran.",
        "runtime_deps": "depset of File: everything the server needs in runfiles beyond the generated config and the npm tree.",
    },
)

BundlerInfo = provider(
    doc = """Information about a JavaScript bundler.

BundlerInfo is the interface behind ts_binary's `bundler` attr. The ruleset
ships no implementation; a consumer writes a rule that returns this provider
and names it there.

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
  ts_binary generates a Vite lib-mode vite.config.mjs and invokes the bundler
  with four exec-root-relative arguments: that config, the entry .js, the
  output directory and the stylesheet to create. The binary runs Vite over the
  config with EXEC_ROOT, VITE_ENTRY_PATH and VITE_OUT_DIR set; see
  ts/private/bundle_action.bzl for the outputs it must produce.
""",
    fields = {
        "bundler_binary": "File: The bundler CLI executable.",
        "config_file": "File or None: Optional static bundler config file passed via --config (mode 1 only).",
        "runtime_deps": "depset of File: Additional files needed by the bundler at runtime.",
        "use_generated_config": "bool: When True, ts_binary generates a vite.config.mjs and invokes bundler_binary in mode 2. Default False.",
    },
)
