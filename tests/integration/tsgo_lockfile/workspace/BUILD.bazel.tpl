"""Root BUILD file for the tsgo_lockfile integration test workspace.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

exports_files(["pnpm-lock.yaml"])

# One file type-checked by whichever compiler the lockfile named.
ts_compile(
    name = "hello",
    srcs = ["src/hello.ts"],
)
