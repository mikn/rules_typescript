"""Dev server rule that starts Vite in dev mode.

ts_dev_server is an executable rule.  Running it with `bazel run //app:dev`
starts a Vite development server that serves compiled JavaScript from bazel-bin.

Architecture
────────────
Bazel (or ibazel) compiles .ts → .js under bazel-bin/.  This rule generates:

  1. A vite.config.mjs for dev mode that:
     - Sets `root` to the workspace root (BUILD_WORKSPACE_DIRECTORY when running
       under `bazel run`, or the runfiles directory otherwise).
     - Configures `server.fs.allow` to serve files from bazel-bin.
     - Points `resolve.modules` at the Bazel-generated node_modules tree so that
       `import "react"` finds the right packages.
     - Optionally uses the vite-plugin-bazel plugin (when the `plugin` attr is
       set) for .ts-to-.js resolution and HMR support when run with ibazel.

  2. A runner shell script that:
     - Locates the Node runtime (JS runtime toolchain or falls back to `node`).
     - Resolves the vite CLI from the node_modules runfile tree.
     - Sets BAZEL_BIN_DIR to the bazel-bin symlink so the plugin can find outputs.
     - cd's to BUILD_WORKSPACE_DIRECTORY (set by `bazel run`).
     - Exec's: node vite --config <generated_config> --port <port> [--open]

ibazel integration
──────────────────
When run via `ibazel run //app:dev`, ibazel:
  1. Builds the target (compiles .ts → .js under bazel-bin).
  2. Starts the runner script for the first build.
  3. On subsequent rebuilds, sends SIGTERM to the runner, rebuilds, then
     restarts.  (This is ibazel's default "run" behaviour.)

The preferred integration is to keep the dev server alive across rebuilds.
The generated vite.config.mjs watches bazel-bin for .js file changes via
Vite's built-in file watcher (server.watch), so Vite picks up newly compiled
files and sends HMR updates without requiring a server restart.

When vite-plugin-bazel is wired in via the `plugin` attr, the plugin intercepts
.ts import resolution and redirects to .js in bazel-bin, and uses a bazel-bin
file watcher to trigger HMR updates precisely.

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
    # Or with ibazel for live reloading:
    #   ibazel run //app:dev
"""

