"""Root BUILD file for the node_version integration test workspace.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@npm//:defs.bzl", "npm_virtual_store")
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_test")

npm_virtual_store(name = "node_modules/.pnpm")

node_modules(
    name = "node_modules",
    hoist = ":node_modules/.pnpm/node_modules",
    deps = ["@npm//:types_node"],
)

# .nvmrc is staged beside the test, which compares it to process.version.
ts_test(
    name = "node_version_test",
    srcs = ["node_version.test.ts"],
    data = [".nvmrc"],
    emit = True,
    node_modules = ":node_modules",
    runner = "@rules_typescript//ts/runners:node_test",
    deps = ["@npm//:types_node"],
)
