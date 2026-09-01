"""Public API for rules_typescript.

Users should load rules from this file:
    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test", "ts_binary")
    load("@rules_typescript//ts:defs.bzl", "ts_bundle", "BundlerInfo")
    load("@rules_typescript//ts:defs.bzl", "ts_lint", "TsLintInfo")
    load("@rules_typescript//ts:defs.bzl", "css_library", "css_module", "asset_library")
    load("@rules_typescript//ts:defs.bzl", "json_library")
    load("@rules_typescript//ts:defs.bzl", "ts_pnpm", "ts_add_package", "ts_refresh_tsconfig")
    load("@rules_typescript//ts:defs.bzl", "ts_codegen", "refresh_workspace_files")
    load("@rules_typescript//ts:defs.bzl", "next_build", "next_dev_server", "next_serve")
    load("@rules_typescript//ts:defs.bzl", "remix_build")
    load("@rules_typescript//ts:defs.bzl", "svelte_library", "sveltekit_build")
"""

load("//ts/private:asset_library.bzl", _asset_library = "asset_library")
load("//ts/private:css_library.bzl", _css_library = "css_library")
load("//ts/private:css_module.bzl", _css_module = "css_module")
load("//ts/private:json_library.bzl", _json_library = "json_library")
load("//ts/private:next_build.bzl", _next_build = "next_build")
load("//ts/private:next_server.bzl", _next_dev_server = "next_dev_server", _next_serve = "next_serve")
load("//ts/private:pnpm.bzl", _ts_add_package = "ts_add_package", _ts_pnpm = "ts_pnpm")
load("//ts/private:providers.bzl", _AssetInfo = "AssetInfo", _BundlerInfo = "BundlerInfo", _CssInfo = "CssInfo", _CssModuleInfo = "CssModuleInfo", _JsInfo = "JsInfo", _TsDeclarationInfo = "TsDeclarationInfo")
load("//ts/private:remix_build.bzl", _remix_build = "remix_build")
load("//ts/private:svelte_library.bzl", _svelte_library = "svelte_library")
load("//ts/private:sveltekit_build.bzl", _sveltekit_build = "sveltekit_build")
load("//ts/private:ts_binary.bzl", _ts_binary = "ts_binary")
load("//ts/private:ts_bundle.bzl", _ts_bundle = "ts_bundle")
load("//ts/private:ts_codegen.bzl", _ts_codegen = "ts_codegen")
load("//ts/private:ts_compile.bzl", _TsModuleInfo = "TsModuleInfo", _fail_on_mixed_src_packages = "fail_on_mixed_src_packages", _ts_compile_rule = "ts_compile")
load("//ts/private:ts_config.bzl", _ts_config = "ts_config")
load("//ts/private:ts_dev_server.bzl", _ts_dev_server = "ts_dev_server")
load("//ts/private:ts_lint.bzl", _TsLintInfo = "TsLintInfo", _ts_lint = "ts_lint")
load("//ts/private:ts_npm_publish.bzl", _NpmPublishInfo = "NpmPublishInfo", _ts_npm_publish = "ts_npm_publish")
load("//ts/private:ts_test.bzl", _ts_test = "ts_test")
load("//ts/private:tsconfig_aspect.bzl", _refresh_workspace_files = "refresh_workspace_files", _ts_refresh_tsconfig = "ts_refresh_tsconfig")
load("//ts/private:wrangler.bzl", _ts_worker_deploy = "ts_worker_deploy", _ts_worker_dry_run = "ts_worker_dry_run", _ts_worker_dry_run_test = "ts_worker_dry_run_test")

# Providers — exported for use in custom rules that extend this ruleset.
AssetInfo = _AssetInfo
BundlerInfo = _BundlerInfo
CssInfo = _CssInfo
CssModuleInfo = _CssModuleInfo
JsInfo = _JsInfo
NpmPublishInfo = _NpmPublishInfo
TsDeclarationInfo = _TsDeclarationInfo
TsModuleInfo = _TsModuleInfo
TsLintInfo = _TsLintInfo
ts_worker_deploy = _ts_worker_deploy
ts_worker_dry_run = _ts_worker_dry_run
ts_worker_dry_run_test = _ts_worker_dry_run_test

