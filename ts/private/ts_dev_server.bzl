"""Dev server rule that starts Vite in dev mode.

ts_dev_server is an executable rule.  Running it with `bazel run //app:dev`
starts a Vite development server that serves first-party source, and reads
bazel-bin only for what Vite cannot produce itself.

Architecture
────────────
Dev and prod want opposite things from the same import, and get them.

`vite build` (ts_bundle): Bazel compiled every first-party .ts to .js under
bazel-bin, and the plugin redirects imports there.  Bazel owns the transform.

`bazel run //app:dev`: Bazel is OUT of the inner loop.  Vite transforms
checked-in first-party source in memory, so a keystroke reaches the browser
without a Bazel analysis and action cycle in between.  bazel-bin stays
authoritative for what Vite cannot produce itself: the npm tree the
`node_modules` attr built, `ts_codegen` output (route trees, generated protos),
and non-source assets and passthrough .d.ts.  Generated code is recognised by
having no checked-in source, not by a list of paths that would drift out of
sync with ts_codegen.

Serving source means the dev server does not typecheck.  That is native parity
-- Vite has never typechecked, tsserver does -- but it makes editor correctness
load-bearing: a type error now surfaces in the editor and in `bazel build`, and
no longer blocks the browser update.

This rule generates:

  1. A vite.config.mjs for dev mode that:
     - Sets `root` to the workspace root (BUILD_WORKSPACE_DIRECTORY when running
       under `bazel run`, or the runfiles directory otherwise).
     - Configures `server.fs.allow` to serve files from bazel-bin.
     - Installs the `bazel:npm-resolve` plugin, which is what makes a bare
       `import "react"` from first-party source resolve at all. Vite has no
       search-path option (`resolve.modules` is webpack's): it resolves a bare
       specifier by walking up from the importer, and nothing above a checked-in
       source file is a node_modules directory -- the npm tree is a Bazel
       output. The plugin hands the specifier straight back to Vite's own
       resolver, anchored at that package's package.json inside the tree, so
       exports maps, conditions and subpaths stay Vite's to interpret rather
       than this rule's to reimplement.
     - Loads @vitejs/plugin-react (`react_refresh = True`) at the entry point
       that package's own `exports` map declares.
     - Imports the `vite_config` file from a copy in bazel-bin rather than from
       the source tree; see the attr doc for what such a config may import.
     - Emits one `resolve.alias` entry per first-party `module_name` in the
       graph, pointing at that package's SOURCE, so that `@scope/pkg` and a
       relative import of the same file are one module in Vite's graph rather
       than two copies of it.  The mapping is the TsModuleInfo one ts_compile
       already writes into tsconfig `paths` -- not a second source of truth.
     - Exports `bazelConfigInputs`, the inputs it was generated from (below).
     - Optionally uses the vite-plugin-bazel plugin (when the `plugin` attr is
       set) for resolution and HMR support.

  2. A launcher config (read by //tools/launcher) that:
     - Runs the Node binary of the resolved JS runtime toolchain, from runfiles.
     - Resolves the vite CLI from the node_modules runfile tree.
     - Exits with a diagnostic rather than reaching for a host `node` or `vite`.
     - Sets BAZEL_BIN_DIR to the bazel-bin symlink so the plugin can find outputs.
     - cd's to BUILD_WORKSPACE_DIRECTORY (set by `bazel run`).
     - Runs: node vite dev --config <generated_config>

ibazel integration
──────────────────
`ibazel run //app:dev` SIGTERMs the launcher after every rebuild, and the
launcher deliberately survives it (//tools/launcher, Supervise with
IgnoreTerm): one Vite process lives across every rebuild.  So the
restart-or-keep decision is not ibazel's to make -- it is made inside that
process, by this generated config and the plugin:

    changed                   handled by             restart Vite?
    ───────────────────────── ────────────────────── ─────────────
    first-party source        Vite transform → HMR   no (Bazel uninvolved)
    ts_codegen output         bazel-bin watcher      no
    this generated config     ConfigWatcher          yes
    npm tree / Vite version   ConfigWatcher          yes, plus a warning that
    toolchain node binary     ConfigWatcher          only a new `bazel run`
                                                     really replaces them

The config exports `bazelConfigInputs`: for each input a path, which digest
identifies it, and whether an in-process restart can fix it.  Content digests,
not timestamps -- Bazel rewrites outputs on every action, so an mtime says
nothing about whether anything changed.

Vite restarts itself when its own config file changes, but has no concept of
the thing that GENERATES its config, because natively nothing does.  That is
what the ConfigWatcher adds.

Usage:

    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_dev_server")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    ts_compile(
        name = "app",
        srcs = ["app.tsx"],
        deps = [...],
    )

    node_modules(
        name = "node_modules",
        deps = ["@npm//:vite", "@npm//:react", ...],
    )

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        port = 5173,
    )

    # With the Bazel Vite plugin for better .ts resolution and HMR:
    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        plugin = "//vite:vite_plugin_bazel",
        port = 5173,
    )

    # With React Fast Refresh (preserves component state across HMR updates):
    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",  # must include @npm//:vitejs_plugin-react
        react_refresh = True,
        port = 5173,
    )

    # Run with:
    #   bazel run //app:dev
    # Or with ibazel, so a ts_codegen rebuild or a config change is picked up:
    #   ibazel run //app:dev
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:providers.bzl", "BundlerInfo", "DevServerInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_compile.bzl", "TsModuleInfo")

# ─── Config generation ─────────────────────────────────────────────────────────

def _bin_relative(f):
    """The path of a generated file relative to the bazel-bin symlink."""
    if f.short_path.startswith("../"):
        return "external/" + f.short_path[3:]
    return f.short_path

def _generate_dev_config(
        ctx,
        node_modules_rl,
        plugin_rl,
        react_refresh,
        modules,
        runtime_rl,
        user_config_rl = ""):
    """Generates a vite.config.mjs for dev server mode.

    The config is designed to work in conjunction with the launcher:
      - BAZEL_BIN_DIR env var is set to the bazel-bin path.
      - BUILD_WORKSPACE_DIRECTORY is set by `bazel run`.
      - NODE_MODULES_PATH env var is set to the generated node_modules tree.
      - VITE_PLUGIN_PATH env var is set to the compiled vite-plugin-bazel .mjs
        (only when the plugin attr is set).
      - VITE_USER_CONFIG_PATH env var is set to the user-supplied plugin config
        (only when the vite_config attr is set).

    Args:
        ctx: The rule context.
        node_modules_rl: Runfiles-tree-relative path to the node_modules dir,
            or empty string if node_modules is not set.
        plugin_rl: Runfiles-tree-relative path to the compiled
            vite_plugin_bazel.mjs, or empty string if not set.
        react_refresh: bool, whether to import and use @vitejs/plugin-react
            for React Fast Refresh (HMR that preserves component state).
        modules: list of struct(module_name, source_root) for every first-party
            package in the graph that declared a module_name.
        runtime_rl: Runfiles-tree-relative path of the toolchain node binary,
            so the config can watch the one it is running under.
        user_config_rl: Runfiles-tree-relative path to the bin copy of the
            user-supplied Vite plugin config, or empty string if there is none.
            When set, the generated config dynamically imports it and prepends
            its plugins before the Bazel system plugins.

    Returns:
        The generated vite.config.mjs File.
    """
    port = ctx.attr.port
    host = ctx.attr.host
    open_browser = ctx.attr.open

    # Declared before the content is built: the config watches itself, and only
    # Bazel knows where the file it is about to write will land.
    config_file = ctx.actions.declare_file(
        "{}_dev/vite.config.mjs".format(ctx.label.name),
    )

    open_js = "true" if open_browser else "false"
    host_js = json.encode(host) if host else "true"

    config_content = (
        "// Generated by rules_typescript ts_dev_server for " + str(ctx.label) + "\n" +
        "// DO NOT EDIT — regenerated on every build.\n" +
        "//\n" +
        "// Environment variables read at startup:\n" +
        "//   BUILD_WORKSPACE_DIRECTORY — workspace root (set by `bazel run`)\n" +
        "//   BAZEL_BIN_DIR             — absolute path to the bazel-bin symlink\n" +
        "//   NODE_MODULES_PATH         — absolute path to the Bazel-generated node_modules\n" +
        (
            "//   VITE_PLUGIN_PATH           — absolute path to vite_plugin_bazel.mjs\n" if plugin_rl else ""
        ) +
        (
            "//   VITE_USER_CONFIG_PATH      — absolute path to the user-supplied plugin config\n" if user_config_rl else ""
        ) +
        "\n" +
        "import fs from 'node:fs';\n" +
        "import path from 'node:path';\n" +
        "\n" +
        "// Resolve key directories from environment variables.\n" +
        "// BUILD_WORKSPACE_DIRECTORY is set by `bazel run`; fall back to process.cwd().\n" +
        "const workspaceRoot = process.env['BUILD_WORKSPACE_DIRECTORY'] || process.cwd();\n" +
        "\n" +
        "// bazel-bin is typically a symlink at <workspace>/bazel-bin.\n" +
        "const bazelBin = process.env['BAZEL_BIN_DIR'] || path.join(workspaceRoot, 'bazel-bin');\n" +
        "\n" +
        "// The Bazel-generated node_modules tree (absolute path in runfiles).\n" +
        "const nodeModulesPath = process.env['NODE_MODULES_PATH'] || null;\n" +
        "\n"
    )

    config_content += (
        "// Every first-party package in this graph that declared a module_name,\n" +
        "// as TsModuleInfo reports it: the mapping ts_compile writes into tsconfig\n" +
        "// `paths`, so the editor and the dev server agree what `@scope/pkg` means.\n" +
        "const firstPartyModules = " + json.encode([
            {"name": m.module_name, "dir": m.source_root}
            for m in modules
        ]) + ";\n" +
        "\n" +
        "// In dev the alias points at SOURCE, which is what takes Bazel out of the\n" +
        "// inner loop for a package imported by its bare specifier. A package whose\n" +
        "// source is not checked in is generated, and resolves under bazel-bin.\n" +
        "const firstPartyAliases = [];\n" +
        "for (const mod of firstPartyModules) {\n" +
        "  const dirs = [path.join(workspaceRoot, mod.dir), path.join(bazelBin, mod.dir)];\n" +
        "  const entry = dirs\n" +
        "    .flatMap((dir) => ['index.ts', 'index.tsx'].map((f) => path.join(dir, f)))\n" +
        "    .find((candidate) => fs.existsSync(candidate));\n" +
        "  // Exact match first, and as a RegExp: a string `find` also matches every\n" +
        "  // subpath under it, which would rewrite `@scope/pkg/button` to\n" +
        "  // `<pkg>/index.ts/button`.\n" +
        "  if (entry) {\n" +
        "    const escaped = mod.name.replace(/[.*+?^${}()|[\\]\\\\]/g, '\\\\$&');\n" +
        "    firstPartyAliases.push({ find: new RegExp('^' + escaped + '$'), replacement: entry });\n" +
        "  }\n" +
        "  const dir = dirs.find((candidate) => fs.existsSync(candidate));\n" +
        "  if (dir) firstPartyAliases.push({ find: mod.name, replacement: dir });\n" +
        "}\n" +
        "\n" +
        "// The inputs this config was generated from. A rebuild that changes one of\n" +
        "// them leaves the running server configured for a graph that no longer\n" +
        "// exists; a rebuild that only rewrote ts_codegen output leaves it correct,\n" +
        "// and the bazel-bin watcher turns that into HMR instead of a restart.\n" +
        "const configInputs = [\n" +
        "  {\n" +
        "    label: 'the generated vite config',\n" +
        "    path: path.join(bazelBin, " + json.encode(_bin_relative(config_file)) + "),\n" +
        "    digest: 'content',\n" +
        "    remedy: 'restart',\n" +
        "  },\n" +
        "];\n" +
        "if (nodeModulesPath) {\n" +
        "  configInputs.push({\n" +
        "    label: 'vite in the Bazel npm tree',\n" +
        "    path: path.join(nodeModulesPath, 'vite', 'package.json'),\n" +
        "    digest: 'content',\n" +
        "    remedy: 'manual',\n" +
        "  });\n" +
        "}\n" +
        "// The runfiles symlink, not process.execPath: execPath is already resolved\n" +
        "// to the old toolchain's real file, which a new toolchain does not touch.\n" +
        "const runfilesDir = process.env['RUNFILES_DIR'];\n" +
        "if (runfilesDir) {\n" +
        "  configInputs.push({\n" +
        "    label: 'the toolchain node binary',\n" +
        "    path: path.join(runfilesDir, " + json.encode(runtime_rl) + "),\n" +
        "    digest: 'identity',\n" +
        "    remedy: 'manual',\n" +
        "  });\n" +
        "}\n" +
        "\n" +
        "export const bazelConfigInputs = configInputs;\n" +
        "\n"
    )

    # A static `import react from '@vitejs/plugin-react'` resolves from the config
    # file's own directory, which is a generated one with no node_modules above it.
    if react_refresh:
        react_load_failed = json.encode(
            "[ts_dev_server] {} sets react_refresh = True, but @vitejs/plugin-react did not ".format(ctx.label) +
            "load from the Bazel node_modules tree. Add @npm//:vitejs_plugin-react to the deps " +
            "of the node_modules() target this dev server uses. Cause: ",
        )
        config_content += (
            "// A package's own `exports` map is the only authority on its entry point;\n" +
            "// a path into its dist/ is a guess at a layout the package reorganises.\n" +
            "function npmExportsEntry(node) {\n" +
            "  if (typeof node === 'string') return node;\n" +
            "  if (node === null || typeof node !== 'object') return null;\n" +
            "  if (Array.isArray(node)) {\n" +
            "    for (const alternative of node) {\n" +
            "      const hit = npmExportsEntry(alternative);\n" +
            "      if (hit) return hit;\n" +
            "    }\n" +
            "    return null;\n" +
            "  }\n" +
            "  const keys = Object.keys(node);\n" +
            "  if (keys.some((key) => key.startsWith('.'))) return npmExportsEntry(node['.']);\n" +
            "  for (const condition of ['import', 'module', 'default']) {\n" +
            "    if (condition in node) {\n" +
            "      const hit = npmExportsEntry(node[condition]);\n" +
            "      if (hit) return hit;\n" +
            "    }\n" +
            "  }\n" +
            "  return null;\n" +
            "}\n" +
            "\n" +
            "function npmEntryPath(pkg) {\n" +
            "  const dir = path.join(nodeModulesPath, pkg);\n" +
            "  const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'));\n" +
            "  const entry = npmExportsEntry(manifest.exports) || manifest.module || manifest.main;\n" +
            "  if (!entry) throw new Error(pkg + ' in ' + dir + ' declares no entry point');\n" +
            "  return path.join(dir, entry);\n" +
            "}\n" +
            "\n" +
            "let react;\n" +
            "try {\n" +
            "  react = (await import(npmEntryPath('@vitejs/plugin-react'))).default;\n" +
            "} catch (err) {\n" +
            "  throw new Error(" + react_load_failed + " + err.message);\n" +
            "}\n" +
            "\n"
        )

    # Add plugin import when plugin is wired in.
    # We use a dynamic import pattern to load the plugin from the env-var path.
    # Since vite.config.mjs is evaluated as ESM, we can use a top-level await
    # or use createRequire for the dynamic load.
    # The simplest approach: conditionally use the plugin via dynamic import().
    if plugin_rl:
        config_content += (
            "// Load the vite-plugin-bazel from the runfiles path.\n" +
            "// The plugin path is passed via VITE_PLUGIN_PATH env var.\n" +
            "const pluginPath = process.env['VITE_PLUGIN_PATH'];\n" +
            "let bazelPluginFn = null;\n" +
            "if (pluginPath) {\n" +
            "  try {\n" +
            "    const mod = await import(pluginPath);\n" +
            "    bazelPluginFn = mod.bazelPlugin;\n" +
            "  } catch (err) {\n" +
            "    console.warn('[ts_dev_server] Failed to load vite-plugin-bazel:', err.message);\n" +
            "  }\n" +
            "}\n" +
            "\n"
        )

    if user_config_rl:
        config_content += (
            "// The user's vite_config, as VITE_USER_CONFIG_PATH points at it: the bin\n" +
            "// copy, so its own bare imports resolve beside the Bazel npm tree rather\n" +
            "// than in the source tree. It must default-export a `plugins` array.\n" +
            "const userConfigPath = process.env['VITE_USER_CONFIG_PATH'];\n" +
            "let _userPlugins = [];\n" +
            "if (userConfigPath) {\n" +
            "  try {\n" +
            "    const _userMod = await import(userConfigPath);\n" +
            "    const _userCfg = _userMod.default || _userMod;\n" +
            "    if (Array.isArray(_userCfg.plugins)) {\n" +
            "      _userPlugins = _userCfg.plugins;\n" +
            "    }\n" +
            "  } catch (err) {\n" +
            "    throw new Error('[rules_typescript] Failed to load vite_config: ' + err.message);\n" +
            "  }\n" +
            "}\n" +
            "\n"
        )

    config_content += (
        "// Build the list of directories Vite's dev server is allowed to serve.\n" +
        "const fsAllow = [workspaceRoot, bazelBin];\n" +
        "if (nodeModulesPath) fsAllow.push(nodeModulesPath);\n" +
        "\n"
    )

    if node_modules_rl:
        config_content += (
            "// Vite resolves a bare specifier by walking up from the importer, and above\n" +
            "// a checked-in source file there is no node_modules to find -- the npm tree\n" +
            "// is a Bazel output elsewhere. So the id goes back to Vite's own resolver\n" +
            "// with an importer that does have it above them: the package's own manifest\n" +
            "// inside the tree. Exports maps, conditions and subpaths stay Vite's.\n" +
            "const bazelNpmResolve = {\n" +
            "  name: 'bazel:npm-resolve',\n" +
            "  enforce: 'pre',\n" +
            "  async resolveId(id, importer, options) {\n" +
            "    if (id.startsWith('.') || id.startsWith('/') || id.includes(':') || id.includes('\\0')) {\n" +
            "      return null;\n" +
            "    }\n" +
            "    const segments = id.split('/');\n" +
            "    const pkg = id.startsWith('@') ? segments.slice(0, 2).join('/') : segments[0];\n" +
            "    const manifest = path.join(nodeModulesPath, pkg, 'package.json');\n" +
            "    if (!fs.existsSync(manifest)) return null;\n" +
            "    return this.resolve(id, manifest, { ...options, skipSelf: true });\n" +
            "  },\n" +
            "};\n" +
            "\n"
        )

    # User plugins first: a framework transform has to see a module before the
    # Bazel ones. The npm resolver last, so anything above it can claim an id.
    config_content += "const plugins = [{}];\n".format("..._userPlugins" if user_config_rl else "")
    if react_refresh:
        config_content += (
            "// React Fast Refresh — preserves component state across HMR updates.\n" +
            "plugins.push(react());\n"
        )
    if plugin_rl:
        config_content += (
            "if (bazelPluginFn) {\n" +
            "  plugins.push(bazelPluginFn({\n" +
            "    bazelBin: bazelBin,\n" +
            "    nodeModules: nodeModulesPath || undefined,\n" +
            "    target: " + json.encode(str(ctx.label)) + ",\n" +
            "    configInputs: bazelConfigInputs,\n" +
            "  }));\n" +
            "}\n"
        )
    if node_modules_rl:
        config_content += "plugins.push(bazelNpmResolve);\n"
    config_content += "\n"

    config_content += (
        "// @type {import('vite').UserConfig}\n" +
        "export default {\n" +
        "  // Serve from the workspace root so that absolute paths in the compiled\n" +
        "  // JS (e.g. /src/components/Button.js) resolve correctly.\n" +
        "  root: workspaceRoot,\n" +
        "\n" +
        "  server: {\n" +
        "    port: " + str(port) + ",\n" +
        "    host: " + host_js + ",\n" +
        "    open: " + open_js + ",\n" +
        "    fs: {\n" +
        "      // Allow Vite to serve files from bazel-bin and the generated\n" +
        "      // node_modules tree (Vite restricts serving by default).\n" +
        "      allow: fsAllow,\n" +
        "    },\n" +
        "    watch: {\n" +
        "      // Include bazel-bin in Vite's file watcher so that changes\n" +
        "      // to compiled .js files trigger HMR updates automatically.\n" +
        "      // ibazel writes new .js files here after each rebuild.\n" +
        "      paths: [bazelBin],\n" +
        "    },\n" +
        "  },\n" +
        "\n" +
        "  resolve: {\n" +
        "    // A first-party module_name resolves to source; a bare npm specifier is\n" +
        "    // left to the bazel:npm-resolve plugin, which Vite runs before its own\n" +
        "    // resolver. There is no resolve.modules: that is a webpack option, and\n" +
        "    // Vite ignores it.\n" +
        "    alias: firstPartyAliases,\n" +
        "  },\n" +
        "\n" +
        "  plugins,\n" +
        "\n"
    )

    config_content += (
        "  // Disable dependency pre-bundling when using a Bazel node_modules tree.\n" +
        "  // The Bazel tree already has all packages at the correct versions;\n" +
        "  // pre-bundling would re-process them unnecessarily.\n" +
        "  optimizeDeps: {\n" +
        "    noDiscovery: nodeModulesPath !== null,\n" +
        "  },\n" +
        "\n" +
        "  // Suppress the 'public dir does not exist' warning when no public/\n" +
        "  // directory exists in the workspace root.\n" +
        "  publicDir: false,\n" +
        "\n" +
        "  logLevel: 'info',\n" +
        "};\n"
    )

    ctx.actions.write(
        output = config_file,
        content = config_content,
    )
    return config_file

# ─── Rule implementation ───────────────────────────────────────────────────────

# The config the generator writes sets these, and each is only reached through
# the attr named beside it. A server that does not read one is only a problem for
# a target that asked for it, so the check is per-attr rather than per-field.
_CONFIG_FIELD_ATTRS = {
    "server.open": "open",
    "server.watch.paths": None,
    "root": None,
    "optimizeDeps.noDiscovery": None,
}

def _check_ignored_fields(ctx, server_info):
    """Fails when a set attr reaches a config field this server does not read."""
    for field in server_info.ignored_config_fields:
        attr_name = _CONFIG_FIELD_ATTRS.get(field)
        if not attr_name:
            continue
        if getattr(ctx.attr, attr_name, None):
            fail(
                "ts_dev_server: {} sets {} = {}, which reaches the generated config as `{}`.\n".format(
                    ctx.label,
                    attr_name,
                    getattr(ctx.attr, attr_name),
                    field,
                ) +
                "The server this target selected ({}) does not read that field, so the\n".format(
                    ctx.attr.server.label,
                ) +
                "setting would be silently dropped rather than applied.\n" +
                "Either drop the attr, or select a server that reads it:\n" +
                "    server = \"@rules_typescript//vite:dev_server\"",
            )

def _resolve_server(ctx):
    """Reads DevServerInfo off the server attr and checks it is self-consistent."""
    server_info = ctx.attr.server[DevServerInfo]
    if server_info.config_dialect != "vite":
        fail(
            "ts_dev_server: server '{}' declares config_dialect '{}'.\n".format(
                ctx.attr.server.label,
                server_info.config_dialect,
            ) +
            "This rule only generates a Vite-dialect config; a server reading another\n" +
            "format needs a generator for it before it can be selected here.",
        )
    if bool(server_info.server_binary) == bool(server_info.server_in_tree):
        fail(
            "ts_dev_server: server '{}' must set exactly one of DevServerInfo.server_binary ".format(
                ctx.attr.server.label,
            ) +
            "(a native or built executable) and DevServerInfo.server_in_tree (a path inside " +
            "the node_modules tree); it set " +
            ("both" if server_info.server_binary else "neither") + ".",
        )
    _check_ignored_fields(ctx, server_info)
    return server_info

def _ts_dev_server_impl(ctx):
    entry_point = ctx.attr.entry_point
    if JsInfo not in entry_point:
        fail(
            "ts_dev_server: entry_point '{}' does not provide JsInfo.\n".format(
                ctx.attr.entry_point.label,
            ) +
            "The entry_point attr must be a ts_compile target (or any target that provides JsInfo).\n" +
            "Did you mean: entry_point = \"//path/to:your_ts_compile_target\"?",
        )

    entry_js_info = entry_point[JsInfo]
    server_info = _resolve_server(ctx)

    # ── BundlerInfo (optional) ──────────────────────────────────────────────
    # When a `bundler` attr is provided, its BundlerInfo is collected and the
    # bundler's runtime_deps are added to the runfiles.  The launcher still runs
    # the Vite CLI from the node_modules tree, but the bundler's binary reaches
    # the launcher config as BUNDLER_BINARY so that a non-Vite dev server can be
    # invoked in the future.
    bundler_info = None
    bundler_runtime_files = depset()
    if ctx.attr.bundler:
        if BundlerInfo not in ctx.attr.bundler:
            fail(
                "ts_dev_server: bundler '{}' does not provide BundlerInfo.\n".format(
                    ctx.attr.bundler.label,
                ) +
                "The bundler attr must be a target that provides BundlerInfo " +
                "(e.g. a vite_bundler() or custom bundler rule).\n" +
                "Did you mean: bundler = \"//vite:bundler\"?",
            )
        bundler_info = ctx.attr.bundler[BundlerInfo]
        bundler_runtime_files = bundler_info.runtime_deps

    js_runtime = get_js_runtime(ctx)
    if not js_runtime:
        fail(
            "ts_dev_server: no JS runtime toolchain resolved for '{}'.\n".format(ctx.label) +
            "The dev server runs the toolchain's Node binary out of runfiles; it does " +
            "not fall back to whatever `node` is on your PATH.\n" +
            "Did you mean to register the toolchains in MODULE.bazel?\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )
    runtime_binary = js_runtime.runtime_binary
    runtime_args = js_runtime.args_prefix

    # ── node_modules ───────────────────────────────────────────────────────────
    node_modules_files = ctx.files.node_modules
    node_modules_rl = ""
    if node_modules_files:
        node_modules_rl = rlocation_path(ctx, node_modules_files[0])

    # ── vite-plugin-bazel (optional) ───────────────────────────────────────────
    plugin_files = ctx.files.plugin
    plugin_rl = ""
    if plugin_files:
        plugin_rl = rlocation_path(ctx, plugin_files[0])

    # ── User-supplied vite_config (optional) ────────────────────────────────────
    # A copy in bin, not the source file: Node resolves the runfiles symlink
    # before that file's own imports, which would then leave the Bazel tree.
    user_config = None
    user_config_rl = ""
    if ctx.file.vite_config:
        user_config = ctx.actions.declare_file("{}_dev/user.vite.config.{}".format(
            ctx.label.name,
            ctx.file.vite_config.extension,
        ))
        ctx.actions.expand_template(
            template = ctx.file.vite_config,
            output = user_config,
            substitutions = {},
        )
        user_config_rl = rlocation_path(ctx, user_config)

    # ── First-party module_name mapping ────────────────────────────────────────
    # Materialised because the config file is a list of them; the same depset
    # ts_compile walks to write tsconfig `paths`.
    modules = []
    if TsModuleInfo in entry_point:
        modules = [
            m
            for m in entry_point[TsModuleInfo].transitive_modules.to_list()
            if m.module_name
        ]

    # ── Generate the vite.config.mjs ───────────────────────────────────────────
    react_refresh = ctx.attr.react_refresh
    config_file = _generate_dev_config(
        ctx,
        node_modules_rl,
        plugin_rl,
        react_refresh,
        modules,
        rlocation_path(ctx, runtime_binary),
        user_config_rl,
    )

    # ── Launcher config ────────────────────────────────────────────────────────
    # A server inside the npm tree is a path, not a File: an individual file
    # inside a TreeArtifact has no label at analysis time, so the launcher joins
    # it onto the resolved tree. A native server is a File and needs neither.
    dev_server = {
        "config_file": rlocation_path(ctx, config_file),
        "argv": server_info.argv,
        "runs_in_js_runtime": server_info.runs_in_js_runtime,
        "port": ctx.attr.port,
    }
    if server_info.server_in_tree:
        dev_server["server_in_tree"] = server_info.server_in_tree
    else:
        dev_server["server_binary"] = rlocation_path(ctx, server_info.server_binary)
    if node_modules_files:
        dev_server["node_modules"] = node_modules_rl
    if plugin_files:
        dev_server["plugin"] = plugin_rl
    if user_config:
        dev_server["user_config"] = user_config_rl

    # A non-Vite dev server is invoked from a wrapper that reads BUNDLER_BINARY.
    if bundler_info:
        dev_server["bundler_binary"] = rlocation_path(ctx, bundler_info.bundler_binary)

    launcher = declare_launcher(ctx, {
        "label": str(ctx.label),
        "mode": "devserver",
        "workspace": ctx.workspace_name,
        "runtime": rlocation_path(ctx, runtime_binary),
        "runtime_args": runtime_args,
        "dev_server": dev_server,
    })

    # ── Runfiles ───────────────────────────────────────────────────────────────
    explicit_runfiles = [config_file, runtime_binary] + launcher.files
    explicit_runfiles.extend(node_modules_files)
    explicit_runfiles.extend(plugin_files)
    if user_config:
        explicit_runfiles.append(user_config)
    if bundler_info:
        explicit_runfiles.append(bundler_info.bundler_binary)

    runfiles = ctx.runfiles(
        files = explicit_runfiles,
        root_symlinks = launcher.root_symlinks,
        transitive_files = depset(
            [runtime_binary],
            transitive = [
                entry_js_info.transitive_js_files,
                entry_js_info.transitive_js_map_files,
                bundler_runtime_files,
                server_info.runtime_deps,
            ],
        ),
    )

    # ── Providers ──────────────────────────────────────────────────────────────
    return [
        DefaultInfo(
            executable = launcher.executable,
            files = depset([config_file]),
            runfiles = runfiles,
        ),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_dev_server = rule(
    implementation = _ts_dev_server_impl,
    executable = True,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    attrs = LAUNCHER_ATTRS | {
        "entry_point": attr.label(
            doc = "The ts_compile target that is the application entry point. " +
                  "Must provide JsInfo.",
            providers = [JsInfo],
            mandatory = True,
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target containing vite and all application dependencies. " +
                  "The directory must be named 'node_modules' so that Node.js ESM resolution " +
                  "works correctly. When set, the generated config points module resolution at " +
                  "this tree.",
            allow_files = True,
        ),
        "plugin": attr.label(
            doc = "Optional compiled vite-plugin-bazel JavaScript file. " +
                  "When set (e.g. '//vite:vite_plugin_bazel'), the generated vite.config.mjs " +
                  "imports the plugin, which resolves generated code out of bazel-bin, " +
                  "invalidates precisely on a rebuild, and restarts the server when the " +
                  "config it was generated from changes. Without this attr Vite serves " +
                  "first-party source and nothing else: bazel-bin, and therefore ts_codegen " +
                  "output, is invisible to it. This attr accepts a bundled .mjs file target.",
            allow_single_file = [".mjs", ".js"],
        ),
        "port": attr.int(
            doc = "Port for the Vite dev server. Default: 5173.",
            default = 5173,
        ),
        "host": attr.string(
            doc = "Host to bind the dev server to. Default: 'localhost'. " +
                  "Set to '0.0.0.0' to bind on all interfaces.",
            default = "localhost",
        ),
        "open": attr.bool(
            doc = "Whether to open the browser automatically when the dev server starts.",
            default = False,
        ),
        "bundler": attr.label(
            doc = "Optional bundler target providing BundlerInfo. " +
                  "When set, the bundler's binary and runtime_deps are included in the runfiles " +
                  "tree so that a custom dev server (non-Vite) can be invoked. " +
                  "The default Vite-based dev server does not require this attr — Vite is " +
                  "resolved from the node_modules tree. " +
                  "Example: bundler = \"//vite:bundler\" for explicit Vite bundler wiring.",
            providers = [BundlerInfo],
        ),
        "server": attr.label(
            doc = "Which dev server implementation serves this target, as a " +
                  "target providing DevServerInfo. Defaults to Vite. " +
                  "`@rules_typescript//oj:dev_server` selects oj instead, which " +
                  "reads the same generated config but is a native binary and " +
                  "needs no @npm//:vite in the node_modules tree. A server that " +
                  "does not read a config field this target set is an analysis-" +
                  "time error naming both, so switching implementations cannot " +
                  "silently drop a setting.",
            default = "@rules_typescript//vite:dev_server",
            providers = [DevServerInfo],
        ),
        "react_refresh": attr.bool(
            doc = "Enable React Fast Refresh via @vitejs/plugin-react. " +
                  "When True, the generated vite.config.mjs imports and uses " +
                  "@vitejs/plugin-react so that React component state is preserved " +
                  "across HMR updates instead of being lost on every file change. " +
                  "Requires @vitejs/plugin-react to be included in the node_modules attr. " +
                  "Example: add '@npm//:vitejs_plugin-react' to your node_modules() deps.",
            default = False,
        ),
        "vite_config": attr.label(
            doc = "Optional user-supplied Vite plugin configuration file (.mjs or .js). " +
                  "When set, the generated vite.config.mjs imports this file and prepends " +
                  "its plugins to the Bazel system plugins (react, bazel-plugin, npm " +
                  "resolution), which is how a framework plugin (TanStack Start, Remix) " +
                  "gets to transform a module first. The file must export a default object " +
                  "with a `plugins` array: " +
                  "  export default { plugins: [myFrameworkPlugin()] }; " +
                  "\n\n" +
                  "What such a config may import is a hard boundary, because the rule " +
                  "loads a COPY of it in bazel-bin (a source-tree config would resolve " +
                  "its own imports through a source-tree node_modules, which this ruleset " +
                  "does not have): a bare npm specifier resolves through the tree the " +
                  "node_modules attr built, as long as that target is in the same Bazel " +
                  "package as this one -- that is the directory Node finds walking up from " +
                  "the copy. A RELATIVE import does not resolve: only this one file is " +
                  "copied, so a sibling module is not there to be found, and the load " +
                  "fails with a `[rules_typescript] Failed to load vite_config` error " +
                  "naming it. Keep the config self-contained, or reach the tree explicitly " +
                  "through the NODE_MODULES_PATH environment variable the launcher sets. " +
                  "The copy's path reaches the generated config as VITE_USER_CONFIG_PATH.",
            allow_single_file = [".mjs", ".js"],
        ),
    },
    doc = """Starts a Vite dev server for a TypeScript application.

