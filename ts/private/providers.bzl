"""Provider definitions for rules_typescript."""

JsInfo = provider(
    doc = "Provider for JavaScript compilation outputs.",
    fields = {
        "js_files": "depset of File: Direct .js output files from this target.",
        "js_map_files": "depset of File: Direct .js.map source map files from this target.",
        "transitive_js_files": "depset of File: Transitive closure of all .js files from this target and its deps.",
        "transitive_js_map_files": "depset of File: Transitive closure of all .js.map files.",
    },
)

TsDeclarationInfo = provider(
    doc = "Provider for TypeScript declaration outputs (.d.ts files).",
    fields = {
        "declaration_files": "depset of File: Direct .d.ts output files from this target.",
        "transitive_declaration_files": "depset of File: Transitive closure of all .d.ts files from this target and its deps.",
        "type_roots": "depset of File: Root directories containing type declarations (for @types packages).",
    },
)

TsConfigInfo = provider(
    doc = "Provider for generated tsconfig.json files.",
    fields = {
        "tsconfig": "File: The generated tsconfig.json file.",
        "deps_tsconfigs": "depset of File: Transitive tsconfig.json files from dependencies.",
    },
)

NpmPackageInfo = provider(
    doc = "Provider for npm package targets.",
    fields = {
        "package_name": "string: npm package name (e.g., 'react').",
        "package_version": "string: npm package version.",
        "package_dir": "File: The package.json file at the root of the extracted package.",
        "all_files": "depset of File: All files in this package (package.json + .js + .d.ts + other assets). Used by node_modules rule for runtime.",
        "js_files": "depset of File: JavaScript files in this package.",
        "declaration_files": "depset of File: TypeScript declaration files (.d.ts) in this package.",
        "transitive_deps": "depset of NpmPackageInfo: Transitive npm dependencies.",
        "transitive_package_dirs": "depset of File: package.json files for this package and all transitive deps.",
        "exports_types_file": "File or None: The specific .d.ts entry point from package.json exports['.']['types'], or None if not specified. Used by ts_compile to build more precise tsconfig paths entries.",
    },
)

CssInfo = provider(
    doc = "Provider for CSS file outputs.",
    fields = {
        "css_files": "depset of File: Direct .css files from this target.",
        "transitive_css_files": "depset of File: Transitive closure of all .css files.",
    },
)

CssModuleInfo = provider(
    doc = "Provider for CSS Module outputs (.module.css files with typed class names).",
    fields = {
        "css_files": "depset of File: Direct .module.css files from this target.",
        "transitive_css_files": "depset of File: Transitive closure of all .module.css files.",
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
        "asset_files": "depset of File: Direct asset files from this target.",
        "transitive_asset_files": "depset of File: Transitive closure of all asset files.",
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
