"""Runnable targets for whichever toolchain is active.

    bazel run //ts/toolchain:oxc_resolved -- --help
    bazel run //ts/toolchain:tsgo_resolved -- --version
    bazel run //ts/toolchain:node_resolved -- --version
"""

load("@toolchain_utils//toolchain:resolved.bzl", _resolved = "export")

oxc_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:oxc_toolchain_type"),
)

tsgo_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:tsgo_toolchain_type"),
)

node_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:js_runtime_type"),
)
