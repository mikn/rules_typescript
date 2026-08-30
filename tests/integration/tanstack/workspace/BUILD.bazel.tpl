"""Root BUILD file for the TanStack Start integration test workspace.

Everything else at this level -- node_modules, vite_bundler, ts_bundle -- is
what Gazelle generates from the @tanstack/* deps in package.json.
"""

load("@gazelle//:def.bzl", "gazelle")
load("@rules_typescript//npm:defs.bzl", "node_modules")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_typescript",
)

# The route-tree generator's npm tree, separate from the app's.
node_modules(
    name = "router_generator_node_modules",
    visibility = ["//visibility:public"],
    deps = ["@npm//:tanstack_router-generator"],
)
