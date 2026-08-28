"""Records the binaries a rule's toolchains resolve to, for the tests here."""

load(
    "//ts/toolchain:defs.bzl",
    "JS_RUNTIME_TOOLCHAIN_TYPE",
    "JS_TOOL_TOOLCHAIN_TYPE",
    "OXC_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "get_js_runtime",
    "get_js_tool",
    "get_oxc_toolchain",
    "get_tsgo_toolchain",
)

ToolchainProbeInfo = provider(
    doc = "Execroot paths of the binaries the probe's toolchains resolved to.",
    fields = {
        "oxc": "string: path of the resolved oxc binary.",
        "tsgo": "string: path of the resolved tsgo binary.",
        "js_runtime": "string: path of the resolved target-platform JS runtime.",
        "js_tool": "string: path of the resolved exec-platform JS runtime.",
    },
)

def _toolchain_probe_impl(ctx):
    return [ToolchainProbeInfo(
        oxc = get_oxc_toolchain(ctx).oxc_binary.path,
        tsgo = get_tsgo_toolchain(ctx).tsgo_binary.path,
        js_runtime = get_js_runtime(ctx).runtime_binary.path,
        js_tool = get_js_tool(ctx).runtime_binary.path,
    )]

toolchain_probe = rule(
    implementation = _toolchain_probe_impl,
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        TSGO_TOOLCHAIN_TYPE,
        JS_RUNTIME_TOOLCHAIN_TYPE,
        JS_TOOL_TOOLCHAIN_TYPE,
    ],
    doc = "Analysis-only rule exposing the toolchain binaries that resolved for it.",
)
