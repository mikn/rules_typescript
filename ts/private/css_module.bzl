"""css_module rule — processes .module.css files and generates typed .d.ts declarations.

CSS Modules (*.module.css) differ from plain CSS:
  - They are imported with a default import:  import styles from "./Button.module.css"
  - The import value is an object mapping class names to opaque strings.
  - TypeScript needs a typed .d.ts declaration for each .module.css file.

This rule:
  1. Parses class names out of each .module.css source with css_module_dts.mjs,
     run on the js_tool toolchain's Node.
  2. Generates a typed .d.ts declaration for each file.
  3. Propagates both the .css and .d.ts files through CssModuleInfo and
     TsDeclarationInfo so that ts_compile can consume them.

The generated .d.ts looks like:
    declare const styles: {
      readonly container: string;
      readonly button: string;
    };
    export default styles;
"""

load("//ts/private:providers.bzl", "CssModuleInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

def _css_module_impl(ctx):
    css_files = ctx.files.srcs
    js_tool = get_js_tool(ctx)
    generator = ctx.file._generator

    dts_outputs = []

    # A copy in bazel-bin, not the source file, for the reason css_library
    # copies too: the importer is the compiled .js beside it, so a relative
    # import resolves in the output tree rather than in the source tree.
    bin_css_files = []
    for css_file in css_files:
        # The <source>.d.ts name is what TypeScript looks for with
        # allowArbitraryExtensions enabled.
        dts = ctx.actions.declare_file(css_file.basename + ".d.ts", sibling = css_file)

        ctx.actions.run(
            inputs = [css_file, generator],
            outputs = [dts],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [
                generator.path,
                css_file.path,
                dts.path,
            ],
            mnemonic = "CssModuleDts",
            progress_message = "CssModuleDts %{label}",
        )
        dts_outputs.append(dts)

        # A generated src is already in bazel-bin, and declaring an output at a
        # path another rule owns is an error rather than a copy.
        if css_file.is_source:
            bin_css = ctx.actions.declare_file(css_file.basename, sibling = css_file)
            ctx.actions.expand_template(
                template = css_file,
                output = bin_css,
                substitutions = {},
            )
            bin_css_files.append(bin_css)
        else:
            bin_css_files.append(css_file)

    # Build transitive depsets from any css_module deps.
    transitive_css_sets = []
    transitive_dts_sets = []
    for dep in ctx.attr.deps:
        if CssModuleInfo in dep:
            transitive_css_sets.append(dep[CssModuleInfo].transitive_css_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)

    direct_css = depset(bin_css_files)
    transitive_css = depset(bin_css_files, transitive = transitive_css_sets, order = "postorder")
    direct_dts = depset(dts_outputs)
    transitive_dts = depset(dts_outputs, transitive = transitive_dts_sets, order = "postorder")

    return [
        DefaultInfo(files = depset(bin_css_files + dts_outputs)),
        CssModuleInfo(
            css_files = direct_css,
            transitive_css_files = transitive_css,
        ),
        # Expose the generated .d.ts files through TsDeclarationInfo so that
        # ts_compile can pick them up as declaration inputs for type-checking.
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
        ),
    ]

css_module = rule(
    implementation = _css_module_impl,
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    attrs = {
        "srcs": attr.label_list(
            doc = "CSS Module source files (*.module.css).",
            allow_files = [".css"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Other css_module targets whose CSS this target composes from.",
            providers = [[CssModuleInfo]],
        ),
        "_generator": attr.label(
            default = Label("//ts/private:css_module_dts.mjs"),
            allow_single_file = True,
        ),
    },
    doc = """Processes CSS Module files and generates typed TypeScript declarations.

A css_module target provides CssModuleInfo and TsDeclarationInfo (with
generated .module.css.d.ts typed declarations) so that:

  1. TypeScript accepts 'import styles from \"./Button.module.css\"' and
     provides typed access to class names (e.g. styles.container).
  2. ts_compile targets can declare a CSS Module dependency without failing.
  3. The .module.css files are copied untransformed into bazel-bin beside the
     compiled .js that imports them, which is what lets the bundler resolve
     the relative import. Vite handles CSS Modules natively from there,
     applying local scoping and class name mangling.

The generated .d.ts maps each class name found in the CSS to a string:

    declare const styles: {
      readonly container: string;
      readonly button: string;
    };
    export default styles;

Class names come from selectors only: declaration values, comments, strings,
at-rule preludes, @keyframes/@font-face bodies and :global(...) groups
contribute none.  A class scoped globally by the selector-level form
(':global .foo', with no parentheses) is still declared.

Requires the js_tool toolchain (Node.js on the exec platform).

Example:
    css_module(
        name = "button_module",
        srcs = ["Button.module.css"],
    )

    ts_compile(
        name = "button",
        srcs = ["Button.tsx"],
        deps = [":button_module"],
    )
""",
)
