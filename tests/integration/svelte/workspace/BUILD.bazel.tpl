"""Root BUILD file for the svelte_library integration test workspace."""

load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "svelte_library")

node_modules(
    name = "node_modules",
    deps = ["@npm//:svelte"],
)

svelte_library(
    name = "components",
    srcs = [
        "src/Card.svelte",
        "src/nested/Plain.svelte",
    ],
    node_modules = ":node_modules",
)

svelte_library(
    name = "components_ssr",
    srcs = ["src/Badge.svelte"],
    generate = "server",
    node_modules = ":node_modules",
)
