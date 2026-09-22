"""Root BUILD file for the consumer_crates integration test workspace.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_rs//rs:rust_binary.bzl", "rust_binary")
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_config")

# One file through oxc-bazel, which the ruleset's own hub builds.
ts_compile(
    name = "hello",
    srcs = ["src/hello.ts"],
)

ts_config(
    name = "commonjs_config",
    src = "tsconfig.commonjs.json",
    module = "node16",
)

ts_compile(
    name = "hello_commonjs",
    srcs = ["src/commonjs.ts"],
    tsconfig = ":commonjs_config",
)

rust_binary(
    name = "consumer",
    srcs = ["src/consumer.rs"],
    edition = "2024",
    deps = ["@crates//:cfg-if"],
)
