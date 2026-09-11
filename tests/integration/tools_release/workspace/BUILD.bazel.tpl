"""Root BUILD file for the tools_release integration test workspace.

Renamed to BUILD.bazel in the scratch copy: a real BUILD.bazel here would make
workspace/ a subpackage of the parent, where glob() cannot see it.
"""

load("@rules_typescript//ts:defs.bzl", "ts_binary", "ts_compile")

# One file through TsConfig, TsEmit and TsgoDeclare: tsaction from the release.
ts_compile(
    name = "hello",
    srcs = ["src/hello.ts"],
)

# One script under the launcher from the release.
ts_binary(
    name = "hello_bin",
    entry_point = "src/main.js",
)
