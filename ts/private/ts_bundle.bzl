"""Core bundle rule for rules_typescript.

ts_bundle is the canonical implementation of the bundle action. It accepts an
entry_point (a ts_compile target providing JsInfo) and an optional bundler
(a target providing BundlerInfo). When a bundler is wired in, it is invoked
via the standard CLI interface or the generated-config interface (for Vite).
When no bundler is provided, the rule falls back to a placeholder concatenation
so that downstream targets continue to build during iterative development.

Standard bundler CLI interface (BundlerInfo.use_generated_config = False):
  <bundler_binary>
    --entry  <path/to/entry.js>
    --out-dir <output/dir>
    --format esm|cjs|iife
    [--external <pkg>]...
    [--sourcemap]
    [--config <config_file>]

Generated-config interface (BundlerInfo.use_generated_config = True, used by Vite):
  <bundler_binary> <generated_vite_config_path>
  In lib mode (default), output files follow Vite lib mode convention:
    <bundle_name>.es.js     (for format=esm)
    <bundle_name>.cjs.js    (for format=cjs)
    <bundle_name>.iife.js   (for format=iife)
    <bundle_name>.es.js.map (if sourcemap=True)
  In app mode (mode="app"), the output is a declared_directory:
    <name>_bundle/  (directory containing HTML + hashed JS/CSS/assets)

When split_chunks=True (lib mode only), chunk splitting is enabled and the rule
declares the output directory instead of a single file:
    <name>_bundle/  (directory containing all chunks)

env_vars attr: a string_dict that is sugar over the define attr.  Each entry
    {key: value} is translated to define["import.meta.env.<key>"] = '"<value>"'.
    This mirrors the import.meta.env.VITE_* pattern common in Vite applications.
"""

load("//ts/private:providers.bzl", "BundlerInfo", "CssInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE")

# Vite lib mode output filename suffixes per format.
# Vite uses: esm → .es.js, cjs → .cjs.js, iife → .iife.js
_VITE_FORMAT_SUFFIX = {
    "esm": "es",
    "cjs": "cjs",
    "iife": "iife",
}

