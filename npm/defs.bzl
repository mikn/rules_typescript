"""Public API for npm dependency management in BUILD files.

Users should load from this file in their BUILD files:
    load("@rules_typescript//npm:defs.bzl", "node_modules")
    load("@rules_typescript//npm:defs.bzl", "npm_bin")

Note: npm_translate_lock is a repository rule used in MODULE.bazel via the
npm module extension, not in BUILD files.  To set up npm dependencies, use:

    # In MODULE.bazel:
    npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
    npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
    use_repo(npm, "npm")
"""

load("//ts/private:node_modules.bzl", _node_modules = "node_modules")
load("//ts/private:npm_bin.bzl", _npm_bin = "npm_bin")

node_modules = _node_modules
npm_bin = _npm_bin
