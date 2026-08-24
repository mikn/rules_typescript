"""JS runtime and JS tool toolchains for rules_typescript.

Node is used in two unrelated roles and they resolve against different
platforms:

  js_runtime_type  the runtime a ts_test / ts_binary program executes on.  It
                   belongs to the TARGET platform and is built for it.
  js_tool_type     node as a build tool (node_modules tree builder, ts_codegen,
                   next_build, bundlers).  It runs on the EXEC platform.

They are equal under a plain host build and differ the moment --platforms does.
Both are backed here by Node.js from rules_nodejs; a consumer can register
Deno, Bun, or a wrapper of their own for either role.
"""

load("//platforms:platforms.bzl", "constraints")

# ─── Toolchain type labels ─────────────────────────────────────────────────────

# Label(), not a string: these resolve in this file's repository mapping, so
# they keep working when a consumer gives rules_typescript another repo name.
JS_RUNTIME_TOOLCHAIN_TYPE = Label("//ts/toolchain:js_runtime_type")
JS_TOOL_TOOLCHAIN_TYPE = Label("//ts/toolchain:js_tool_type")

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

def _runtime_attrs(cfg):
    return {
        "runtime_binary": attr.label(
            doc = "The runtime executable.",
            mandatory = True,
            allow_single_file = True,
            executable = True,
            cfg = cfg,
        ),
        "runtime_name": attr.string(
            doc = "Human-readable name for diagnostics.",
            mandatory = True,
        ),
        "args_prefix": attr.string_list(
            doc = "Arguments prepended before the entrypoint script.",
            default = [],
        ),
    }

js_runtime_toolchain = rule(
    implementation = _js_runtime_toolchain_impl,
    attrs = _runtime_attrs("target"),
    doc = """Declares a JavaScript runtime that target-platform programs execute on.

The binary is built for the target platform: it is staged into the runfiles of
a ts_test or ts_binary and runs wherever that program runs.
""",
)

js_tool_toolchain = rule(
    implementation = _js_runtime_toolchain_impl,
    attrs = _runtime_attrs("exec"),
    doc = """Declares a JavaScript runtime used as a build tool.

The binary is built for the exec platform: it runs inside build actions on the
machine (or remote executor) performing the build, never on the target.
""",
)

# ─── Resolution helpers ────────────────────────────────────────────────────────

def get_js_runtime(ctx):
    """Resolves the target-platform JS runtime from the rule context.

    Use for a runtime staged into runfiles and executed by the built program.

    Args:
        ctx: The rule context.

    Returns:
        JsRuntimeInfo if the toolchain is registered, else None.
    """
    toolchain = ctx.toolchains[JS_RUNTIME_TOOLCHAIN_TYPE]
    if toolchain:
        return toolchain.runtime_info
    return None

def get_js_tool(ctx):
    """Resolves the exec-platform JS runtime from the rule context.

    Use for a runtime invoked by a build action.

    Args:
        ctx: The rule context.

    Returns:
        JsRuntimeInfo if the toolchain is registered, else None.
    """
    toolchain = ctx.toolchains[JS_TOOL_TOOLCHAIN_TYPE]
    if toolchain:
        return toolchain.runtime_info
    return None

# ─── Toolchain macros ──────────────────────────────────────────────────────────

# rules_nodejs names its per-platform repositories "nodejs_<platform>" for the
# platform keys we use, given node.toolchain(name = "nodejs") in MODULE.bazel.
NODE_PLATFORMS = [
    "linux_amd64",
    "linux_arm64",
    "darwin_amd64",
    "darwin_arm64",
    "windows_amd64",
]

def _declare_node_toolchains(name, toolchain_rule, toolchain_type, exec_bound):
    for platform in NODE_PLATFORMS:
        toolchain_name = "{}_{}".format(name, platform)
        toolchain_rule(
            name = "{}_impl".format(toolchain_name),
            runtime_binary = Label("@nodejs_{}//:node_bin".format(platform)),
            runtime_name = "node",
            # Manual so that a //... build does not download every platform's
            # Node distribution; resolution reaches the selected one directly.
            tags = ["manual"],
        )
        native.toolchain(
            name = toolchain_name,
            toolchain = ":{}_impl".format(toolchain_name),
            toolchain_type = toolchain_type,
            exec_compatible_with = constraints(platform) if exec_bound else [],
            target_compatible_with = [] if exec_bound else constraints(platform),
        )

def declare_node_runtime_toolchains(name):
    """Declares the target-platform Node.js toolchains, one per platform.

    Args:
        name: Base name prefix for the generated targets.
    """
    _declare_node_toolchains(
        name = name,
        toolchain_rule = js_runtime_toolchain,
        toolchain_type = JS_RUNTIME_TOOLCHAIN_TYPE,
        exec_bound = False,
    )

def declare_node_tool_toolchains(name):
    """Declares the exec-platform Node.js toolchains, one per platform.

    Args:
        name: Base name prefix for the generated targets.
    """
    _declare_node_toolchains(
        name = name,
        toolchain_rule = js_tool_toolchain,
        toolchain_type = JS_TOOL_TOOLCHAIN_TYPE,
        exec_bound = True,
    )