def _generate_vite_config(ctx, entry_js_file, bundle_filename, out_dir_rel, transitive_js_files, app_mode = False, user_config_file = None, has_staging_srcs = False):
    """Generates a vite.config.mjs file as a Bazel action output.

    The generated config reads the entry path and output directory from
    environment variables (VITE_ENTRY_PATH, VITE_HTML_PATH and VITE_OUT_DIR)
    set by the vite wrapper script. This allows the config to work correctly
    regardless of the Bazel sandbox's absolute path prefix.

    When user_config_file is provided, the generated config imports plugins from
    that file and prepends them to the Bazel-generated plugins array. This lets
    framework-specific Vite plugins (e.g. TanStack Start, Remix, SvelteKit) be
    injected while preserving all Bazel-critical config (outDir, resolve.alias,
    entry point rewriting).

    Args:
        ctx: The rule context.
        entry_js_file: The entry .js File from the ts_compile target (used
            for the env var name only; the actual path is set at runtime).
        bundle_filename: The bundle output filename stem (without extension).
        out_dir_rel: The exec-root-relative path to the output directory
            (embedded in the config as an env var reference comment only).
        transitive_js_files: Depset of all transitive .js files, used to build
            the resolve.alias map so Vite can find compiled Bazel outputs.
        app_mode: When True, generate an app-mode config using
            build.rollupOptions.input (index.html) rather than build.lib.
            The HTML path is read from VITE_HTML_PATH env var at runtime.
        user_config_file: Optional File. When set, the generated config imports
            this file's default export (expected to be { plugins: [...] }) and
            merges the user's plugins before the Bazel system plugins. The file
            path is embedded as an absolute path via process.env["EXEC_ROOT"].
        has_staging_srcs: When True, the generated config reads VITE_STAGING_ROOT
            from the environment and uses it as vite.root (overriding htmlDir and
            any user-supplied root). The staging dir is created and populated by
            the wrapper script before invoking Vite. Framework plugins that scan
            source files and write codegen (e.g. @remix-run/dev, tanstackStart)
            operate within the staging dir, which is writable.

    Returns:
        A string with the vite.config.mjs content. The caller declares the
        output File and writes this string (to decouple config path selection
        from content generation — split_chunks / app mode use different paths).
    """
    fmt = ctx.attr.format
    vite_format = _VITE_FORMAT_SUFFIX.get(fmt, "es")

    # Build the external array as a JavaScript array literal.
    externals_js = "[" + ", ".join([
        '"{}"'.format(e.replace('"', '\\"'))
        for e in ctx.attr.external
    ]) + "]"

    # Build the define object as a JavaScript object literal.
    # Start with explicit define entries, then layer env_vars on top as
    # import.meta.env.<KEY> = "<value>" string literals.  env_vars is sugar
    # over define — it just auto-adds the "import.meta.env." prefix.
    define_entries = []
    for k, v in ctx.attr.define.items():
        define_entries.append('  "{}": {}'.format(
            k.replace('"', '\\"'),
            v,
        ))
    for k, v in ctx.attr.env_vars.items():
        # Translate env_vars key to import.meta.env.<key> with a JSON-encoded
        # string literal value.  Vite/esbuild's define accepts a JS expression
        # string, so to make the replacement a JS string literal we must wrap
        # the value in escaped double-quotes:
        #   value "foo" → define entry value "\"foo\"" (a JS string literal)
        # Without this, esbuild would interpret "foo" as an identifier, not a
        # string literal, and reject values like "https://api.example.com".
        full_key = "import.meta.env." + k
        escaped_v = v.replace("\\", "\\\\").replace('"', '\\"')
        define_entries.append('  "{}": "\\"{}\\""'.format(
            full_key.replace('"', '\\"'),
            escaped_v,
        ))
    define_js = "{\n" + ",\n".join(define_entries) + "\n}" if define_entries else "{}"

    # sourcemap value: true or false (Vite accepts boolean or 'inline')
    sourcemap_js = "true" if ctx.attr.sourcemap else "false"

    # minify value: "esbuild" (default), "terser", or false
    # We map the bool attr to Vite's expected value string or false.
    minify_js = '"esbuild"' if ctx.attr.minify else "false"

    # The entry path and outDir are passed via env vars (set by the wrapper).
    # VITE_ENTRY_PATH: absolute path to the entry .js file
    # VITE_OUT_DIR: absolute path to the output directory

    # The output filename uses a function to force the pattern:
    #   <bundle_filename>.<vite_format>.js
    # e.g., "entry.es.js", "entry.cjs.js", "entry.iife.js"
    # This is required because Vite 6 defaults to .mjs for es format.
    filename_fn = '() => "' + bundle_filename + "." + vite_format + '.js"'

    # ── resolve.alias — map Bazel package paths to their bazel-bin outputs ──
    #
    # When ts_compile targets in a monorepo emit .js files into bazel-bin, Vite
    # needs to know that an import like "./lib" (resolved relative to the entry
    # in bazel-bin) should resolve to the correct bazel-bin .js file.
    #
    # We build a resolve.alias map by collecting all transitive .js files and
    # deriving two alias entries per file:
    #   "<file.path without .js>"     → process.env.EXEC_ROOT + "/<file.path>"
    #   "<file.short_path without .js>" → process.env.EXEC_ROOT + "/<file.path>"
    #
    # The wrapper script (vite_bundler) sets EXEC_ROOT=$(pwd) before cd'ing so
    # that the config can reconstruct absolute paths at runtime.
    alias_entries = []
    seen_alias_keys = {}
    for js_file in transitive_js_files.to_list():
        # Strip trailing .js to get the bare module path that an importer uses.
        # path = exec-root-relative, short_path = workspace-relative.
        path_no_ext = js_file.path[:-3] if js_file.path.endswith(".js") else js_file.path
        short_no_ext = js_file.short_path[:-3] if js_file.short_path.endswith(".js") else js_file.short_path

        # Use the exec-root-relative path as the alias target. The env var
        # EXEC_ROOT is set to $(pwd) by the wrapper before cd'ing.
        safe_path = js_file.path.replace("\\", "\\\\").replace("\n", "\\n").replace("\r", "\\r").replace('"', '\\"')
        abs_target = '" + process.env["EXEC_ROOT"] + "/' + safe_path + '"'

        for key in [path_no_ext, short_no_ext]:
            if key not in seen_alias_keys:
                seen_alias_keys[key] = True
                safe_key = key.replace("\\", "\\\\").replace("\n", "\\n").replace("\r", "\\r").replace('"', '\\"')
                alias_entries.append(
                    '    "{}": "'.format(safe_key) + abs_target + ",",
                )

    alias_js = "{\n" + "\n".join(alias_entries) + "\n  }" if alias_entries else "{}"

    # ── split_chunks — manualChunks splitting strategy ──────────────────────
    # When split_chunks=True, use Rollup's splitVendorChunkPlugin approach by
    # providing a manualChunks function that splits vendor (node_modules) code
    # into a separate "vendor" chunk.  Vite 5+ ships splitVendorChunkPlugin as
    # a named export.  Since we target Vite 5+, we import it directly.
    split_chunks = getattr(ctx.attr, "split_chunks", False)

    if split_chunks:
        split_vendor_import = "import { splitVendorChunkPlugin } from \"vite\";\n"
        split_vendor_plugin = "splitVendorChunkPlugin()"
    else:
        split_vendor_import = ""
        split_vendor_plugin = ""

    # ── User-config plugin injection ─────────────────────────────────────────
    # When a vite_config file is provided, the generated config dynamically
    # imports it and prepends the user's plugins before Bazel's system plugins.
    # The file path is resolved at runtime via process.env["EXEC_ROOT"] so the
    # config works regardless of the Bazel sandbox's absolute path prefix.
    #
    # User plugin files must export a default object with a `plugins` array,
    # and optionally a `root` string:
    #   export default {
    #     root: "/absolute/path/to/package",  // optional, overrides vite.root
    #     plugins: [myPlugin()],
    #   };
    #
    # When `root` is exported, it overrides the Bazel-generated vite.root in
    # app mode. This is required for framework Vite plugins (e.g. tanstackStart)
    # that resolve their own source files relative to vite.root. Without this
    # override, root defaults to the HTML staging directory (under bazel-out),
    # and the framework plugin fails to locate source files.
    if user_config_file:
        user_config_path = user_config_file.path
        user_config_import = (
            "// User-supplied Vite plugin config (from vite_config attr).\n" +
            "// Loaded dynamically so the path stays exec-root-relative.\n" +
            "let _userPlugins = [];\n" +
            "let _userRoot = null;\n" +
            "try {\n" +
            "  const _userConfigPath = process.env[\"EXEC_ROOT\"] + \"/\" + \"" +
            user_config_path.replace('"', '\\"') + "\";\n" +
            "  const _userMod = await import(_userConfigPath);\n" +
            "  const _userCfg = _userMod.default || _userMod;\n" +
            "  if (Array.isArray(_userCfg.plugins)) {\n" +
            "    _userPlugins = _userCfg.plugins;\n" +
            "  }\n" +
            "  if (typeof _userCfg.root === 'string' && _userCfg.root) {\n" +
            "    _userRoot = _userCfg.root;\n" +
            "  }\n" +
            "} catch (_err) {\n" +
            "  throw new Error('[rules_typescript] Failed to load vite_config: ' + _err.message);\n" +
            "}\n"
        )
        user_plugins_prefix = "_userPlugins, "
    else:
        user_config_import = ""
        user_plugins_prefix = ""

    if app_mode:
        # ── App mode: use rollupOptions.input (HTML file) instead of build.lib ──
        # The HTML path is passed via VITE_HTML_PATH env var set by the wrapper.
        # The entry JS path is passed via VITE_ENTRY_PATH.
        # outDir is passed via VITE_OUT_DIR (same as lib mode).
        #
        # We inject a small inline Vite plugin that rewrites every <script type="module">
        # src in the HTML to point at the absolute Bazel-compiled entry JS. This is
        # necessary because the HTML lives in the source tree but the compiled JS lives
        # in bazel-bin: Vite resolves relative src= paths relative to the HTML file,
        # which would produce a path that doesn't exist in the sandbox.

        # Build the plugins array: user plugins first (when present), then
        # bazelEntryPlugin (required for app mode HTML rewriting), then optionally
        # splitVendorChunkPlugin when split_chunks is also requested in app mode.
        if split_vendor_plugin:
            system_plugins = "bazelEntryPlugin, " + split_vendor_plugin
        else:
            system_plugins = "bazelEntryPlugin"
        plugins_list = user_plugins_prefix + system_plugins

        # When staging_srcs is present, VITE_STAGING_ROOT is set by the wrapper
        # and takes priority over everything else as the Vite root. The staging
        # dir is a writable copy of the source files inside the action sandbox,
        # so framework plugins (Remix, TanStack Start) can scan routes and write
        # codegen (route manifests, routeTree.gen.ts) inside it.
        staging_root_line = (
            "const stagingRoot = process.env[\"VITE_STAGING_ROOT\"] || null;\n"
            if has_staging_srcs else
            ""
        )

        if has_staging_srcs and user_config_file:
            # staging > userRoot > htmlDir
            vite_root_line = "const viteRoot = stagingRoot || _userRoot || htmlDir;\n"
        elif has_staging_srcs:
            # staging > htmlDir
            vite_root_line = "const viteRoot = stagingRoot || htmlDir;\n"
        elif user_config_file:
            # userRoot > htmlDir
            vite_root_line = (
                "// Use user-supplied root when available (e.g. for framework plugins\n" +
                "// that resolve source files relative to vite.root). Fall back to\n" +
                "// htmlDir so Rollup derives a clean HTML output filename.\n" +
                "const viteRoot = _userRoot || htmlDir;\n"
            )
        else:
            vite_root_line = (
                "// vite.root = HTML staging dir so Rollup derives a clean output filename.\n" +
                "const viteRoot = htmlDir;\n"
            )

        config_content = (
            "// Generated by rules_typescript ts_bundle for " + str(ctx.label) + "\n" +
            "// DO NOT EDIT — regenerated on every build.\n" +
            "// Entry and output paths are passed via environment variables by the\n" +
            "// vite_bundler wrapper script for sandbox compatibility.\n" +
            "import { defineConfig } from \"vite\";\n" +
            split_vendor_import +
            "\n" +
            "const entryPath = process.env[\"VITE_ENTRY_PATH\"];\n" +
            "const outDir = process.env[\"VITE_OUT_DIR\"];\n" +
            "const htmlPath = process.env[\"VITE_HTML_PATH\"];\n" +
            staging_root_line +
            "\n" +
            "if (!entryPath) throw new Error(\"VITE_ENTRY_PATH env var not set\");\n" +
            "if (!outDir) throw new Error(\"VITE_OUT_DIR env var not set\");\n" +
            "if (!htmlPath) throw new Error(\"VITE_HTML_PATH env var not set (required for app mode)\");\n" +
            "\n" +
            "// Set Vite root to the HTML staging directory so Rollup derives the HTML\n" +
            "// output filename as just the basename (no path separators, no '..' parts).\n" +
            "// The wrapper script stages the HTML at outDir/../_html_staging/<basename>,\n" +
            "// so htmlDir = outDir/../_html_staging is within the Bazel output tree.\n" +
            "const htmlDir = htmlPath.substring(0, htmlPath.lastIndexOf(\"/\"));\n" +
            "\n" +
            "// Inline plugin: rewrite the first <script type=\"module\"> src in the HTML\n" +
            "// to the absolute path of the Bazel-compiled entry JS. Without this,\n" +
            "// Vite would try to resolve the src relative to the HTML staging dir,\n" +
            "// where the compiled .js doesn't exist.\n" +
            "// enforce: \"pre\" + order: \"pre\" ensures this plugin runs before Vite's\n" +
            "// built-in html plugin so the src is rewritten before resolution.\n" +
            "const bazelEntryPlugin = {\n" +
            "  name: \"bazel-entry-rewrite\",\n" +
            "  enforce: \"pre\",\n" +
            "  transformIndexHtml: {\n" +
            "    order: \"pre\",\n" +
            "    handler(html) {\n" +
            "      return html.replace(\n" +
            "        /(<script[^>]+type=[\"']module[\"'][^>]*\\s)src=[\"'][^\"']*[\"']/i,\n" +
            "        (_, prefix) => `${prefix}src=\"${entryPath}\"`\n" +
            "      );\n" +
            "    },\n" +
            "  },\n" +
            "};\n" +
            "\n" +
            user_config_import +
            "\n" +
            vite_root_line +
            "\n" +
            "export default defineConfig({\n" +
            "  root: viteRoot,\n" +
            "  plugins: [" + plugins_list + "],\n" +
            "  build: {\n" +
            "    outDir: outDir,\n" +
            "    rollupOptions: {\n" +
            "      input: htmlPath,\n" +
            "    },\n" +
            "    sourcemap: " + sourcemap_js + ",\n" +
            "    minify: " + minify_js + ",\n" +
            "    emptyOutDir: false,\n" +
            "  },\n" +
            "  resolve: {\n" +
            "    alias: " + alias_js + ",\n" +
            "  },\n" +
            "  define: " + define_js + ",\n" +
            "  logLevel: \"warn\",\n" +
            "});\n"
        )
    else:
        # ── Lib mode (default): use build.lib ───────────────────────────────
        # Build plugins line: present when user_config, split_chunks, or both.
        if user_plugins_prefix or split_vendor_plugin:
            lib_plugins_content = user_plugins_prefix + split_vendor_plugin
            lib_plugins_line = "  plugins: [" + lib_plugins_content + "],\n"
        else:
            lib_plugins_line = ""
        config_content = (
            "// Generated by rules_typescript ts_bundle for " + str(ctx.label) + "\n" +
            "// DO NOT EDIT — regenerated on every build.\n" +
            "// Entry and output paths are passed via environment variables by the\n" +
            "// vite_bundler wrapper script for sandbox compatibility.\n" +
            "import { defineConfig } from \"vite\";\n" +
            split_vendor_import +
            "\n" +
            "const entryPath = process.env[\"VITE_ENTRY_PATH\"];\n" +
            "const outDir = process.env[\"VITE_OUT_DIR\"];\n" +
            "\n" +
            "if (!entryPath) throw new Error(\"VITE_ENTRY_PATH env var not set\");\n" +
            "if (!outDir) throw new Error(\"VITE_OUT_DIR env var not set\");\n" +
            "\n" +
            user_config_import +
            "\n" +
            "export default defineConfig({\n" +
            lib_plugins_line +
            "  build: {\n" +
            "    lib: {\n" +
            "      entry: entryPath,\n" +
            '      name: "' + bundle_filename + '",\n' +
            '      formats: ["' + vite_format + '"],\n' +
            "      fileName: " + filename_fn + ",\n" +
            "    },\n" +
            "    outDir: outDir,\n" +
            "    sourcemap: " + sourcemap_js + ",\n" +
            "    minify: " + minify_js + ",\n" +
            "    emptyOutDir: false,\n" +
            "    rollupOptions: {\n" +
            "      external: " + externals_js + ",\n" +
            "    },\n" +
            "  },\n" +
            "  resolve: {\n" +
            "    alias: " + alias_js + ",\n" +
            "  },\n" +
            "  define: " + define_js + ",\n" +
            "  logLevel: \"warn\",\n" +
            "});\n"
        )

    return config_content

