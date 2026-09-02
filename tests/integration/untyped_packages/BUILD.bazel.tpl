"""The targets the runner builds, most of which must fail."""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

# gazelle:exclude src

ts_compile(
    name = "no_import",
    srcs = ["src/no_import.ts"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "leaks",
    srcs = ["src/leaks.ts"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "excluded",
    srcs = ["src/excluded.ts"],
    untyped_packages = ["@cloudflare/workers-types"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "no_shim",
    srcs = ["src/no_shim.ts"],
    untyped_packages = ["wrangler"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "shimmed",
    srcs = [
        "src/shimmed.ts",
        "src/wrangler.d.ts",
    ],
    untyped_packages = ["wrangler"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "typo",
    srcs = ["src/typo.ts"],
    untyped_packages = ["@cloudflare/worker-types"],
    deps = ["@npm//:wrangler"],
)

# @cloudflare/workers-types is REACHABLE through wrangler but is not a direct
# dep, so strict deps normally names it and says what to add. The exclusion
# takes it out of the reachable set too, and then the import is simply
# unresolved -- which is the honest answer, because adding the dep back would
# not type it.
ts_compile(
    name = "reachable_declared",
    srcs = ["src/reachable_strictdeps.ts"],
    deps = ["@npm//:wrangler"],
)

ts_compile(
    name = "reachable_untyped",
    srcs = ["src/reachable_strictdeps.ts"],
    untyped_packages = ["@cloudflare/workers-types"],
    deps = ["@npm//:wrangler"],
)
