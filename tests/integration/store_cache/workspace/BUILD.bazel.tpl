"""Root BUILD file for the store_cache integration test workspace: the
lockfile's store, which the runner builds one tree of."""

load("@npm//:defs.bzl", "npm_virtual_store")

npm_virtual_store()
