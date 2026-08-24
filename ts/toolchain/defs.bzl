"""The toolchain contract of rules_typescript.

Everything a rule outside this ruleset needs in order to run oxc, tsgo, or a
JavaScript runtime: the toolchain type labels to declare, the providers they
carry, and the accessors that unwrap them.  The implementations live in
//ts/private, which by convention is loadable only from within //ts.
"""

load(
    "//ts/private:runtime.bzl",
    _JS_RUNTIME_TOOLCHAIN_TYPE = "JS_RUNTIME_TOOLCHAIN_TYPE",
    _JS_TOOL_TOOLCHAIN_TYPE = "JS_TOOL_TOOLCHAIN_TYPE",
    _JsRuntimeInfo = "JsRuntimeInfo",
    _get_js_runtime = "get_js_runtime",
    _get_js_tool = "get_js_tool",
)
load(
    "//ts/private:toolchain.bzl",
    _OXC_TOOLCHAIN_TYPE = "OXC_TOOLCHAIN_TYPE",
    _OxcToolchainInfo = "OxcToolchainInfo",
    _TSGO_TOOLCHAIN_TYPE = "TSGO_TOOLCHAIN_TYPE",
    _TsgoToolchainInfo = "TsgoToolchainInfo",
    _get_oxc_toolchain = "get_oxc_toolchain",
    _get_tsgo_toolchain = "get_tsgo_toolchain",
)

OXC_TOOLCHAIN_TYPE = _OXC_TOOLCHAIN_TYPE
TSGO_TOOLCHAIN_TYPE = _TSGO_TOOLCHAIN_TYPE
JS_RUNTIME_TOOLCHAIN_TYPE = _JS_RUNTIME_TOOLCHAIN_TYPE
JS_TOOL_TOOLCHAIN_TYPE = _JS_TOOL_TOOLCHAIN_TYPE

OxcToolchainInfo = _OxcToolchainInfo
TsgoToolchainInfo = _TsgoToolchainInfo
JsRuntimeInfo = _JsRuntimeInfo

get_oxc_toolchain = _get_oxc_toolchain
get_tsgo_toolchain = _get_tsgo_toolchain
get_js_runtime = _get_js_runtime
get_js_tool = _get_js_tool