# ─── Bundle action helper ─────────────────────────────────────────────────────
# create_bundle_action is exported so that ts_binary can reuse the bundle
# action creation without duplicating logic.
#
# Returns a struct with:
#   - bundle_out: File — the primary .js bundle output
#   - outputs: list of File — all outputs (bundle + optional sourcemap)

def create_bundle_action(ctx, entry_js_info, bundle_filename):
    """Creates the bundle action and returns the output file(s).

    Args:
        ctx: The rule context. Must have attrs: bundler, format, sourcemap,
             external, define, env_vars, mode (may be absent for non-bundle rules).
        entry_js_info: JsInfo from the entry_point target.
        bundle_filename: Filename stem (without .js extension) for the bundle.

    Returns:
        A struct with fields: bundle_out (File), outputs (list of File).
    """
    all_js = entry_js_info.transitive_js_files
    all_js_maps = entry_js_info.transitive_js_map_files

    # Collect transitive CSS files so Vite can find them in the sandbox.
    entry_point_target = ctx.attr.entry_point
    all_css = (
        entry_point_target[CssInfo].transitive_css_files
        if CssInfo in entry_point_target
        else depset()
    )

    bundler_target = ctx.attr.bundler
    if bundler_target and BundlerInfo in bundler_target:
        # ── Real bundler path ────────────────────────────────────────────────
        bundler_info = bundler_target[BundlerInfo]

        # Materialise direct js_files to find the entry file (O(1) files).
        entry_js_files = entry_js_info.js_files.to_list()
        if not entry_js_files:
            fail(
            "ts_bundle: entry_point '{ep}' provides JsInfo but has no direct .js outputs.\n".format(ep = ctx.attr.entry_point.label) +
            "Ensure the ts_compile target at entry_point has at least one .ts source file in srcs.",
        )
        if len(entry_js_files) != 1:
            fail(
            "ts_bundle: entry_point '{ep}' must produce exactly 1 .js file (the entry), " +
            "but it produces {n}.\n".format(ep = ctx.attr.entry_point.label, n = len(entry_js_files)) +
            "Use a ts_compile target with a single source file as the entry point, " +
            "e.g. srcs = [\"index.ts\"].",
        )
        entry_js_file = entry_js_files[0]

        use_generated_config = getattr(bundler_info, "use_generated_config", False)

        if use_generated_config:
            # ── Vite-style generated-config invocation ───────────────────────
            # Determine whether chunk splitting is enabled.
            split_chunks = getattr(ctx.attr, "split_chunks", False)

            # Determine whether this is an app-mode bundle.
            bundle_mode = getattr(ctx.attr, "mode", "lib")
            is_app_mode = bundle_mode == "app"

            # In app mode or split_chunks mode, the output is a directory
            # because the set of output files (hashed filenames) is not known
            # at Bazel analysis time.
            out_dir_rel = "{}_bundle".format(ctx.label.name)

            if is_app_mode or split_chunks:
                # Declare the output as a directory (file names unknown at
                # analysis time — hashed in app mode, chunk-named in split mode).
                bundle_out = ctx.actions.declare_directory(out_dir_rel)
                out_dir_path = bundle_out.path
                outputs = [bundle_out]
            else:
                # Lib mode: declare output files using Vite's lib mode naming
                # convention: <bundle_filename>.<format_suffix>.js
                fmt = ctx.attr.format
                vite_suffix = _VITE_FORMAT_SUFFIX.get(fmt, "es")
                vite_js_name = "{}.{}.js".format(bundle_filename, vite_suffix)

                bundle_out = ctx.actions.declare_file(
                    "{}/{}".format(out_dir_rel, vite_js_name),
                )
                out_dir_path = bundle_out.dirname

                outputs = [bundle_out]
                if ctx.attr.sourcemap:
                    map_out = ctx.actions.declare_file(
                        "{}/{}.map".format(out_dir_rel, vite_js_name),
                    )
                    outputs.append(map_out)

            # The config file must be a sibling to avoid "output is a prefix"
            # Bazel errors when the bundle output is a declared_directory.
            if is_app_mode or split_chunks:
                config_out_path = "{}_bundle_config/vite.config.mjs".format(ctx.label.name)
            else:
                config_out_path = "{}_bundle/vite.config.mjs".format(ctx.label.name)

            # Collect the user-supplied vite_config file (if any).
            # This is an optional label attr pointing to a .mjs/.js file that
            # exports { plugins: [...] }. Plugins from this file are prepended
            # to the Bazel-generated plugins array in the generated config.
            user_vite_config_file = None
            user_vite_config_inputs = []
            vite_config_attr = getattr(ctx.attr, "vite_config", None)
            if vite_config_attr:
                vite_config_files = vite_config_attr.files.to_list()
                if vite_config_files:
                    user_vite_config_file = vite_config_files[0]
                    user_vite_config_inputs = [user_vite_config_file]

            # ── staging_srcs — writable source staging for framework plugins ──
            # When staging_srcs is set, we generate a manifest file listing
            # every staging source as "<pkg_rel_path>\t<exec_root_rel_path>".
            # The wrapper script reads this manifest, creates a writable
            # _staging/ directory inside the action sandbox, copies each file
            # there preserving structure, and exports VITE_STAGING_ROOT so the
            # generated vite.config.mjs can use it as vite.root.
            # Framework plugins (Remix, TanStack Start) can then scan source
            # files and write codegen into the staging dir without hitting
            # sandbox write-protection on the original source tree.
            staging_srcs_attr = getattr(ctx.attr, "staging_srcs", [])
            staging_srcs_files = []
            for t in staging_srcs_attr:
                staging_srcs_files.extend(t.files.to_list())

            staging_manifest_file = None
            staging_manifest_inputs = []
            if staging_srcs_files:
                # Build manifest: one line per file, tab-separated:
                #   <package_relative_dest_path>\t<exec_root_relative_src_path>
                # The destination path is derived from the file's short_path
                # (workspace-relative) by stripping the package prefix.
                pkg_prefix = ctx.label.package + "/"
                manifest_lines = []
                for f in staging_srcs_files:
                    # Derive destination relative to the package root.
                    # short_path is workspace-relative, e.g. "src/routes/index.tsx"
                    short = f.short_path
                    if short.startswith(pkg_prefix):
                        dest = short[len(pkg_prefix):]
                    else:
                        # External or generated file: use the short_path directly.
                        dest = short
                    manifest_lines.append("{}\t{}".format(dest, f.path))
                staging_manifest_content = "\n".join(manifest_lines) + "\n"
                staging_manifest_file = ctx.actions.declare_file(
                    "{}_bundle_config/staging_manifest.txt".format(ctx.label.name),
                )
                ctx.actions.write(
                    output = staging_manifest_file,
                    content = staging_manifest_content,
                )
                staging_manifest_inputs = [staging_manifest_file]

            has_staging = bool(staging_srcs_files)

            config_file = ctx.actions.declare_file(config_out_path)
            config_content = _generate_vite_config(
                ctx,
                entry_js_file,
                bundle_filename,
                out_dir_rel,
                all_js,
                app_mode = is_app_mode,
                user_config_file = user_vite_config_file,
                has_staging_srcs = has_staging,
            )
            ctx.actions.write(output = config_file, content = config_content)

            # In app mode, also collect the HTML file as an input.
            html_file = None
            html_input_files = []
            if is_app_mode:
                html_attr = getattr(ctx.attr, "html", None)
                if html_attr:
                    html_files = html_attr.files.to_list()
                    if html_files:
                        html_file = html_files[0]
                        html_input_files = [html_file]
                else:
                    fail(
                        "ts_bundle: 'html' attr is required when mode = \"app\".\n" +
                        "Provide the path to your index.html file:\n" +
                        "  ts_bundle(\n" +
                        "      name = \"app\",\n" +
                        "      mode = \"app\",\n" +
                        "      html = \"index.html\",\n" +
                        "      entry_point = \":entry\",\n" +
                        "      bundler = \":vite\",\n" +
                        "  )",
                    )

            inputs = depset(
                [entry_js_file, config_file] +
                html_input_files +
                user_vite_config_inputs +
                staging_manifest_inputs +
                staging_srcs_files,
                transitive = [
                    all_js,
                    all_js_maps,
                    all_css,
                    bundler_info.runtime_deps,
                ],
            )

            # Arguments to the wrapper script:
            #   $1 = vite.config.mjs path (exec-root-relative)
            #   $2 = entry .js path (exec-root-relative) — used for VITE_ENTRY_PATH
            #   $3 = output directory path (exec-root-relative)
            #   $4 = HTML file path (exec-root-relative, app mode only; "" when absent)
            #   $5 = staging manifest path (exec-root-relative, optional; "" when absent)
            # The wrapper converts these to absolute paths via EXEC_ROOT=$(pwd).
            action_args = [
                config_file.path,
                entry_js_file.path,
                out_dir_path,
            ]
            if is_app_mode and html_file:
                action_args.append(html_file.path)
            else:
                action_args.append("")
            if staging_manifest_file:
                action_args.append(staging_manifest_file.path)

            ctx.actions.run(
                inputs = inputs,
                outputs = outputs,
                executable = bundler_info.bundler_binary,
                arguments = action_args,
                mnemonic = "TsBundleVite",
                progress_message = "TsBundleVite %{label}",
            )
        else:
            # ── Standard CLI invocation ──────────────────────────────────────
            bundle_out = ctx.actions.declare_file(
                "{}_bundle/{}.js".format(ctx.label.name, bundle_filename),
            )
            out_dir = bundle_out.dirname

            args = ctx.actions.args()
            args.add("--entry", entry_js_file)
            args.add("--out-dir", out_dir)
            args.add("--format", ctx.attr.format)
            for ext in ctx.attr.external:
                args.add("--external", ext)
            if ctx.attr.sourcemap:
                args.add("--sourcemap")
            for k, v in ctx.attr.define.items():
                args.add("--define", "{}={}".format(k, v))
            if bundler_info.config_file:
                args.add("--config", bundler_info.config_file)

            inputs = depset(
                [entry_js_file],
                transitive = [
                    all_js,
                    all_js_maps,
                    all_css,
                    bundler_info.runtime_deps,
                ] + ([depset([bundler_info.config_file])] if bundler_info.config_file else []),
            )

            outputs = [bundle_out]
            if ctx.attr.sourcemap:
                map_out = ctx.actions.declare_file(
                    "{}_bundle/{}.js.map".format(ctx.label.name, bundle_filename),
                )
                outputs.append(map_out)

            ctx.actions.run(
                inputs = inputs,
                outputs = outputs,
                executable = bundler_info.bundler_binary,
                arguments = [args],
                mnemonic = "TsBundle",
                progress_message = "TsBundle %{label}",
            )
    else:
        # ── Placeholder fallback (no bundler configured) ─────────────────────
        # Concatenate all .js files in depset order. This preserves the
        # build graph while a real bundler is wired in. ctx.actions.run_shell
        # is used here because we need shell pipeline primitives (while/read).
        bundle_out = ctx.actions.declare_file(
            "{}_bundle/{}.js".format(ctx.label.name, bundle_filename),
        )
        manifest_args = ctx.actions.args()
        manifest_args.set_param_file_format("multiline")
        manifest_args.add_all(all_js)

        manifest = ctx.actions.declare_file("{}_js_manifest.txt".format(ctx.label.name))
        ctx.actions.write(output = manifest, content = manifest_args)

        bundle_cmd = """\
set -euo pipefail
mkdir -p "$(dirname "{out}")"
echo "// Placeholder bundle — replace with a real bundler." > "{out}"
echo "// Entry point: {entry_label}" >> "{out}"
while IFS= read -r f; do
  echo "" >> "{out}"
  echo "// ---- $f ----" >> "{out}"
  cat "$f" >> "{out}"
done < "{manifest}"
""".format(
            out = bundle_out.path,
            manifest = manifest.path,
            entry_label = str(ctx.attr.entry_point.label).replace('"', '\\"').replace("$", "\\$").replace("`", "\\`"),
        )

        ctx.actions.run_shell(
            inputs = depset([manifest], transitive = [all_js, all_js_maps]),
            outputs = [bundle_out],
            command = bundle_cmd,
            mnemonic = "TsBundle",
            progress_message = "TsBundle %{label}",
        )
        outputs = [bundle_out]

    return struct(bundle_out = bundle_out, outputs = outputs)

