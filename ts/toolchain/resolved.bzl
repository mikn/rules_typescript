"""Runnable targets for whichever toolchain is active.

    bazel run //ts/toolchain:oxc_resolved -- --help
    bazel run //ts/toolchain:tsgo_resolved -- --version
    bazel run //ts/toolchain:node_resolved -- --version
    bazel run //ts/toolchain:tools_resolved -- --help
    bazel run //ts/toolchain:launcher_resolved
"""

load("@toolchain_utils//toolchain:resolved.bzl", _resolved = "export")
load(
    "//ts/private:toolchain.bzl",
    "TOOLS_TOOLCHAIN_TYPE",
    "get_tools_toolchain",
)

oxc_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:oxc_toolchain_type"),
)

tsgo_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:tsgo_toolchain_type"),
)

node_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:js_runtime_type"),
)

tools_resolved = _resolved.rule(
    toolchain_type = TOOLS_TOOLCHAIN_TYPE,
)

launcher_resolved = _resolved.rule(
    toolchain_type = Label("//ts/toolchain:launcher_toolchain_type"),
)

def _lcov_merger_resolved_impl(ctx):
    merger = get_tools_toolchain(ctx).lcov_merger
    executable = ctx.actions.declare_file(ctx.label.name)
    ctx.actions.symlink(
        output = executable,
        target_file = merger,
        is_executable = True,
    )
    return [DefaultInfo(
        executable = executable,
        runfiles = ctx.runfiles([merger]),
    )]

lcov_merger_resolved = rule(
    implementation = _lcov_merger_resolved_impl,
    executable = True,
    toolchains = [TOOLS_TOOLCHAIN_TYPE],
    doc = """The tools toolchain's lcov_merger as an executable target.

Bazel's test setup reads a test rule's coverage merger from its `_lcov_merger`
attribute, so ts_test's default names this target and the binary it runs is
the toolchain's.
""",
)
