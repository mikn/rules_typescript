"""css_module rule — compiles .module.css files and generates typed .d.ts declarations.

CSS Modules (*.module.css) differ from plain CSS:
  - They are imported with a default import:  import styles from "./Button.module.css"
  - The import value is an object mapping class names to opaque strings.
  - TypeScript needs a typed .d.ts declaration for each .module.css file.

This rule runs postcss-modules -- the library Vite itself bundles -- over each
source and writes the export map it produced to <source>.exports.json. The
.d.ts is generated from that map's keys, so the declared names and the runtime
names are one derivation rather than two that can disagree.

The scoped name in the map comes from a ruleset-owned generator
(ts/private/css/scoped_name.ts): `_<local name>_<sha256 of the stylesheet>`.
It reads no filename, no working directory and no line number, so the same
bytes produce the same name in any sandbox or output base.

The generated .d.ts looks like:
    declare const styles: {
      readonly container: string;
      readonly button: string;
    };
    export default styles;
"""

load("//ts/private:providers.bzl", "CssModuleInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

_LOCALS_CONVENTIONS = ["", "camelCase", "camelCaseOnly", "dashes", "dashesOnly", "all", "none"]
_SCOPE_BEHAVIOURS = ["", "local", "global"]

def _compile_options(ctx):
    options = {}
    if ctx.attr.locals_convention:
        options["localsConvention"] = ctx.attr.locals_convention
    if ctx.attr.scope_behaviour:
        options["scopeBehaviour"] = ctx.attr.scope_behaviour
    if ctx.attr.hash_prefix:
        options["hashPrefix"] = ctx.attr.hash_prefix
    if ctx.attr.export_globals:
        options["exportGlobals"] = True
    return json.encode(options)

def _css_module_impl(ctx):
    js_tool = get_js_tool(ctx)
    compiler = ctx.file._compiler
    options = _compile_options(ctx)

    transitive_css_sets = []
    transitive_dts_sets = []
    global_entry_sets = []
    transitive_exports_sets = []
    for dep in ctx.attr.deps:
        if CssModuleInfo in dep:
            transitive_css_sets.append(dep[CssModuleInfo].transitive_css_files)
            transitive_exports_sets.append(dep[CssModuleInfo].transitive_exports_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            global_entry_sets.append(dep[TsDeclarationInfo].transitive_global_entry_files)

    bin_css_files = []
    dts_outputs = []
    exports_outputs = []

    for css_file in ctx.files.srcs:
        # A copy in bazel-bin, not the source file, for the reason css_library
        # copies too: the importer is the compiled .js beside it, so a relative
        # import resolves in the output tree rather than in the source tree.
        # A generated src is already in bazel-bin, and declaring an output at a
        # path another rule owns is an error rather than a copy.
        if css_file.is_source:
            bin_css = ctx.actions.declare_file(css_file.basename, sibling = css_file)
            ctx.actions.expand_template(
                template = css_file,
                output = bin_css,
                substitutions = {},
            )
        else:
            bin_css = css_file
        bin_css_files.append(bin_css)

        # The <source>.d.ts name is what TypeScript looks for with
        # allowArbitraryExtensions enabled.
        dts = ctx.actions.declare_file(css_file.basename + ".d.ts", sibling = css_file)
        exports_json = ctx.actions.declare_file(css_file.basename + ".exports.json", sibling = css_file)

        # Compiling the bin copy rather than the source is what lets
        # `composes: x from "./other.module.css"` resolve: a dep's copy is the
        # file beside this one in the output tree.
        ctx.actions.run(
            inputs = depset([bin_css, compiler], transitive = transitive_css_sets),
            outputs = [dts, exports_json],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [
                compiler.path,
                bin_css.path,
                exports_json.path,
                dts.path,
                options,
            ],
            mnemonic = "CssModuleDts",
            progress_message = "CssModuleDts %{label}",
        )
        dts_outputs.append(dts)
        exports_outputs.append(exports_json)

    return [
        DefaultInfo(files = depset(bin_css_files + dts_outputs + exports_outputs)),
        CssModuleInfo(
            css_files = depset(bin_css_files),
            transitive_css_files = depset(bin_css_files, transitive = transitive_css_sets, order = "postorder"),
            exports_files = depset(exports_outputs),
            transitive_exports_files = depset(exports_outputs, transitive = transitive_exports_sets, order = "postorder"),
        ),
        # Expose the generated .d.ts files through TsDeclarationInfo so that
        # ts_compile can pick them up as declaration inputs for type-checking.
        TsDeclarationInfo(
            declaration_files = depset(dts_outputs),
            transitive_declaration_files = depset(dts_outputs, transitive = transitive_dts_sets, order = "postorder"),
            global_entry_files = depset(),
            transitive_global_entry_files = depset(transitive = global_entry_sets, order = "postorder"),
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
        "locals_convention": attr.string(
            doc = "postcss-modules `localsConvention`: rewrites the exported keys.",
            values = _LOCALS_CONVENTIONS,
        ),
        "scope_behaviour": attr.string(
            doc = "postcss-modules `scopeBehaviour`: 'local' (the default) or 'global'.",
            values = _SCOPE_BEHAVIOURS,
        ),
        "hash_prefix": attr.string(
            doc = "Salts the content hash in every scoped name produced for these srcs.",
        ),
        "export_globals": attr.bool(
            doc = "postcss-modules `exportGlobals`: also export names :global(...) left unscoped.",
        ),
        "_compiler": attr.label(
            default = Label("//ts/private/css:css_module_compile"),
            allow_single_file = True,
        ),
    },
    doc = """Compiles CSS Module files and generates typed TypeScript declarations.

A css_module target provides CssModuleInfo and TsDeclarationInfo (with
generated .module.css.d.ts typed declarations) so that:

  1. TypeScript accepts 'import styles from \"./Button.module.css\"' and
     provides typed access to class names (e.g. styles.container).
  2. ts_compile targets can declare a CSS Module dependency without failing.
  3. The .module.css files are copied untransformed into bazel-bin beside the
     compiled .js that imports them, which is what lets the bundler resolve
     the relative import.

Each source additionally produces <source>.exports.json, the export map
postcss-modules computed:

    {"container": "_container_7416ac39", "button": "_button_7416ac39"}

The keys are what the .d.ts declares, and the values are the class names a
bundler must emit. Nothing here transforms the stylesheet -- the bundler still
emits the bytes -- but the naming decision is made once, here, and recorded.

Anything that changes the answer is an attribute of this rule rather than of
the bundler's config, because the .d.ts is written from the answer: a
`localsConvention` set on the bundler side would rename the keys the .d.ts
already declared.

The exported keys are postcss-modules': every locally scoped class, id and
@keyframes name, plus @value names, and NOT a name a :global(...) group left
alone. `composes` values name several classes, and one from another file
resolves through `deps`.

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