# ─── Shared rule implementation ────────────────────────────────────────────────
# ts_bundle_impl is used by the ts_bundle rule.

def ts_bundle_impl(ctx):
    entry_point = ctx.attr.entry_point
    if JsInfo not in entry_point:
        fail(
        "ts_bundle: entry_point '{ep}' does not provide JsInfo.\n".format(ep = ctx.attr.entry_point.label) +
        "The entry_point attr must be a ts_compile target (or any target that provides JsInfo).\n" +
        "Did you mean: entry_point = \"//path/to:your_ts_compile_target\"?",
    )

    entry_js_info = entry_point[JsInfo]

    bundle_filename = ctx.attr.bundle_name if ctx.attr.bundle_name else ctx.label.name
    result = create_bundle_action(ctx, entry_js_info, bundle_filename)
    bundle_out = result.bundle_out
    outputs = result.outputs

    providers = [
        DefaultInfo(files = depset(outputs)),
        JsInfo(
            js_files = depset([bundle_out]),
            js_map_files = depset([]),
            transitive_js_files = depset([bundle_out]),
            transitive_js_map_files = depset([]),
        ),
        OutputGroupInfo(
            bundle = depset([bundle_out]),
            js_tree = entry_js_info.transitive_js_files,
        ),
    ]

    # Forward CssInfo from the entry point so downstream consumers
    # (e.g. another ts_bundle) can collect CSS files.
    if CssInfo in entry_point:
        providers.append(entry_point[CssInfo])

    return providers

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_bundle = rule(
    implementation = ts_bundle_impl,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    attrs = {
        "entry_point": attr.label(
            doc = "The ts_compile target whose output is the bundle entry point.",
            providers = [JsInfo],
            mandatory = True,
        ),
        "bundler": attr.label(
            doc = "Optional target providing BundlerInfo. When absent, falls back to placeholder concatenation.",
            providers = [BundlerInfo],
            default = None,
        ),
        "bundle_name": attr.string(
            doc = "Name for the output bundle file (without extension). Defaults to the rule name.",
            default = "",
        ),
        "format": attr.string(
            doc = "Output module format: 'esm', 'cjs', 'iife'. Passed to the bundler.",
            default = "esm",
            values = ["esm", "cjs", "iife"],
        ),
        "sourcemap": attr.bool(
            doc = "Whether to emit a source map alongside the bundle.",
            default = True,
        ),
        "minify": attr.bool(
            doc = "Whether to minify the bundle output. When True, uses esbuild minification (Vite default). Default True.",
            default = True,
        ),
        "split_chunks": attr.bool(
            doc = "When True, enable chunk splitting via splitVendorChunkPlugin. The output is a directory instead of a single file. Only supported in generated-config mode (Vite bundler). Default False.",
            default = False,
        ),
        "external": attr.string_list(
            doc = "Module specifiers to mark as external (not bundled).",
        ),
        "define": attr.string_dict(
            doc = "Global constant replacements, e.g. {'process.env.NODE_ENV': '\"production\"'}.",
        ),
        "env_vars": attr.string_dict(
            doc = """Vite-style environment variable substitutions (sugar over the define attr).

Each entry {KEY: VALUE} is translated to a define entry:
    "import.meta.env.KEY" -> '"VALUE"'

This mirrors the import.meta.env.VITE_* pattern common in Vite applications.
At bundle time the bundler replaces every reference to import.meta.env.KEY
in the source with the literal string "VALUE".

Example:
    ts_bundle(
        name = "app",
        entry_point = ":entry",
        bundler = ":vite",
        env_vars = {
            "VITE_API_URL": "https://api.example.com",
            "VITE_VERSION": "1.0.0",
        },
    )
""",
        ),
        "mode": attr.string(
            doc = """Bundling mode: "lib" (default) or "app".

In lib mode (default), Vite uses build.lib to produce a single JS file
(or directory when split_chunks=True) suitable for use as a library.

In app mode, Vite uses build.rollupOptions.input to bundle an HTML
application entry point. The output is a directory containing the
processed HTML file with hashed JS/CSS/asset references — ready to
deploy as a static site. Requires the 'html' attr to be set.
""",
            default = "lib",
            values = ["lib", "app"],
        ),
        "html": attr.label(
            doc = """HTML entry point for app mode (mode = "app").

In app mode, Vite reads this HTML file and bundles all the JS/CSS/assets
referenced from it. The output directory contains the processed HTML file
with hashed JS/CSS/asset references, plus all the bundled assets.

Must be set when mode = "app". Ignored in lib mode.

Example:
    ts_bundle(
        name = "app",
        mode = "app",
        html = "index.html",
        entry_point = ":entry",
        bundler = ":vite",
    )
""",
            allow_single_file = [".html"],
            default = None,
        ),
        "vite_config": attr.label(
            doc = """Optional user-supplied Vite plugin configuration file (.mjs or .js).

When set, the generated vite.config.mjs imports this file and prepends its
plugins to the Bazel-generated plugins array. This enables framework-specific
Vite plugins (e.g. TanStack Start, Remix, SvelteKit, Solid Start) to be
injected while preserving all Bazel-critical configuration (outDir, entry point
rewriting, resolve.alias for bazel-bin outputs).

The file must export a default object with a `plugins` array:

    // my-framework-vite-config.mjs
    import { tanstackStart } from "@tanstack/start/vite";
    export default {
        plugins: [tanstackStart()],
    };

In BUILD:
    ts_bundle(
        name = "app",
        mode = "app",
        html = "index.html",
        entry_point = "//src/app",
        bundler = ":vite",
        vite_config = "my-framework-vite-config.mjs",
    )

User plugins are inserted before Bazel's system plugins (e.g. bazelEntryPlugin,
splitVendorChunkPlugin) so that framework transforms run first during bundling.

Only supported when using a Vite bundler (use_generated_config = True).
Ignored for bundlers that use the standard CLI interface.
""",
            allow_single_file = [".mjs", ".js"],
            default = None,
        ),
        "staging_srcs": attr.label_list(
            doc = """Source files to stage into a writable directory for framework Vite plugins.

When set, the wrapper script copies these files into a writable _staging/
directory inside the action sandbox before invoking Vite, preserving their
package-relative directory structure. The staging directory path is exported
as VITE_STAGING_ROOT and the generated vite.config.mjs uses it as vite.root,
overriding htmlDir and any user-supplied root from vite_config.

This enables framework Vite plugins (e.g. @remix-run/dev, tanstackStart) to:
  - Scan route files at their expected relative paths (they are copied there)
  - Write codegen outputs (routeTree.gen.ts, route manifests) inside the
    staging dir without hitting Bazel sandbox write-protection on the real
    source tree

The actual bundle still uses pre-compiled .js outputs from the entry_point's
JsInfo — resolve.alias redirects framework imports to the compiled files.

When staging_srcs is empty (default), behaviour is identical to regular
ts_bundle — no staging dir is created, no root override is applied.

Only effective when using a Vite bundler (use_generated_config = True).
Ignored for the placeholder fallback and standard CLI bundlers.

Example (Remix):
    ts_bundle(
        name = "app",
        mode = "app",
        html = "index.html",
        entry_point = "//src/app",
        bundler = ":vite",
        vite_config = "remix-vite.config.mjs",
        staging_srcs = glob(["app/routes/**/*.tsx"]),
    )
""",
            allow_files = True,
            default = [],
        ),
    },
    doc = """Produces a bundled JavaScript output from a ts_compile entry point.

Collects all transitive .js outputs from the entry_point's dependency graph.
When a bundler target (providing BundlerInfo) is specified via the bundler attr,
it is invoked via the standard CLI interface (or generated-config mode for Vite).
Without a bundler, the rule produces a placeholder concatenation so the build
graph remains valid.

Example (no bundler — placeholder mode):
    ts_bundle(
        name = "app",
        entry_point = "//src/app:app",
        format = "esm",
    )

Example (with a Vite bundler — lib mode, the default):
    load("@rules_typescript//npm:defs.bzl", "node_modules")
    load("@rules_typescript//vite:bundler.bzl", "vite_bundler")

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
        entry_point = "//src/app:app",
        bundler = ":vite",
        format = "esm",
    )

Example (app mode — produces a deployable HTML + JS/CSS/assets directory):
    ts_bundle(
        name = "app",
        mode = "app",
        html = "index.html",
        entry_point = "//src/app:app",
        bundler = ":vite",
    )

Example (env_vars — inject import.meta.env.* values at bundle time):
    ts_bundle(
        name = "app",
        entry_point = "//src/app:app",
        bundler = ":vite",
        env_vars = {
            "VITE_API_URL": "https://api.example.com",
            "VITE_VERSION": "1.0.0",
        },
    )

Example (vite_config — inject framework-specific Vite plugins):
    # my-framework-vite-config.mjs
    # import { tanstackStart } from "@tanstack/start/vite";
    # export default { plugins: [tanstackStart()] };

    ts_bundle(
        name = "app",
        mode = "app",
        html = "index.html",
        entry_point = "//src/app:app",
        bundler = ":vite",
        vite_config = "my-framework-vite-config.mjs",
    )
""",
)
