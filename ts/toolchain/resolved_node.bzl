"""Resolved Node.js runtime toolchain rule.

Exposes the active Node.js binary as a runnable Bazel target via the
toolchain_utils resolved pattern.  This enables:

    bazel run //ts/toolchain:node_resolved -- --version
"""

load("@toolchain_utils//toolchain:resolved.bzl", _resolved = "export")

resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:js_runtime_type"),
)
