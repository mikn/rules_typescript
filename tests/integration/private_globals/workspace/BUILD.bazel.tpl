"""Four targets over one library that has both kinds of ambient.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

# node-shim.d.ts is the shape private_globals exists for: an ambient this
# package needs for its own build, and a collision with @types/node in any
# consumer that has the real thing. build-id.d.ts is the other kind.
ts_compile(
    name = "lib",
    srcs = [
        "src/lib/build-id.d.ts",
        "src/lib/index.ts",
        "src/lib/node-shim.d.ts",
    ],
    private_globals = ["src/lib/node-shim.d.ts"],
    visibility = ["//visibility:public"],
)

ts_compile(
    name = "app",
    srcs = ["src/app/main.ts"],
    deps = [":lib"],
)

# The same dep and the same graph as :app, reaching for the other global. This
# target must NOT build.
ts_compile(
    name = "withheld",
    srcs = ["src/withheld/uses_shim.ts"],
    deps = [":lib"],
)

# secrets.d.ts exports, so it is a module and its declarations were never in a
# consumer's scope. Withholding them states something about the file that is not
# true, and the scan that decides which srcs are global is where that is caught.
ts_compile(
    name = "lying",
    srcs = [
        "src/lying/secrets.d.ts",
        "src/lying/thing.ts",
    ],
    private_globals = ["src/lying/secrets.d.ts"],
)
