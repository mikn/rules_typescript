"""Builds vite-plugin-bazel from its TypeScript sources with esbuild.

The plugin is a single bundled ESM file that a generated vite.config.mjs
imports.  Node comes from the js_tool toolchain (exec platform) and esbuild
through the importer's node_modules, so nothing is resolved from PATH and no
per-platform select is needed.
"""

load("//ts/private:providers.bzl", "NodeModulesInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _esbuild_bundle_impl(ctx):
    js_tool = get_js_tool(ctx)
    node_modules = ctx.attr.node_modules[NodeModulesInfo]
    if "esbuild" not in node_modules.links:
        fail(("esbuild_bundle: {} links no esbuild; add it to the " +
              "node_modules target's deps.").format(
            ctx.attr.node_modules.label,
        ))
    node_modules_dir = node_modules.dir

    out = ctx.actions.declare_file(ctx.attr.out)

    args = ctx.actions.args()
    args.add_all(js_tool.args_prefix)
    args.add("{}/esbuild/bin/esbuild".format(node_modules_dir))
    args.add(ctx.file.entry_point)
    args.add("--bundle")
    args.add("--platform=node")
    args.add("--format=esm")
    args.add("--target=" + ctx.attr.target)

    # Without this esbuild walks up from the entry point's REAL path -- the
    # source tree, since srcs reach the sandbox as symlinks -- and applies the
    # workspace tsconfig.json's `paths`, resolving a bundled npm package to the
    # //:refresh_tsconfig .d.ts tree instead of to node_modules.
    args.add("--tsconfig-raw={}")
    if ctx.attr.banner:
        args.add("--banner:js=" + ctx.attr.banner)
    args.add_all(ctx.attr.external, format_each = "--external:%s")
    args.add("--outfile=" + out.path)

    ctx.actions.run(
        inputs = depset(
            ctx.files.srcs,
            transitive = [ctx.attr.node_modules[DefaultInfo].files],
        ),
        outputs = [out],
        executable = js_tool.runtime_binary,
        arguments = [args],
        # esbuild's launcher requires('esbuild') to find its native binary.
        env = {"NODE_PATH": node_modules_dir},
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
            doc = "The importer's node_modules() target, linking esbuild.",
            providers = [NodeModulesInfo],
            mandatory = True,
        ),
        "external": attr.string_list(
            doc = "Module specifiers left unbundled (esbuild --external:).",
        ),
        "banner": attr.string(
            doc = """Text prepended to the bundle (esbuild --banner:js=).

An ESM bundle has no `require`, so a CJS dependency that require()s a node
builtin fails at load time. The fix is a createRequire banner, which esbuild's
own `__require` shim then picks up.""",
        ),
        "target": attr.string(
            doc = "esbuild --target value, e.g. 'node20'.",
            default = "node20",
        ),
    },
    doc = """Bundles TypeScript into a single ESM file with esbuild.

Used inside this ruleset to build vite-plugin-bazel. It is not part of the
public rule surface.
""",
)
