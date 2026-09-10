"""Root BUILD file for the npmrc_registry integration test workspace. The
consumer's BUILD file is the runner's: a fetch through the registry is what is
under test, so nothing here runs Gazelle."""

load("@npm//:defs.bzl", "npm_virtual_store")

npm_virtual_store(name = "node_modules/.pnpm")