# CSS / asset / JSON support.
asset_library = _asset_library
css_library = _css_library
css_module = _css_module
json_library = _json_library

# Standalone rules for advanced use cases.
ts_codegen = _ts_codegen
ts_config = _ts_config
ts_test = _ts_test

# Bundle rules. Separate rules with overlapping attributes, not aliases:
# ts_binary is runnable and takes a strictly smaller attr set, plus entry_file,
# data and node_modules, which ts_bundle has no use for. See
# docs/rules/ts-binary.md.
ts_binary = _ts_binary
ts_bundle = _ts_bundle

# Dev server rule.
ts_dev_server = _ts_dev_server

# Next.js build rule, and the two ways to run the app.
next_build = _next_build
next_dev_server = _next_dev_server
next_serve = _next_serve

# Remix build rule.
remix_build = _remix_build

# Svelte component compilation.
svelte_library = _svelte_library

# SvelteKit build rule.
sveltekit_build = _sveltekit_build

# Lint rule.
ts_lint = _ts_lint

# npm publish rule.
ts_npm_publish = _ts_npm_publish

# Hermetic pnpm workspace macros.
ts_pnpm = _ts_pnpm
ts_add_package = _ts_add_package
ts_refresh_tsconfig = _ts_refresh_tsconfig

# Copies build outputs into the source tree under `bazel run`. Pair it with
# diff_test for a generated file that has to be checked in.
refresh_workspace_files = _refresh_workspace_files

