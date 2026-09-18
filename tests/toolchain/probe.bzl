"""Records the binaries a rule's toolchains resolve to, for the tests here."""

load(
    "//ts/toolchain:defs.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "LAUNCHER_TOOLCHAIN_TYPE",
    "OXC_TOOLCHAIN_TYPE",
    "TOOLS_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "get_js_runtime",
    "get_js_tool",
    "get_launcher_toolchain",
    "get_oxc_toolchain",
    "get_tools_toolchain",
    "get_tsgo_toolchain",
)

ToolchainProbeInfo = provider(
    doc = "Execroot paths of the binaries the probe's toolchains resolved to.",
    fields = {
        "oxc": "string: path of the resolved oxc binary.",
        "tsgo": "string: path of the resolved tsgo binary.",
        "tsaction": "string: path of the resolved tools toolchain's tsaction.",
        "launcher": "string: path of the resolved launcher, \"\" when none.",
        "js_runtime": "string: path of the resolved target-platform JS runtime.",
        "js_tool": "string: path of the resolved exec-platform JS runtime.",
    },
)

def _toolchain_probe_impl(ctx):
    launcher = get_launcher_toolchain(ctx)
    return [ToolchainProbeInfo(
        oxc = get_oxc_toolchain(ctx).oxc_binary.path,
        tsgo = get_tsgo_toolchain(ctx).tsgo_binary.path,
        tsaction = get_tools_toolchain(ctx).tsaction.path,
        launcher = launcher.launcher.path if launcher else "",
        js_runtime = get_js_runtime(ctx).runtime_binary.path,
        js_tool = get_js_tool(ctx).runtime_binary.path,
    )]

toolchain_probe = rule(
    implementation = _toolchain_probe_impl,
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        TSGO_TOOLCHAIN_TYPE,
        TOOLS_TOOLCHAIN_TYPE,
        config_common.toolchain_type(
            LAUNCHER_TOOLCHAIN_TYPE,
            mandatory = False,
        ),
        JS_RUNTIME_TOOLCHAIN_TYPE,
        JS_TOOL_TOOLCHAIN_TYPE,
    ],
    doc = "Analysis-only rule exposing the toolchain binaries that resolved for it.",
)
