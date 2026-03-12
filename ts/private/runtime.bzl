"""JS runtime toolchain for rules_typescript.

Provides a pluggable JavaScript runtime (Node, Deno, Bun, etc.) for
ts_test and ts_binary rules. The default registered runtime is Node.js,
sourced from rules_nodejs.
"""

# ─── Toolchain type label ──────────────────────────────────────────────────────

# Must match the toolchain_type() target in //ts/toolchain/BUILD.bazel.
JS_RUNTIME_TOOLCHAIN_TYPE = "@rules_typescript//ts/toolchain:js_runtime_type"

# ─── Provider ──────────────────────────────────────────────────────────────────

JsRuntimeInfo = provider(
    doc = "Information about a JavaScript runtime.",
    fields = {
        "runtime_binary": "File: The runtime executable (node, deno, bun, etc.).",
        "runtime_name": "string: Human-readable name used in diagnostics.",
        "args_prefix": "list of string: Arguments prepended before the entrypoint script.",
    },
)

# ─── Rule implementation ────────────────────────────────────────────────────────

def _js_runtime_toolchain_impl(ctx):
    binary = ctx.file.runtime_binary
    runtime_info = JsRuntimeInfo(
        runtime_binary = binary,
        runtime_name = ctx.attr.runtime_name,
        args_prefix = ctx.attr.args_prefix,
    )
    toolchain_info = platform_common.ToolchainInfo(
        runtime_info = runtime_info,
        # Standard fields consumed by @toolchain_utils//toolchain:resolved.bzl.
        executable = binary,
        variable = "NODE",
        default = DefaultInfo(
            files = depset([binary]),
            runfiles = ctx.runfiles([binary]),
        ),
    )
    return [toolchain_info]

js_runtime_toolchain = rule(
    implementation = _js_runtime_toolchain_impl,
    attrs = {
        "runtime_binary": attr.label(
            doc = "The runtime executable.",
            mandatory = True,
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
        "runtime_name": attr.string(
            doc = "Human-readable name for diagnostics.",
            mandatory = True,
        ),
        "args_prefix": attr.string_list(
            doc = "Arguments prepended before the entrypoint script.",
            default = [],
        ),
    },
    doc = "Declares a JavaScript runtime toolchain instance.",
)

# ─── Resolution helper ─────────────────────────────────────────────────────────

def get_js_runtime(ctx):
    """Resolves the JS runtime toolchain from the rule context.

    Args:
        ctx: The rule context.

    Returns:
        JsRuntimeInfo if the toolchain is registered, else None.
    """
    toolchain = ctx.toolchains[JS_RUNTIME_TOOLCHAIN_TYPE]
    if toolchain:
        return toolchain.runtime_info
    return None

# ─── declare_node_runtime_toolchains macro ─────────────────────────────────────

# Map from our platform key to the rules_nodejs repository name suffix.
# rules_nodejs creates repos named "nodejs_<platform>" when node.toolchain is
# called with name = "nodejs" (the explicit default in MODULE.bazel).
_NODEJS_REPO_PLATFORM = {
    "linux_amd64": "nodejs_linux_amd64",
    "linux_arm64": "nodejs_linux_arm64",
    "darwin_amd64": "nodejs_darwin_amd64",
    "darwin_arm64": "nodejs_darwin_arm64",
    "windows_amd64": "nodejs_windows_amd64",
}

# Platform constraints for Node.js toolchains.  Kept separate from
# PLATFORM_CONSTRAINTS in toolchain.bzl for clarity; both maps now include
# linux_arm64 and windows_amd64.
_NODE_PLATFORM_CONSTRAINTS = {
    "linux_amd64": {
        "os": "@platforms//os:linux",
        "cpu": "@platforms//cpu:x86_64",
    },
    "linux_arm64": {
        "os": "@platforms//os:linux",
        "cpu": "@platforms//cpu:aarch64",
    },
    "darwin_amd64": {
        "os": "@platforms//os:macos",
        "cpu": "@platforms//cpu:x86_64",
    },
    "darwin_arm64": {
        "os": "@platforms//os:macos",
        "cpu": "@platforms//cpu:aarch64",
    },
    "windows_amd64": {
        "os": "@platforms//os:windows",
        "cpu": "@platforms//cpu:x86_64",
    },
}

def declare_node_runtime_toolchains(name):
    """Declares js_runtime_toolchain targets for Node.js on all supported platforms.

    Uses the node binary provided by rules_nodejs (via the "node" module extension
    with name = "nodejs" in MODULE.bazel). Each platform's binary comes from the
    corresponding @nodejs_<platform>//:node_bin target, which is an alias to the
    raw Node.js executable (confirmed not a wrapper script).

    Register them in MODULE.bazel with:

        register_toolchains(
            "//ts/toolchain:node_linux_amd64",
            "//ts/toolchain:node_linux_arm64",
            "//ts/toolchain:node_darwin_amd64",
            "//ts/toolchain:node_darwin_arm64",
            "//ts/toolchain:node_windows_amd64",
        )

    Args:
        name: Base name prefix for the generated targets.
    """
    for platform, constraints in _NODE_PLATFORM_CONSTRAINTS.items():
        if platform not in _NODEJS_REPO_PLATFORM:
            fail(
                "declare_node_runtime_toolchains: platform '{}' is listed in " +
                "_NODE_PLATFORM_CONSTRAINTS but has no entry in _NODEJS_REPO_PLATFORM. " +
                "Add '{}': 'nodejs_<suffix>' to _NODEJS_REPO_PLATFORM in runtime.bzl.".format(
                    platform,
                    platform,
                ),
            )
        nodejs_repo = _NODEJS_REPO_PLATFORM[platform]
        toolchain_name = "{}_{}".format(name, platform)
        js_runtime_toolchain(
            name = "{}_impl".format(toolchain_name),
            # @nodejs_<platform>//:node_bin is an alias to the raw Node.js
            # executable (not a wrapper script) as confirmed by:
            #   bazel query --output=build @nodejs_linux_amd64//:node_bin
            runtime_binary = "@{}//:node_bin".format(nodejs_repo),
            runtime_name = "node",
        )
        native.toolchain(
            name = toolchain_name,
            toolchain = ":{}_impl".format(toolchain_name),
            toolchain_type = JS_RUNTIME_TOOLCHAIN_TYPE,
            target_compatible_with = [
                constraints["os"],
                constraints["cpu"],
            ],
        )