load("//ts/private:providers.bzl", "BundlerInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# ─── Config generation ─────────────────────────────────────────────────────────

def _generate_dev_config(ctx, node_modules_rl, plugin_rl, react_refresh, user_config_rl = ""):
    """Generates a vite.config.mjs for dev server mode.

    The config is designed to work in conjunction with the runner script:
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
        user_config_rl: Runfiles-tree-relative path to the user-supplied Vite
            plugin config file (.mjs/.js), or empty string if not set. When set,
            the generated config dynamically imports this file and prepends its
            plugins before the Bazel system plugins.

    Returns:
        The generated vite.config.mjs File.
    """
    port = ctx.attr.port
    host = ctx.attr.host
    open_browser = ctx.attr.open

    open_js = "true" if open_browser else "false"
    host_js = '"{}"'.format(_shell_escape(host)) if host else "true"

    config_content = (
        "// Generated by rules_typescript ts_dev_server for " + str(ctx.label) + "\n" +
        "// DO NOT EDIT — regenerated on every build.\n" +
        "//\n" +
        "// Environment variables read at startup:\n" +
        "//   BUILD_WORKSPACE_DIRECTORY — workspace root (set by `bazel run`)\n" +
        "//   BAZEL_BIN_DIR             — absolute path to the bazel-bin symlink\n" +
        "//   NODE_MODULES_PATH         — absolute path to the Bazel-generated node_modules\n" +
        (
            "//   VITE_PLUGIN_PATH           — absolute path to vite_plugin_bazel.mjs\n"
            if plugin_rl else ""
        ) +
        (
            "//   VITE_USER_CONFIG_PATH      — absolute path to the user-supplied plugin config\n"
            if user_config_rl else ""
        ) +
        "\n" +
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

    # Add react dynamic import when react_refresh is enabled.
    # We cannot use a static `import react from '@vitejs/plugin-react'` here
    # because Node resolves bare specifiers relative to the config file's
    # directory (inside the runfiles tree), where there is no node_modules/.
    # Instead we use a dynamic import() with the absolute path derived from
    # NODE_MODULES_PATH, matching the pattern used for vite-plugin-bazel.
    if react_refresh:
        config_content += (
            "// Load @vitejs/plugin-react dynamically from the Bazel node_modules tree.\n" +
            "// A static import would fail because the config file lives in runfiles,\n" +
            "// where there is no node_modules/ directory for bare-specifier resolution.\n" +
            "let react = null;\n" +
            "if (nodeModulesPath) {\n" +
            "  try {\n" +
            "    const reactMod = await import(nodeModulesPath + '/@vitejs/plugin-react/dist/index.mjs');\n" +
            "    react = reactMod.default;\n" +
            "  } catch (err) {\n" +
            "    console.warn('[ts_dev_server] Failed to load @vitejs/plugin-react:', err.message);\n" +
            "  }\n" +
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
            "// Load the user-supplied Vite plugin config from the runfiles path.\n" +
            "// The config path is passed via VITE_USER_CONFIG_PATH env var.\n" +
            "// The file must export a default object with a `plugins` array.\n" +
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
        "\n" +
        "// Resolve modules: prefer Bazel-generated node_modules, then fallback\n" +
        "// to workspace-root node_modules for compatibility.\n" +
        "const resolveModules = nodeModulesPath\n" +
        "  ? [nodeModulesPath, 'node_modules']\n" +
        "  : ['node_modules'];\n" +
        "\n"
    )

    if plugin_rl or react_refresh or user_config_rl:
        config_content += (
            "// Build the plugins array.\n" +
            "// User-supplied plugins (from vite_config attr) run first so that\n" +
            "// framework transforms execute before Bazel system plugins.\n" +
            "const plugins = [..._userPlugins];\n" if user_config_rl else
            "// Build the plugins array.\n" +
            "const plugins = [];\n"
        )
        if react_refresh:
            config_content += (
                "// React Fast Refresh — preserves component state across HMR updates.\n" +
                "if (react) plugins.push(react());\n"
            )
        if plugin_rl:
            config_content += (
                "if (bazelPluginFn) {\n" +
                "  plugins.push(bazelPluginFn({\n" +
                "    bazelBin: bazelBin,\n" +
                "    nodeModules: nodeModulesPath || undefined,\n" +
                "    target: '" + _shell_escape(str(ctx.label)) + "',\n" +
                "  }));\n" +
                "}\n"
            )
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
        "    // Point module resolution at the Bazel-generated node_modules tree.\n" +
        "    // This ensures `import 'react'` finds the Bazel-managed package.\n" +
        "    modules: resolveModules,\n" +
        "  },\n" +
        "\n"
    )

    if plugin_rl or react_refresh or user_config_rl:
        config_content += (
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

    config_file = ctx.actions.declare_file(
        "{}_dev/vite.config.mjs".format(ctx.label.name),
    )
    ctx.actions.write(
        output = config_file,
        content = config_content,
    )
    return config_file

# ─── Rule implementation ───────────────────────────────────────────────────────

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

    # ── BundlerInfo (optional) ──────────────────────────────────────────────
    # When a `bundler` attr is provided, its BundlerInfo is collected and the
    # bundler's runtime_deps are added to the runfiles.  The generated runner
    # script currently still uses the bundler-embedded Vite CLI (mode 2 or the
    # default node_modules Vite path), but the bundler's binary and runtime_deps
    # are made available so that a non-Vite dev server can be invoked in the
    # future by extending the runner template below.
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

    # Resolve the JS runtime (toolchain or fall back to system node).
    runtime_binary = None
    runtime_args = []
    js_runtime = get_js_runtime(ctx)
    if js_runtime:
        runtime_binary = js_runtime.runtime_binary
        runtime_args = js_runtime.args_prefix

    # Helper: convert a file's short_path to its runfiles-tree-relative path.
    workspace_name = ctx.workspace_name
    def _rl(short_path):
        if short_path.startswith("../"):
            return short_path[3:]  # strip leading "../"
        return workspace_name + "/" + short_path

    # ── node_modules ───────────────────────────────────────────────────────────
    node_modules_files = ctx.files.node_modules
    node_modules_rl = ""
    if node_modules_files:
        node_modules_rl = _rl(node_modules_files[0].short_path)

    # ── vite-plugin-bazel (optional) ───────────────────────────────────────────
    plugin_files = ctx.files.plugin
    plugin_rl = ""
    if plugin_files:
        plugin_rl = _rl(plugin_files[0].short_path)

    # ── User-supplied vite_config (optional) ────────────────────────────────────
    vite_config_files = ctx.files.vite_config
    user_config_rl = ""
    if vite_config_files:
        user_config_rl = _rl(vite_config_files[0].short_path)

    # ── Generate the vite.config.mjs ───────────────────────────────────────────
    react_refresh = ctx.attr.react_refresh
    config_file = _generate_dev_config(ctx, node_modules_rl, plugin_rl, react_refresh, user_config_rl)

    # ── Locate Vite CLI ────────────────────────────────────────────────────────
    # vite/bin/vite.js lives inside the node_modules directory tree artifact.
    # We cannot reference individual files inside a TreeArtifact at analysis
    # time, so we compute the path dynamically in the runner script using the
    # node_modules directory artifact's runfiles path as a base.
    vite_rel_path = "vite/bin/vite.js"  # relative to node_modules root

    # ── Generate the runner script ─────────────────────────────────────────────
    runtime_path_rl = _shell_escape(_rl(runtime_binary.short_path)) if runtime_binary else ""
    runtime_args_str = " ".join(['"{}"'.format(_shell_escape(a)) for a in runtime_args])
    config_rl = _shell_escape(_rl(config_file.short_path))
    node_modules_rl_esc = _shell_escape(node_modules_rl)
    plugin_rl_esc = _shell_escape(plugin_rl)
    user_config_rl_esc = _shell_escape(user_config_rl)

    runner = ctx.actions.declare_file("{}_runner.sh".format(ctx.label.name))
    runner_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated dev server runner for " + str(ctx.label) + "\n" +
        "# Run with:  bazel run " + str(ctx.label) + "\n" +
        "# Or:        ibazel run " + str(ctx.label) + "\n" +
        "set -euo pipefail\n" +
        "\n" +
        "# Resolve the runfiles root.\n" +
        "# `bazel run` sets RUNFILES_DIR; fall back to the .runfiles sibling of\n" +
        "# the script for direct invocation.\n" +
        'RUNFILES="${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}"\n' +
        "\n" +
        "# Resolve the JS runtime binary.\n" +
        "RUNTIME_REL=\"" + runtime_path_rl + "\"\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        'if [[ -z "$RUNTIME_REL" ]]; then\n' +
        "  # No toolchain runtime: use system node.\n" +
        '  RUNTIME="node"\n' +
        "else\n" +
        '  RUNTIME="${RUNFILES}/${RUNTIME_REL}"\n' +
        "fi\n" +
        "\n" +
        "# Resolve the generated vite.config.mjs.\n" +
        'CONFIG="${RUNFILES}/' + config_rl + '"\n' +
        "\n" +
        "# Resolve the Bazel-generated node_modules directory.\n" +
        "NODE_MODULES_REL=\"" + node_modules_rl_esc + "\"\n" +
        'if [[ -n "$NODE_MODULES_REL" ]]; then\n' +
        '  export NODE_MODULES_PATH="${RUNFILES}/${NODE_MODULES_REL}"\n' +
        "fi\n" +
        "\n" +
        "# Resolve the Vite CLI entry point.\n" +
        "# The vite CLI is inside the node_modules tree artifact.\n" +
        'if [[ -n "$NODE_MODULES_REL" ]]; then\n' +
        '  VITE_BIN="${RUNFILES}/${NODE_MODULES_REL}/' + vite_rel_path + '"\n' +
        "else\n" +
        "  # Fallback: try to find vite on PATH.\n" +
        '  VITE_BIN="$(command -v vite || echo "node_modules/vite/bin/vite.js")"\n' +
        "fi\n" +
        "\n"
    )

    # Add plugin path resolution when plugin is wired in.
    if plugin_rl:
        runner_content += (
            "# Resolve the vite-plugin-bazel compiled JS.\n" +
            "PLUGIN_REL=\"" + plugin_rl_esc + "\"\n" +
            'export VITE_PLUGIN_PATH="${RUNFILES}/${PLUGIN_REL}"\n' +
            "\n"
        )

    # Add user config path resolution when vite_config is wired in.
    if user_config_rl:
        runner_content += (
            "# Resolve the user-supplied Vite plugin config file.\n" +
            "USER_CONFIG_REL=\"" + user_config_rl_esc + "\"\n" +
            'export VITE_USER_CONFIG_PATH="${RUNFILES}/${USER_CONFIG_REL}"\n' +
            "\n"
        )

    # When a bundler attr is set, expose the bundler binary path as
    # BUNDLER_BINARY so that custom wrapper scripts or future extensions can
    # delegate to the specific bundler binary rather than the node_modules Vite.
    if bundler_info:
        bundler_binary_rl = _shell_escape(_rl(bundler_info.bundler_binary.short_path))
        runner_content += (
            "# Bundler binary (from bundler attr).\n" +
            'export BUNDLER_BINARY="${RUNFILES}/' + bundler_binary_rl + '"\n' +
            "\n"
        )

    runner_content += (
        "# Set the bazel-bin path so the vite.config.mjs can configure file serving.\n" +
        "# BUILD_WORKSPACE_DIRECTORY is set by `bazel run`; if not present, try to\n" +
        "# derive it from the workspace symlink.\n" +
        'if [[ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then\n' +
        '  export BAZEL_BIN_DIR="${BUILD_WORKSPACE_DIRECTORY}/bazel-bin"\n' +
        "else\n" +
        '  # Not running via `bazel run` — use the exec root heuristic.\n' +
        '  export BAZEL_BIN_DIR="$(pwd)/bazel-bin"\n' +
        "fi\n" +
        "\n" +
        "# If running via `bazel run`, cd to the workspace root.\n" +
        '# This ensures Vite finds the correct index.html and static assets.\n' +
        'if [[ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then\n' +
        '  cd "${BUILD_WORKSPACE_DIRECTORY}"\n' +
        "fi\n" +
        "\n" +
        "# Print a helpful startup message.\n" +
        'echo "[ts_dev_server] Starting Vite dev server on port ' + str(ctx.attr.port) + '..."\n' +
        'echo "[ts_dev_server] Workspace: ${BUILD_WORKSPACE_DIRECTORY:-$(pwd)}"\n' +
        'echo "[ts_dev_server] bazel-bin: ${BAZEL_BIN_DIR}"\n' +
        'if [[ -n "${NODE_MODULES_PATH:-}" ]]; then\n' +
        '  echo "[ts_dev_server] node_modules: ${NODE_MODULES_PATH}"\n' +
        "fi\n"
    )

    if plugin_rl:
        runner_content += (
            'echo "[ts_dev_server] plugin: ${VITE_PLUGIN_PATH}"\n'
        )

    if user_config_rl:
        runner_content += (
            'echo "[ts_dev_server] vite_config: ${VITE_USER_CONFIG_PATH}"\n'
        )

    runner_content += (
        'echo "[ts_dev_server] Config: ${CONFIG}"\n' +
        'echo "[ts_dev_server] Vite: ${VITE_BIN}"\n' +
        "\n" +
        "# ibazel HMR mode: keep Vite alive across rebuilds.\n" +
        "#\n" +
        "# When run via `ibazel run`, ibazel sends SIGTERM to this process after\n" +
        "# each rebuild to trigger a restart.  We intercept SIGTERM and ignore it\n" +
        "# so that the Vite dev server stays alive.  The Vite config sets\n" +
        "# server.watch.paths = [bazelBin], so Vite's built-in file watcher\n" +
        "# detects the newly compiled .js files written by ibazel and sends HMR\n" +
        "# updates to the browser — no server restart required.\n" +
        "#\n" +
        "# When run directly via `bazel run` (not ibazel), SIGTERM is never sent\n" +
        "# and the trap has no effect — the server runs normally until Ctrl-C\n" +
        "# (SIGINT), which is NOT trapped and terminates Vite as expected.\n" +
        "#\n" +
        "# Start Vite as a background job so the shell remains alive to receive\n" +
        "# the SIGTERM.  We then wait for the Vite process and forward any exit\n" +
        "# code back to the caller.\n" +
        "trap '' TERM\n" +
        '"$RUNTIME" "${RUNTIME_ARGS[@]}" "$VITE_BIN" dev --config "$CONFIG" "$@" &\n' +
        "VITE_PID=$!\n" +
        "trap 'kill -TERM \"$VITE_PID\" 2>/dev/null; wait \"$VITE_PID\" 2>/dev/null; exit 0' INT HUP\n" +
        "wait $VITE_PID\n"
    )

    ctx.actions.write(
        output = runner,
        content = runner_content,
        is_executable = True,
    )

    # ── Runfiles ───────────────────────────────────────────────────────────────
    explicit_runfiles = [config_file]
    if runtime_binary:
        explicit_runfiles.append(runtime_binary)
    explicit_runfiles.extend(node_modules_files)
    explicit_runfiles.extend(plugin_files)
    explicit_runfiles.extend(vite_config_files)
    if bundler_info:
        explicit_runfiles.append(bundler_info.bundler_binary)

    runfiles = ctx.runfiles(
        files = explicit_runfiles,
        transitive_files = depset(
            ([runtime_binary] if runtime_binary else []),
            transitive = [
                entry_js_info.transitive_js_files,
                entry_js_info.transitive_js_map_files,
                bundler_runtime_files,
            ],
        ),
    )

    # ── Providers ──────────────────────────────────────────────────────────────
    return [
        DefaultInfo(
            executable = runner,
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
    attrs = {
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
                  "will import and use the plugin for .ts-to-.js resolution and precise HMR " +
                  "invalidation. Without this attr, Vite's built-in file watcher is used. " +
                  "This attr accepts a .mjs file target (typically from a genrule).",
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
                  "its plugins to the Bazel system plugins (react, bazel-plugin). " +
                  "The file must export a default object with a `plugins` array: " +
                  "  export default { plugins: [myFrameworkPlugin()] }; " +
                  "This enables framework-specific Vite plugins (TanStack Start, Remix, " +
                  "SvelteKit, Solid Start) in the dev server while preserving Bazel " +
                  "module resolution and HMR integration. " +
                  "The plugin config file path is passed via the VITE_USER_CONFIG_PATH " +
                  "environment variable to the generated vite.config.mjs at runtime.",
            allow_single_file = [".mjs", ".js"],
        ),
    },
    doc = """Starts a Vite dev server for a TypeScript application.

`bazel run //app:dev` compiles the TypeScript sources and then starts Vite
in dev mode.  The dev server serves compiled JavaScript directly from bazel-bin
and watches for file changes to trigger HMR updates.

For live-reloading on TypeScript edits, use `ibazel run //app:dev`.  ibazel
will recompile the TypeScript sources and write new .js files to bazel-bin;
Vite's file watcher will then send HMR updates to the browser.

The node_modules attr must point to a node_modules() rule that includes `vite`
and all packages imported by the application.

The optional plugin attr wires in the vite-plugin-bazel for better .ts import
resolution and precise HMR invalidation.  Set it to `//vite:vite_plugin_bazel`
to use the compiled plugin from this repository.

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

    # Start with live HMR on TypeScript edits (requires ibazel):
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