`bazel run //app:dev` builds the target once and then starts Vite in dev mode.
From there Bazel is out of the inner loop: Vite transforms your first-party
`.ts`/`.tsx` source in memory, so a save reaches the browser as HMR without a
Bazel analysis and action cycle in between. bazel-bin is still where Vite reads
what it cannot produce itself -- `ts_codegen` output, generated assets, and the
npm tree from the `node_modules` attr.

**The dev server does not typecheck.** That is the same deal as a native
`vite dev` (Vite has never typechecked; tsserver and `bazel build` do), but it
makes editor correctness load-bearing: a type error shows up in the editor and
in `bazel build`, and no longer blocks the browser update.

`ibazel run //app:dev` is still worth using for what Bazel does own: a
`ts_codegen` rebuild reaches the browser as HMR, and a rebuild that changed the
server's own configuration -- BUILD deps, a `module_name`, the entry point, the
npm tree -- restarts Vite instead of leaving it serving a graph that no longer
exists. A rebuild that changed neither does nothing, which is the point.

Each first-party `module_name` in the graph becomes a `resolve.alias` entry
pointing at that package's source, so `import "@scope/pkg"` and a relative
import of the same file are one module in Vite's graph.

The node_modules attr must point to a node_modules() rule that includes `vite`
and all packages imported by the application.

