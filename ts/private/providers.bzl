"""Provider definitions for rules_typescript.

A `direct` field carries only what the target itself produces. A rule that just
forwards a dep's files -- ts_compile relative to a dep's data files, say --
leaves the direct field empty and puts the closure in the transitive one; a
consumer that wants everything reachable reads the transitive field.
"""

JsInfo = provider(
    doc = "Provider for JavaScript compilation outputs.",
    fields = {
        "js_files": "depset of File: .js files this target produces -- compiled output plus any JavaScript src staged as-is.",
        "js_map_files": "depset of File: .js.map source map files this target produces.",
        "transitive_js_files": "depset of File: Transitive closure of all .js files from this target and its deps.",
        "transitive_js_map_files": "depset of File: Transitive closure of all .js.map files.",
        "data_files": "depset of File: the srcs that are neither TypeScript, " +
                      "JavaScript nor declarations, staged into the output " +
                      "tree at their package-relative paths beside the .js.",
        "transitive_data_files": "depset of File: the data files of this " +
                                 "target and its deps, what a compiled " +
                                 "module reaches beside itself at run time.",
        "source_files": "depset of File: the TypeScript srcs -- .ts, .tsx " +
                        "and declarations. A ts_test in the same package " +
                        "stages them at their source paths.",
    },
)

TsDeclarationInfo = provider(
    doc = "Provider for TypeScript declaration outputs (.d.ts files).",
    fields = {
        "declaration_files": "depset of File: declaration files this target produces, plus the ambient ones it passes through from srcs; on an npm package, its module entry too when that is a `.ts`. A global one is in scope in a consumer only when the consumer's tsconfig `types` names it.",
        "transitive_declaration_files": "depset of File: the .d.ts files this target and its non-npm deps produce, transitively. An npm package's declarations reach a consumer through the node_modules forest its tsgo action stages, not through this depset.",
        "transitive_npm_packages": "depset of NpmPackageInfo: the npm packages a consumer links in its node_modules forest for this target's declarations. A dep's emitted .d.ts imports the packages the dep declared and resolves them in the consumer's program by walking that forest. An npm package target names its transitive_deps; the package itself arrives through its NpmPackageInfo.",
    },
)

TsConfigInfo = provider(
    doc = """A tsconfig.json, the files it extends and its jsx when preserve.

Starlark cannot read the file, so a ts_config target declares what a rule needs
from it before any action runs: the `extends` chain, every file of which becomes
an action input, and the one compiler option that names an output.
""",
    fields = {
        "tsconfig": "File: The tsconfig.json this target declares.",
        "deps_tsconfigs": "depset of File: Every file `tsconfig` extends, transitively.",
        "jsx": "string: \"preserve\" when the chain's effective jsx is " +
               "preserve, so a .tsx emits .jsx as under tsc; \"\" otherwise.",
    },
)

NpmPackageInfo = provider(
    doc = "Provider for npm package targets.",
    fields = {
        "package_name": "string: npm package name (e.g., 'react').",
        "package_version": "string: npm package version.",
        "peer_id": "string: a filesystem-safe token naming the peer set this resolution was made against, empty for a package pnpm resolved only one way. Two snapshots can share name@version and differ only here, and they are two different dependency graphs, so anything keying a package by name and version alone merges them.",
        "package_dir": "File or None: The package.json file at the root of the extracted package. None on a pnpm workspace member, which was never extracted from a tarball: its view writes the manifest it links.",
        "package_root": "string: exec-root-relative directory the files in `all_files` hang off -- where `package_dir` sits for an extracted tarball, the member's directory under bazel-bin for a workspace member. A file outside it stages at the package root under its basename.",
        "all_files": "depset of File: All files in this package (package.json + .js + .d.ts + other assets): what a node_modules tree holds for it, the runtime tree's and the type-check forest's alike.",
        "js_files": "depset of File: JavaScript files in this package.",
        "direct_deps": "list of NpmPackageInfo: the packages this one depends on directly, each under the name this package imports it by. The flattened transitive closure cannot answer which version an individual package resolved to, which is what a node_modules tree needs to place two versions of one name.",
        "transitive_deps": "depset of NpmPackageInfo: Transitive npm dependencies.",
        "transitive_package_dirs": "depset of File: package.json files for this package and all transitive deps.",
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
