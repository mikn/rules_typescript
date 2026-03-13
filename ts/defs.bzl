"""Public API for rules_typescript.

Users should load rules from this file:
    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test", "ts_binary")
    load("@rules_typescript//ts:defs.bzl", "ts_bundle", "BundlerInfo")
    load("@rules_typescript//ts:defs.bzl", "ts_lint", "TsLintInfo")
    load("@rules_typescript//ts:defs.bzl", "css_library", "css_module", "asset_library")
    load("@rules_typescript//ts:defs.bzl", "json_library")
    load("@rules_typescript//ts:defs.bzl", "ts_pnpm", "ts_add_package")
"""
load("//ts/private:asset_library.bzl", _asset_library = "asset_library")
load("//ts/private:pnpm.bzl", _ts_add_package = "ts_add_package", _ts_pnpm = "ts_pnpm")
load("//ts/private:css_library.bzl", _css_library = "css_library")
load("//ts/private:css_module.bzl", _css_module = "css_module")
load("//ts/private:json_library.bzl", _json_library = "json_library")
load("//ts/private:providers.bzl", _AssetInfo = "AssetInfo", _BundlerInfo = "BundlerInfo", _CssInfo = "CssInfo", _CssModuleInfo = "CssModuleInfo", _JsInfo = "JsInfo", _TsDeclarationInfo = "TsDeclarationInfo")
load("//ts/private:ts_binary.bzl", _ts_binary = "ts_binary")
load("//ts/private:ts_bundle.bzl", _ts_bundle = "ts_bundle")
load("//ts/private:ts_check.bzl", _ts_check = "ts_check")
load("//ts/private:ts_codegen.bzl", _ts_codegen = "ts_codegen")
load("//ts/private:ts_compile.bzl", _ts_compile_rule = "ts_compile")
load("//ts/private:ts_config_gen.bzl", _ts_config_gen = "ts_config_gen")
load("//ts/private:ts_dev_server.bzl", _ts_dev_server = "ts_dev_server")
load("//ts/private:ts_lint.bzl", _TsLintInfo = "TsLintInfo", _ts_lint = "ts_lint")
load("//ts/private:ts_npm_publish.bzl", _NpmPublishInfo = "NpmPublishInfo", _ts_npm_publish = "ts_npm_publish")
load("//ts/private:ts_test.bzl", _ts_test = "ts_test")

# Providers — exported for use in custom rules that extend this ruleset.
AssetInfo = _AssetInfo
BundlerInfo = _BundlerInfo
CssInfo = _CssInfo
CssModuleInfo = _CssModuleInfo
JsInfo = _JsInfo
NpmPublishInfo = _NpmPublishInfo
TsDeclarationInfo = _TsDeclarationInfo
TsLintInfo = _TsLintInfo

# CSS / asset / JSON support.
asset_library = _asset_library
css_library = _css_library
css_module = _css_module
json_library = _json_library

# Standalone rules for advanced use cases.
ts_check = _ts_check
ts_codegen = _ts_codegen
ts_config_gen = _ts_config_gen
ts_test = _ts_test

# Bundle rules.
# ts_binary is the stable public name (backwards-compatible).
# ts_bundle is the canonical internal name and the preferred future API.
# Both accept identical attrs; ts_binary delegates to ts_bundle's implementation.
ts_binary = _ts_binary
ts_bundle = _ts_bundle

# Dev server rule.
ts_dev_server = _ts_dev_server

# Lint rule.
ts_lint = _ts_lint

# npm publish rule.
ts_npm_publish = _ts_npm_publish

# Hermetic pnpm workspace macros.
ts_pnpm = _ts_pnpm
ts_add_package = _ts_add_package

def ts_compile(
        name,
        srcs,
        deps = None,
        target = "es2022",
        jsx_mode = "react-jsx",
        isolated_declarations = True,
        enable_check = True,
        path_aliases = None,
        vite_types = False,
        **kwargs):
    """Compiles TypeScript source files with oxc-bazel and optionally type-checks with tsgo.

    Type-checking runs as a Bazel validation action when a tsgo toolchain is
    registered. It executes during `bazel build` but does not block downstream
    compilation.

    Args:
        name:                  Target name.
        srcs:                  TypeScript source files (.ts, .tsx).
        deps:                  Dependency targets providing TsDeclarationInfo + JsInfo.
        target:                ECMAScript target version (default "es2022").
        jsx_mode:              JSX transform mode (default "react-jsx").
        isolated_declarations: Whether to use isolated declarations (default True).
        enable_check:          Whether to run tsgo type-checking (default True).
        path_aliases:          Optional dict mapping path alias prefixes to workspace-relative
                               directory paths (e.g. {"@/": "src/"}). Injected into the tsgo
                               validation tsconfig so aliases like `import "@/components"`
                               resolve correctly during type-checking.
        vite_types:            When True, automatically prepends the Vite client-side ambient
                               type shim (@rules_typescript//ts:vite_env.d.ts) to srcs. This
                               provides types for import.meta.env, import.meta.hot, and asset
                               URL imports (*.svg, *.png, etc.) without requiring vite as a
                               compile-time dependency. Default False.
        **kwargs:              Additional args forwarded to the rule (e.g. visibility, tags).
    """
    if deps == None:
        deps = []

    if path_aliases != None:
        kwargs["path_aliases"] = path_aliases

    effective_srcs = srcs
    if vite_types:
        effective_srcs = ["@rules_typescript//ts:vite_env.d.ts"] + list(srcs)

    _ts_compile_rule(
        name = name,
        srcs = effective_srcs,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        isolated_declarations = isolated_declarations,
        enable_check = enable_check,
        **kwargs
    )

def ts_compile_legacy(
        name,
        srcs,
        deps = None,
        target = "es2022",
        jsx_mode = "react-jsx",
        enable_check = True,
        path_aliases = None,
        **kwargs):
    """Wrapper around ts_compile with isolated_declarations = False.

    Use this during a gradual rollout of isolated declarations.  Packages that
    have not yet added explicit return types to all exports should use this
    macro instead of ts_compile.  Once all violations in a package are fixed,
    replace ts_compile_legacy with ts_compile (which defaults to
    isolated_declarations = True).

    Migration workflow:
        1. Replace ts_compile with ts_compile_legacy for all existing targets.
        2. Add isolated-declarations/require-explicit-types to your ESLint config.
        3. Enable one package at a time: run the linter, fix violations,
           switch that package back to ts_compile.
        4. Repeat until all packages use ts_compile.

    Args:
        name:         Target name.
        srcs:         TypeScript source files (.ts, .tsx).
        deps:         Dependency targets providing TsDeclarationInfo + JsInfo.
        target:       ECMAScript target version (default "es2022").
        jsx_mode:     JSX transform mode (default "react-jsx").
        enable_check: Whether to run tsgo type-checking (default True).
        path_aliases: Optional path alias dict forwarded to ts_compile.
        **kwargs:     Additional args forwarded to the rule.
    """
    ts_compile(
        name = name,
        srcs = srcs,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        isolated_declarations = False,
        enable_check = enable_check,
        path_aliases = path_aliases,
        **kwargs
    )
