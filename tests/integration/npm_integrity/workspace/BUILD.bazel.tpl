"""Root BUILD file for the npm_integrity integration test workspace.

The lockfile's store and nothing else: the failure under test happens while
the module extension is evaluated, so naming a target in @npm is all it takes
to reach it.
"""

load("@npm//:defs.bzl", "npm_virtual_store")

npm_virtual_store(name = "node_modules/.pnpm")
