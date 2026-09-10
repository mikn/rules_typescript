"""Root BUILD file for the npmrc_registry integration test workspace. The
consumer's BUILD file is the runner's: a fetch through the registry is what is
under test, so nothing here runs Gazelle."""

load("@npm//:defs.bzl", "npm_virtual_store")
load("@rules_typescript//npm:defs.bzl", "node_modules")

npm_virtual_store(name = "node_modules/.pnpm")

node_modules(
    name = "node_modules",
    visibility = ["//visibility:public"],
    deps = ["@npm//:acme_greeter"],
)