The optional plugin attr wires in vite-plugin-bazel, which is what resolves
generated code out of bazel-bin, invalidates precisely on a rebuild, and makes
the restart decision. Without it Vite cannot see bazel-bin at all. Set it to
`//vite:vite_plugin_bazel` to use the compiled plugin from this repository.

Example (basic):

    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_dev_server")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    ts_compile(
        name = "app",
        srcs = glob(["src/**/*.tsx"]),
        deps = ["@npm//:react", "@npm//:react-dom"],
    )

    node_modules(
        name = "node_modules",
        deps = ["@npm//:vite", "@npm//:react", "@npm//:react-dom"],
    )

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        port = 5173,
    )

Example (with vite-plugin-bazel for enhanced HMR):

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        plugin = "@rules_typescript//vite:vite_plugin_bazel",
        port = 5173,
    )

    # Start the dev server:
    #   bazel run //app:dev

    # Same, with codegen rebuilds and config-aware restarts (requires ibazel):
    #   ibazel run //app:dev

Example (with React Fast Refresh — preserves component state across HMR):

    node_modules(
        name = "node_modules",
        deps = [
            "@npm//:vite",
            "@npm//:react",
            "@npm//:react-dom",
            "@npm//:vitejs_plugin-react",  # required for react_refresh = True
        ],
    )

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        react_refresh = True,
        port = 5173,
    )

    # Or combine with vite-plugin-bazel for both Fast Refresh and .ts resolution:
    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        plugin = "@rules_typescript//vite:vite_plugin_bazel",
        react_refresh = True,
        port = 5173,
    )

Example (with explicit bundler wiring via BundlerInfo):

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        bundler = "//vite:bundler",
        port = 5173,
    )

The bundler attr is optional and exists to allow custom dev server implementations.
When set, the bundler's binary is available in runfiles as $BUNDLER_BINARY.
The default workflow (Vite from node_modules) does not require the bundler attr.
""",
)
