"""Resolved oxc toolchain rule.

Exposes the active oxc-bazel binary as a runnable Bazel target via the
toolchain_utils resolved pattern.  This enables:

    bazel run //ts/toolchain:oxc_resolved -- --help
"""

load("@toolchain_utils//toolchain:resolved.bzl", _resolved = "export")

resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:oxc_toolchain_type"),
)
