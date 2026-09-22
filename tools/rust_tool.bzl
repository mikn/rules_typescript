"""Build a native ruleset executable with the ruleset's Rust toolchain."""

def _rust_tool_transition_impl(settings, _attr):
    return {
        "//command_line_option:extra_toolchains": settings["//command_line_option:extra_toolchains"] + [str(Label("@default_rust_toolchains//rustc:all"))],
    }

_rust_tool_transition = transition(
    implementation = _rust_tool_transition_impl,
    inputs = ["//command_line_option:extra_toolchains"],
    outputs = ["//command_line_option:extra_toolchains"],
)

def _rust_tool_impl(ctx):
    actual = ctx.attr.actual[0][DefaultInfo]
    suffix = ".exe" if actual.files_to_run.executable.extension == "exe" else ""
    executable = ctx.actions.declare_file(ctx.label.name + suffix)
    ctx.actions.symlink(output = executable, target_file = actual.files_to_run.executable, is_executable = True)
    return [DefaultInfo(
        executable = executable,
        runfiles = actual.default_runfiles.merge(ctx.runfiles(files = [executable])),
    )]

rust_tool = rule(
    implementation = _rust_tool_impl,
    executable = True,
    attrs = {
        "actual": attr.label(mandatory = True, cfg = _rust_tool_transition),
        "_allowlist_function_transition": attr.label(default = "@bazel_tools//tools/allowlists/function_transition_allowlist"),
    },
)
