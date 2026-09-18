"""Root BUILD file for the consumer_crates integration test workspace.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

# One file through oxc-bazel, which the ruleset's own hub builds.
ts_compile(
    name = "hello",
    srcs = ["src/hello.ts"],
)
