"""css_module rule — processes .module.css files and generates typed .d.ts declarations.

CSS Modules (*.module.css) differ from plain CSS:
  - They are imported with a default import:  import styles from "./Button.module.css"
  - The import value is an object mapping class names to opaque strings.
  - TypeScript needs a typed .d.ts declaration for each .module.css file.

This rule:
  1. Parses class names from the .module.css source using a shell/awk action.
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

# ── Typed .d.ts generation ───────────────────────────────────────────────────
#
# We use a run_shell action with awk to extract CSS class names and emit the
# typed declaration.  This avoids any dependency on Python, Node.js, or other
# external tools at generation time.
#
# The awk script matches the most common class selector forms:
#   .className {          → extract "className"
#   .className,           → extract "className" (multi-selector)
#   .className:hover {    → extract "className" (pseudo-class)
#   .className.another {  → extract "className" only (first segment)
#
# Composing (composes: other from ...) is not extracted since it is not a
# locally-defined class.
#
# The awk script:
#   1. Finds all occurrences of .[ident] in the input using a while/match loop.
#   2. De-duplicates class names while preserving first-occurrence order.
#   3. Writes the .d.ts header, one line per class, then the footer.

_EXTRACT_CLASSES_CMD = r"""
css_in="$1"
dts_out="$2"

awk '
BEGIN {
    n = 0
    in_comment = 0
}
/\/\*/ { in_comment = 1 }
/\*\// { in_comment = 0; next }
in_comment { next }
/^[[:space:]]*composes:/ { next }
{
    line = $0
    while (match(line, /\.[a-zA-Z_][a-zA-Z0-9_-]*/)) {
        cls = substr(line, RSTART + 1, RLENGTH - 1)
        if (!(cls in seen)) {
            seen[cls] = 1
            order[n++] = cls
        }
        line = substr(line, RSTART + RLENGTH)
    }
}
END {
    print "declare const styles: {"
    for (i = 0; i < n; i++) {
        print "  readonly " order[i] ": string;"
    }
    print "};"
    print "export default styles;"
}
' "$css_in" > "$dts_out"
"""

def _css_module_impl(ctx):
    css_files = ctx.files.srcs

    dts_outputs = []
    for css_file in css_files:
        # Emit the .d.ts next to the .css source.
        # The module.css.d.ts name is important: TypeScript requires the
        # declaration file to be named <source>.d.ts when
        # allowArbitraryExtensions is enabled.
        dts = ctx.actions.declare_file(css_file.basename + ".d.ts", sibling = css_file)

        ctx.actions.run_shell(
            inputs = [css_file],
            outputs = [dts],
            command = _EXTRACT_CLASSES_CMD,
            arguments = [css_file.path, dts.path],
            mnemonic = "CssModuleDts",
            progress_message = "CssModuleDts %{label}",
        )
        dts_outputs.append(dts)

    # Build transitive depsets from any css_module deps.
    transitive_css_sets = []
    transitive_dts_sets = []
    for dep in ctx.attr.deps:
        if CssModuleInfo in dep:
            transitive_css_sets.append(dep[CssModuleInfo].transitive_css_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)

    direct_css = depset(css_files)
    transitive_css = depset(css_files, transitive = transitive_css_sets, order = "postorder")
    direct_dts = depset(dts_outputs)
    transitive_dts = depset(dts_outputs, transitive = transitive_dts_sets, order = "postorder")

    return [
        DefaultInfo(files = depset(css_files + dts_outputs)),
        CssModuleInfo(
            css_files = direct_css,
            transitive_css_files = transitive_css,
        ),
        # Expose the generated .d.ts files through TsDeclarationInfo so that
        # ts_compile can pick them up as declaration inputs for type-checking.
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            type_roots = depset([]),
        ),
    ]

css_module = rule(
    implementation = _css_module_impl,
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
    },
    doc = """Processes CSS Module files and generates typed TypeScript declarations.

A css_module target provides CssModuleInfo and TsDeclarationInfo (with
generated .module.css.d.ts typed declarations) so that:

  1. TypeScript accepts 'import styles from \"./Button.module.css\"' and
     provides typed access to class names (e.g. styles.container).
  2. ts_compile targets can declare a CSS Module dependency without failing.
  3. The .module.css files are passed through to the bundler (Vite handles
     CSS Modules natively, applying local scoping and class name mangling).

The generated .d.ts maps each class name found in the CSS to a string:

    declare const styles: {
      readonly container: string;
      readonly button: string;
    };
    export default styles;

Class names are extracted via regex — this handles the common cases but does
not parse @import, @media blocks, or :global() selectors specially.

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
