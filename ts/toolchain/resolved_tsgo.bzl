"""Resolved tsgo toolchain rule.

Exposes the active tsgo binary as a runnable Bazel target via the
toolchain_utils resolved pattern.  This enables:

    bazel run //ts/toolchain:tsgo_resolved -- --version
"""

load("@toolchain_utils//toolchain:resolved.bzl", _resolved = "export")

resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:tsgo_toolchain_type"),
)
