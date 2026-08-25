# ts_bundle needs exactly one .js from entry_point, so the client entry is its
# own target rather than being merged into the package's :app.
# gazelle:ts_exclude entry.client.tsx
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
    deps = [
        "@npm//:react",
        "@npm//:react-dom",
        "@npm//:remix-run_react",
    ],
)
