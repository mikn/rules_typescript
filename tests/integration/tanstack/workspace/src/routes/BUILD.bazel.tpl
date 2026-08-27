# The route tree is what every createFileRoute() is typed against, and Gazelle
# drops every *.gen.ts from a source target -- so it is the one src, and the one
# whole target, that is not Gazelle's here.
load("@bazel_skylib//rules:diff_test.bzl", "diff_test")
load("@rules_typescript//ts:defs.bzl", "refresh_workspace_files", "ts_codegen", "ts_compile")

ts_compile(
    name = "routes",
    srcs = [
        "routeTree.gen.ts",  # keep
    ],
    visibility = ["//visibility:public"],
)

# `# keep` on outs: Gazelle prunes a ts_codegen attr it did not itself write.
ts_codegen(
    name = "route_tree",
    srcs = glob(["**/*.tsx"]),
    outs = ["routeTree.gen.expected.ts"],  # keep
    args = [
        "--out",
        "{out}",
        "--srcs",
        "{srcs}",
    ],
    generator = "@rules_typescript//tools/codegen:tanstack_routes",
    node_modules = "//:router_generator_node_modules",
)

refresh_workspace_files(
    name = "update_route_tree",
    files = {":route_tree": "src/routes/routeTree.gen.ts"},
    visibility = ["//visibility:public"],
)

diff_test(
    name = "route_tree_test",
    size = "small",
    failure_message = "src/routes/routeTree.gen.ts is stale: run `bazel run //src/routes:update_route_tree`.",
    file1 = ":route_tree",
    file2 = "routeTree.gen.ts",
)
