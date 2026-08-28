"""svelte_library rule — compiles .svelte components with the Svelte compiler.

A `.svelte` file is not TypeScript, so ts_compile cannot read it. svelte_library
runs `svelte/compiler`'s `compile()` over each source and propagates the two
kinds of output it returns:

    Card.svelte  →  Card.svelte.js      (JsInfo)
                    Card.svelte.js.map  (JsInfo)
                    Card.svelte.css     (CssInfo)

Both come out of one action per component, which is not an implementation
detail: Svelte scopes the CSS with a class whose hash appears in the JS as
well, so JS and CSS emitted by two actions would eventually disagree.

The Svelte compiler is not vendored — it comes from the node_modules tree the
target names, the same way next_build takes its `next`:

    node_modules(
        name = "node_modules",
        deps = ["@npm//:svelte"],
    )

    svelte_library(
        name = "components",
        srcs = ["Card.svelte"],
        node_modules = ":node_modules",
    )

Two things this rule does not do. It emits no .d.ts, so a `.ts` file importing
a component does not type-check against its props: that needs svelte2tsx, which
the compiler package does not ship. And `<script lang="ts">` is handled by the
compiler's own type stripping, which rejects TypeScript needing runtime emit
(`enum`, parameter properties) instead of transpiling it — such a component
fails the build with the compiler's `typescript_invalid_feature` error.
"""

load("//ts/private:providers.bzl", "CssInfo", "JsInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _svelte_library_impl(ctx):
    js_tool = get_js_tool(ctx)
    compiler = ctx.file._compiler

    nm_files = ctx.attr.node_modules[DefaultInfo].files.to_list()
    if not nm_files:
        fail(
            "svelte_library: 'node_modules' names a target with no files. It has " +
            "to be a node_modules() tree containing svelte:\n" +
            "  node_modules(\n" +
            "      name = \"node_modules\",\n" +
            "      deps = [\"@npm//:svelte\"],\n" +
            "  )",
        )
    node_modules_dir = nm_files[0]

    js_outputs = []
    js_map_outputs = []
    css_outputs = []
    for src in ctx.files.srcs:
        js_out = ctx.actions.declare_file(src.basename + ".js", sibling = src)
        map_out = ctx.actions.declare_file(src.basename + ".js.map", sibling = src)
        css_out = ctx.actions.declare_file(src.basename + ".css", sibling = src)

        ctx.actions.run(
            inputs = [src, compiler, node_modules_dir],
            outputs = [js_out, map_out, css_out],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [
                compiler.path,
                "--node-modules",
                node_modules_dir.path,
                "--generate",
                ctx.attr.generate,
                "--dev",
                str(ctx.attr.dev).lower(),
                # The scope-class hash is derived from this name as well as the
                # style block, so a path carrying the output tree's
                # configuration would change every class on --compilation_mode.
                "--filename",
                src.short_path,
                "--src",
                src.path,
                "--js-out",
                js_out.path,
                "--map-out",
                map_out.path,
                "--css-out",
                css_out.path,
            ],
            mnemonic = "SvelteCompile",
            progress_message = "SvelteCompile %{label} " + src.short_path,
        )
        js_outputs.append(js_out)
        js_map_outputs.append(map_out)
        css_outputs.append(css_out)

    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_css_sets = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_js_map_sets.append(dep[JsInfo].transitive_js_map_files)
        if CssInfo in dep:
            transitive_css_sets.append(dep[CssInfo].transitive_css_files)

    return [
        DefaultInfo(files = depset(js_outputs + js_map_outputs + css_outputs)),
        JsInfo(
            js_files = depset(js_outputs),
            js_map_files = depset(js_map_outputs),
            transitive_js_files = depset(js_outputs, transitive = transitive_js_sets, order = "postorder"),
            transitive_js_map_files = depset(js_map_outputs, transitive = transitive_js_map_sets, order = "postorder"),
        ),
        CssInfo(
            css_files = depset(css_outputs),
            transitive_css_files = depset(css_outputs, transitive = transitive_css_sets, order = "postorder"),
        ),
    ]

svelte_library = rule(
    implementation = _svelte_library_impl,
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    attrs = {
        "srcs": attr.label_list(
            doc = "Svelte component sources (*.svelte).",
            allow_files = [".svelte"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Targets whose JavaScript and CSS the components import, forwarded transitively.",
            providers = [[JsInfo], [CssInfo]],
        ),
        "node_modules": attr.label(
            doc = "A node_modules() tree containing svelte. The compiler is loaded from it at build time.",
            mandatory = True,
        ),
        "generate": attr.string(
            doc = "Which of the compiler's two outputs to emit: browser code ('client') or SSR code ('server').",
            default = "client",
            values = ["client", "server"],
        ),
        "dev": attr.bool(
            doc = "Compile with the compiler's `dev` option, which adds runtime checks and source locations.",
            default = False,
        ),
        "_compiler": attr.label(
            default = Label("//ts/private:svelte_compile.mjs"),
            allow_single_file = True,
        ),
    },
    doc = """Compiles .svelte components into JavaScript and scoped CSS.

Each source produces three declared outputs beside it in bazel-bin:

    Card.svelte  →  Card.svelte.js, Card.svelte.js.map, Card.svelte.css

The .js reaches consumers through JsInfo and the .css through CssInfo, so a
bundler or a ts_compile can take the target as a dep. The .css is present even
for a component with no <style> block, empty in that case — whether a component
has styles is not knowable at analysis time, and a declared output has to exist.

The output name keeps the source extension, so the import specifier does too:

    import Card from "./Card.svelte.js";

`generate` picks between the compiler's client and server output for the whole
target. Two targets in one package cannot compile the same source under
different settings — they would declare the same output file twice.

Requires the js_tool toolchain (Node.js on the exec platform) and a
node_modules tree containing svelte.

Example:
    node_modules(
        name = "node_modules",
        deps = ["@npm//:svelte"],
    )

    svelte_library(
        name = "components",
        srcs = ["Card.svelte", "List.svelte"],
        node_modules = ":node_modules",
    )
""",
)
