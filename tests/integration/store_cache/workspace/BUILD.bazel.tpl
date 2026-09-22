"""Root BUILD file for the store_cache integration test workspace: the
lockfile's store, which the runner builds one tree of, and a test that runs in
it under the modes CI runs tests in."""

load("@npm//:defs.bzl", "npm_virtual_store")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_test")

npm_virtual_store(name = "node_modules/.pnpm")

node_modules(
    name = "node_modules",
    hoist = ":node_modules/.pnpm/node_modules",
    deps = ["@npm//:strip-ansi"],
)

# strip-ansi's import of ansi-regex resolves from its tree to the edge beside
# it, whatever layout the runfiles take.
ts_test(
    name = "strip_ansi_test",
    srcs = ["strip_ansi.test.js"],
    node_modules = ":node_modules",
    runner = "@rules_typescript//ts/runners:node_test",
    deps = ["@npm//:strip-ansi"],
)
