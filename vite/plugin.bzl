"""Builds vite-plugin-bazel from its TypeScript sources with esbuild.

The plugin is a single bundled ESM file that a generated vite.config.mjs
imports.  Node comes from the js_tool toolchain (exec platform) and esbuild
from a node_modules tree, so nothing is resolved from PATH and no per-platform
select is needed.
"""

load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _esbuild_bundle_impl(ctx):
    js_tool = get_js_tool(ctx)
    node_modules_files = ctx.files.node_modules
    if not node_modules_files:
        fail("esbuild_bundle: 'node_modules' produced no files; it must be a node_modules() target containing esbuild.")
    node_modules_dir = node_modules_files[0]

    out = ctx.actions.declare_file(ctx.attr.out)

    args = ctx.actions.args()
    args.add_all(js_tool.args_prefix)
    args.add("{}/esbuild/bin/esbuild".format(node_modules_dir.path))
    args.add(ctx.file.entry_point)
    args.add("--bundle")
    args.add("--platform=node")
    args.add("--format=esm")
    args.add("--target=" + ctx.attr.target)
    args.add_all(ctx.attr.external, format_each = "--external:%s")
    args.add("--outfile=" + out.path)

    ctx.actions.run(
        inputs = depset(ctx.files.srcs, transitive = [depset(node_modules_files)]),
        outputs = [out],
        executable = js_tool.runtime_binary,
        arguments = [args],
        # esbuild's launcher requires('esbuild') to find its native binary.
        env = {"NODE_PATH": node_modules_dir.path},
        mnemonic = "EsbuildBundle",
        progress_message = "EsbuildBundle %{label}",
    )

    return [DefaultInfo(files = depset([out]))]

esbuild_bundle = rule(
    implementation = _esbuild_bundle_impl,
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    attrs = {
        "srcs": attr.label_list(
            doc = "All TypeScript sources reachable from entry_point.",
            allow_files = [".ts", ".mts", ".js", ".mjs", ".json"],
            mandatory = True,
        ),
        "entry_point": attr.label(
            doc = "The bundle entry point; must also be listed in srcs.",
            allow_single_file = [".ts", ".mts", ".js", ".mjs"],
            mandatory = True,
        ),
        "out": attr.string(
            doc = "Name of the bundled output file.",
            mandatory = True,
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target containing esbuild.",
            allow_files = True,
            mandatory = True,
        ),
        "external": attr.string_list(
            doc = "Module specifiers left unbundled (esbuild --external:).",
        ),
        "target": attr.string(
            doc = "esbuild --target value, e.g. 'node20'.",
            default = "node20",
        ),
    },
    doc = """Bundles TypeScript into a single ESM file with esbuild.

Used inside this ruleset to build vite-plugin-bazel.  It is not part of the
public rule surface: ts_bundle is the rule for bundling application code.
""",
)
