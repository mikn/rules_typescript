"""Public API for rules_typescript.

Users should load rules from this file:
    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_test", "ts_binary")
    load("@rules_typescript//ts:defs.bzl", "BundlerInfo")
    load("@rules_typescript//ts:defs.bzl", "ts_lint", "TsLintInfo")
    load("@rules_typescript//ts:defs.bzl", "css_library", "css_module", "asset_library")
    load("@rules_typescript//ts:defs.bzl", "json_library")
    load("@rules_typescript//ts:defs.bzl", "ts_pnpm", "ts_add_package", "ts_refresh_tsconfig")
    load("@rules_typescript//ts:defs.bzl", "ts_codegen", "refresh_workspace_files")
"""

load("//ts/private:asset_library.bzl", _asset_library = "asset_library")
load("//ts/private:css_library.bzl", _css_library = "css_library")
load("//ts/private:css_module.bzl", _css_module = "css_module")
load("//ts/private:json_library.bzl", _json_library = "json_library")
load("//ts/private:pnpm.bzl", _ts_add_package = "ts_add_package", _ts_pnpm = "ts_pnpm")
load("//ts/private:providers.bzl", _AssetInfo = "AssetInfo", _BundlerInfo = "BundlerInfo", _CssInfo = "CssInfo", _CssModuleInfo = "CssModuleInfo", _JsInfo = "JsInfo", _TsDeclarationInfo = "TsDeclarationInfo")
load("//ts/private:ts_binary.bzl", _ts_binary = "ts_binary")
load("//ts/private:ts_codegen.bzl", _ts_codegen = "ts_codegen")
load("//ts/private:ts_compile.bzl", _ts_compile = "ts_compile")
load("//ts/private:ts_config.bzl", _ts_config = "ts_config")
load("//ts/private:ts_dev_server.bzl", _ts_dev_server = "ts_dev_server")
load("//ts/private:ts_lint.bzl", _TsLintInfo = "TsLintInfo", _ts_lint = "ts_lint")
load("//ts/private:ts_test.bzl", _ts_test = "ts_test")
load("//ts/private:tsconfig_aspect.bzl", _refresh_workspace_files = "refresh_workspace_files", _ts_refresh_tsconfig = "ts_refresh_tsconfig")

# Providers — exported for use in custom rules that extend this ruleset.
AssetInfo = _AssetInfo
BundlerInfo = _BundlerInfo
CssInfo = _CssInfo
CssModuleInfo = _CssModuleInfo
JsInfo = _JsInfo
TsDeclarationInfo = _TsDeclarationInfo
TsLintInfo = _TsLintInfo

# CSS / asset / JSON support.
asset_library = _asset_library
css_library = _css_library
css_module = _css_module
json_library = _json_library

# The compile rule: srcs, deps, tsconfig. Every compiler option is the
# tsconfig's; the emit knobs are the flags in //ts:BUILD.bazel.
ts_compile = _ts_compile

# Standalone rules for advanced use cases.
ts_codegen = _ts_codegen
ts_config = _ts_config
ts_test = _ts_test

# Runnable entry point; `bundler` takes any target returning BundlerInfo.
ts_binary = _ts_binary

# Dev server rule.
ts_dev_server = _ts_dev_server

# Lint rule.
ts_lint = _ts_lint

# Hermetic pnpm workspace macros.
ts_pnpm = _ts_pnpm
ts_add_package = _ts_add_package
ts_refresh_tsconfig = _ts_refresh_tsconfig

# Copies build outputs into the source tree under `bazel run`. Pair it with
# diff_test for a generated file that has to be checked in.
refresh_workspace_files = _refresh_workspace_files
