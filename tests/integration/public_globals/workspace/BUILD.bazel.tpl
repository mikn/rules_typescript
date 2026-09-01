"""One library holding both kinds of ambient, three targets over it, an untrue
claim about a .d.ts, and the same question asked of the shim `vite_types` adds.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

# node-shim.d.ts is the shape the default exists for: an ambient this package
# needs for its own build, and a collision with @types/node in any consumer
# that has the real thing. Nothing names it, so nothing carries it out of here.
# build-id.d.ts is the other kind, and public_globals is what says so.
ts_compile(
    name = "lib",
    srcs = [
        "src/lib/build-id.d.ts",
        "src/lib/index.ts",
        "src/lib/node-shim.d.ts",
    ],
    public_globals = ["src/lib/build-id.d.ts"],
    visibility = ["//visibility:public"],
)

ts_compile(
    name = "app",
    srcs = ["src/app/main.ts"],
    deps = [":lib"],
)

# The same dep and the same graph as :app, reaching for the ambient nobody
# exported. This target must NOT build.
ts_compile(
    name = "unexported",
    srcs = ["src/unexported/uses_shim.ts"],
    deps = [":lib"],
)

# A module has no globals to hand to a consumer, so exporting them is not a
# narrower `export` -- it is a statement about the file that the scan can see is
# wrong. secrets.d.ts exports, so it is a module.
ts_compile(
    name = "lying_public",
    srcs = [
        "src/lying_public/secrets.d.ts",
        "src/lying_public/thing.ts",
    ],
    public_globals = ["src/lying_public/secrets.d.ts"],
)

# vite_types prepends @rules_typescript//ts:vite_env.d.ts to srcs, so the Vite
# client globals are a src of this target like any other and stop here.
ts_compile(
    name = "vite_lib",
    srcs = ["src/vite_lib/index.ts"],
    visibility = ["//visibility:public"],
    vite_types = True,
)

# `import.meta.env` in a consumer of that target, with nothing said about it.
# ImportMeta is in lib, so the failure is on the member rather than the name.
# This target must NOT build.
ts_compile(
    name = "vite_consumer",
    srcs = ["src/vite_consumer/reads_env.ts"],
    deps = [":vite_lib"],
)

# The same source with the shim asked for here too, which is the migration.
ts_compile(
    name = "vite_consumer_typed",
    srcs = ["src/vite_consumer_typed/reads_env.ts"],
    vite_types = True,
    deps = [":vite_lib"],
)
