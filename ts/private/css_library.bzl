"""css_library rule — collects .css files and propagates them via CssInfo."""

load("//ts/private:providers.bzl", "CssInfo", "TsDeclarationInfo")

# Minimal ambient declaration emitted alongside each .css file so that
# TypeScript (with allowArbitraryExtensions: true) does not error when a .tsx
# or .ts file imports the CSS file as a side-effect.
#
# The content is intentionally empty — side-effect CSS imports have no typed
# export surface.  For CSS modules (import styles from "./Foo.module.css") a
# richer .d.ts with `export default` would be needed, but that is a separate
# feature (3.2 CSS Modules) not in scope here.
_CSS_DTS_CONTENT = """// Auto-generated CSS ambient declaration.
// This file allows TypeScript (allowArbitraryExtensions: true) to accept
// side-effect CSS imports without type errors.
export {};
"""

def _css_library_impl(ctx):
    css_files = ctx.files.srcs

    # Generate a .css.d.ts ambient declaration for each .css source so that
    # tsgo/TypeScript does not error on `import "./foo.css"` when
    # allowArbitraryExtensions is enabled.
    dts_outputs = []

    # A copy in bazel-bin, not the source file. What imports this CSS is the
    # compiled .js, which lives in bazel-bin, and `import "./button.css"` is
    # resolved relative to the importer -- so a bundler running over the output
    # tree looks for the CSS beside that .js and a source-tree-only .css is not
    # there. The dev server is unaffected: it serves source from the workspace
    # root, where both sit side by side already.
    bin_css_files = []
    for css_file in css_files:
        dts = ctx.actions.declare_file(css_file.basename + ".d.ts", sibling = css_file)
        ctx.actions.write(output = dts, content = _CSS_DTS_CONTENT)
        dts_outputs.append(dts)

        # A copy, not a symlink: a bundler realpaths a symlink before resolving
        # what the CSS itself imports, so `@import "tailwindcss"` from a symlink
        # would look for a source-tree node_modules that does not exist.
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

    # Build the transitive depsets from any css_library deps.
    transitive_css_sets = []
    transitive_dts_sets = []
    npm_closure_sets = []
    global_entry_sets = []
    for dep in ctx.attr.deps:
        if CssInfo in dep:
            transitive_css_sets.append(dep[CssInfo].transitive_css_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            npm_closure_sets.append(dep[TsDeclarationInfo].transitive_npm_packages)
            global_entry_sets.append(dep[TsDeclarationInfo].transitive_global_entry_files)

    direct_css = depset(bin_css_files)
    transitive_css = depset(bin_css_files, transitive = transitive_css_sets, order = "postorder")
    direct_dts = depset(dts_outputs)
    transitive_dts = depset(dts_outputs, transitive = transitive_dts_sets, order = "postorder")

    return [
        DefaultInfo(files = depset(bin_css_files + dts_outputs)),
        CssInfo(
            css_files = direct_css,
            transitive_css_files = transitive_css,
        ),
        # Expose the generated .css.d.ts files through TsDeclarationInfo so
        # that ts_compile can pick them up in its transitive_dts_sets and
        # include them in the inputs to the tsgo validation action.
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            transitive_npm_packages = depset(transitive = npm_closure_sets, order = "postorder"),
            global_entry_files = depset(),
            transitive_global_entry_files = depset(transitive = global_entry_sets, order = "postorder"),
        ),
    ]

css_library = rule(
    implementation = _css_library_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "CSS source files.",
            allow_files = [".css"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Other css_library targets whose CSS this target transitively depends on.",
            providers = [[CssInfo]],
        ),
    },
    doc = """Collects CSS files and makes them available to ts_compile and ts_bundle.

A css_library target provides CssInfo and TsDeclarationInfo (with generated
.css.d.ts ambient declarations) so that:

  1. TypeScript compilation (tsgo validation) does not error on CSS side-effect
     imports when allowArbitraryExtensions is true.
  2. ts_compile targets can declare a CSS dependency without failing on the
     absence of JsInfo.
  3. The .css files are copied untransformed into bazel-bin beside the compiled
     .js that imports them, which is what makes `import "./button.css"` resolve
     for a bundler running over the output tree.

Example:
    css_library(
        name = "button_styles",
        srcs = ["button.css"],
    )

    ts_compile(
        name = "button",
        srcs = ["Button.tsx"],
        deps = [":button_styles"],
    )
""",
)