def ts_compile(
        name,
        srcs,
        deps = None,
        target = "es2022",
        jsx_mode = "react-jsx",
        jsx_import_source = None,
        lib = None,
        types = None,
        compiler_options = None,
        tsconfig = None,
        declarations = "tsgo",
        enable_check = True,
        source_map = True,
        declaration_map = False,
        tsgo_args = None,
        path_aliases = None,
        path_alias_srcs = None,
        vite_types = False,
        **kwargs):
    """Compiles TypeScript with oxc-bazel and emits declarations with tsgo.

    oxc always does the JavaScript transform. The `declarations` attribute
    decides who emits the .d.ts, and that choice is the one real trade-off in
    this rule:

    | declarations | annotations needed | type errors        | checking        |
    |--------------|--------------------|--------------------|-----------------|
    | "tsgo"       | none               | fail the build     | critical path   |
    | "oxc"        | on every export    | fail _validation   | concurrent      |

    Default "tsgo" works on unmodified TypeScript. Move a package to "oxc" once
    every export carries an explicit type, to take type-checking off the
    critical path.

    ### Where compiler options come from

    Lowest precedence first:

    1. The baseline (strict, module Preserve, skipLibCheck, esModuleInterop,
       plus moduleResolution Bundler without a `tsconfig`) -- with a `tsconfig`
       it is a file that config extends FIRST, so it reaches only the keys the
       file never mentions. moduleResolution is left to tsgo to derive from
       whichever `module` wins, because TypeScript rejects a pair it did not
       derive itself.
    2. `tsconfig` -- the project's own tsconfig.json, and whatever it extends,
       so tsgo checks the code under the same options `tsc` does.
    3. `target` and `jsx_mode`, then `jsx_import_source`, `lib`, `types`, then
       `compiler_options`, which wins among these.
    4. The options Bazel owns: paths, include, outDir / rootDir / rootDirs,
       baseUrl and the emit shape. Setting one of those in `compiler_options`
       is an error that names the attribute to use instead.

    `target` and `jsx_mode` are always injected, because oxc transforms with
    them and the two compilers have to agree; a `target` or `jsx` in the
    tsconfig file is superseded.

    Args:
        name:                  Target name.
        srcs:                  TypeScript source files (.ts, .tsx).
        deps:                  Dependency targets providing TsDeclarationInfo + JsInfo.
        target:                ECMAScript target version (default "es2022").
        jsx_mode:              JSX transform mode (default "react-jsx").
        jsx_import_source:     compilerOptions.jsxImportSource, e.g. "solid-js"
                               or "preact".
        lib:                   compilerOptions.lib, e.g. ["es2022", "webworker"].
                               Replaces the whole set `target` implies, which is
                               how DOM gets dropped from a worker build.
        types:                 compilerOptions.types -- which ambient type
                               packages load. [] loads none, the only way to stop
                               unrelated npm packages in the dep graph from
                               reaching the global scope. Relative entries
                               resolve against this target's package.
        compiler_options:      Any other compilerOptions, as a dict, e.g.
                               {"allowImportingTsExtensions": True}. Passed
                               through verbatim; relative paths in them resolve
                               against the generated tsconfig, so path-valued
                               options belong in `tsconfig` instead (`types` and
                               `typeRoots` being the two exceptions).
        tsconfig:              Label of the project's tsconfig.json, or of a
                               ts_config target when that file extends others.
        declarations:          "tsgo" (default) or "oxc" -- see the table above.
        enable_check:          Whether to run tsgo type-checking. Only meaningful
                               with declarations = "oxc"; under "tsgo" the
                               compiler emits and checks in one pass.
        source_map:            Emit a .js.map next to every .js (default True).
        declaration_map:       Emit a .d.ts.map next to every declaration, so
                               go-to-definition across a package boundary lands
                               on the .ts source. Needs the tsgo emit.
        tsgo_args:             Extra tsgo flags, restricted to the ones that only
                               report on the program (e.g. ["--traceResolution"]).
        path_aliases:          Optional dict mapping path alias prefixes to workspace-relative
                               source directory paths (e.g. {"@/": "src/"}). Injected into the
                               tsgo tsconfig so aliases like `import "@/components"` resolve
                               during type-checking. For a bare specifier that has to resolve
                               to another target's generated declarations, set module_name on
                               that target instead.
        path_alias_srcs:       Labels whose files a path_aliases entry resolves to,
                               when they are not in srcs. They become inputs to the
                               type-check action.
        vite_types:            When True, automatically prepends the Vite client-side ambient
                               type shim (@rules_typescript//ts:vite_env.d.ts) to srcs. This
                               provides types for import.meta.env, import.meta.hot, and asset
                               URL imports (*.svg, *.png, etc.) without requiring vite as a
                               compile-time dependency. Default False.
        **kwargs:              Additional args forwarded to the rule (e.g. module_name,
                               private_globals, visibility, tags).
    """
    if deps == None:
        deps = []

    _fail_on_mixed_src_packages("ts_compile", name, srcs, declarations, enable_check)

    if path_aliases != None:
        kwargs["path_aliases"] = path_aliases
    if path_alias_srcs != None:
        kwargs["path_alias_srcs"] = path_alias_srcs
    if tsgo_args != None:
        kwargs["tsgo_args"] = tsgo_args

    # target and jsx_mode stay rule attrs (oxc needs them too), so the rule
    # injects them; everything else reaches the tsconfig through this dict.
    compiler_opts = {}
    if jsx_import_source != None:
        compiler_opts["jsxImportSource"] = jsx_import_source
    if lib != None:
        compiler_opts["lib"] = lib
    if types != None:
        compiler_opts["types"] = types
    for key, value in (compiler_options or {}).items():
        compiler_opts[key] = value

    effective_srcs = srcs
    if vite_types:
        effective_srcs = ["@rules_typescript//ts:vite_env.d.ts"] + list(srcs)

    _ts_compile_rule(
        name = name,
        srcs = effective_srcs,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        tsconfig = tsconfig,
        compiler_options_json = json.encode(compiler_opts),
        declarations = declarations,
        enable_check = enable_check,
        source_map = source_map,
        declaration_map = declaration_map,
        **kwargs
    )
