"""asset_library rule — collects static assets and generates ambient TypeScript declarations.

Static assets (images, SVGs, fonts) are imported in TypeScript applications as
URL strings:

    import logo from "./logo.svg";       // → string URL at runtime
    import heroImage from "./hero.png";  // → string URL at runtime

TypeScript rejects these imports by default because it cannot resolve the type
of a non-TypeScript file extension.  When 'allowArbitraryExtensions' is enabled,
TypeScript looks for a <filename>.<ext>.d.ts ambient declaration next to the
source file.

This rule:
  1. Generates an ambient .d.ts for each asset file so TypeScript accepts the
     import without errors.
  2. Propagates asset files via AssetInfo so bundlers can copy/hash them.
  3. Exposes declarations via TsDeclarationInfo so ts_compile includes them.

NOTE: JSON files are NOT handled by asset_library. Use json_library instead,
which parses the JSON at build time and generates a fully-typed .d.ts:

    json_library(
        name = "config",
        srcs = ["config.json"],
    )

The generated declarations vary by asset type:
  .svg / .png / .jpg / .gif / .webp → 'declare const asset: string; export default asset;'
  .woff / .woff2 / .ttf / .eot      → 'declare const asset: string; export default asset;'
"""

load("//ts/private:providers.bzl", "AssetInfo", "TsDeclarationInfo")

# ── Ambient declaration content by file type ─────────────────────────────────

_URL_DTS = """\
// Auto-generated asset declaration.
// This file allows TypeScript (allowArbitraryExtensions: true) to accept
// imports of this asset as a URL string.
declare const asset: string;
export default asset;
"""

# Map of extension (without dot) → declaration content.
_DTS_BY_EXT = {
    "svg": _URL_DTS,
    "png": _URL_DTS,
    "jpg": _URL_DTS,
    "jpeg": _URL_DTS,
    "gif": _URL_DTS,
    "webp": _URL_DTS,
    "woff": _URL_DTS,
    "woff2": _URL_DTS,
    "ttf": _URL_DTS,
    "eot": _URL_DTS,
}

def _asset_library_impl(ctx):
    asset_files = ctx.files.srcs

    dts_outputs = []
    for asset_file in asset_files:
        ext = asset_file.extension.lower()
        content = _DTS_BY_EXT.get(ext, _URL_DTS)

        # The .d.ts must be named <basename>.d.ts so that TypeScript resolves
        # it when allowArbitraryExtensions is enabled:
        #   logo.svg  →  logo.svg.d.ts
        dts = ctx.actions.declare_file(asset_file.basename + ".d.ts", sibling = asset_file)
        ctx.actions.write(output = dts, content = content)
        dts_outputs.append(dts)

    # Build transitive depsets from any asset_library deps.
    transitive_asset_sets = []
    transitive_dts_sets = []
    for dep in ctx.attr.deps:
        if AssetInfo in dep:
            transitive_asset_sets.append(dep[AssetInfo].transitive_asset_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)

    direct_assets = depset(asset_files)
    transitive_assets = depset(asset_files, transitive = transitive_asset_sets, order = "postorder")
    direct_dts = depset(dts_outputs)
    transitive_dts = depset(dts_outputs, transitive = transitive_dts_sets, order = "postorder")

    return [
        DefaultInfo(files = depset(asset_files + dts_outputs)),
        AssetInfo(
            asset_files = direct_assets,
            transitive_asset_files = transitive_assets,
        ),
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            type_roots = depset([]),
        ),
    ]

# Accepted file extensions for asset_library.
# NOTE: .json is intentionally excluded. Use json_library for JSON files so
# that TypeScript callers get a fully-typed declaration instead of `unknown`.
_ASSET_EXTENSIONS = [
    ".svg",
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".woff",
    ".woff2",
    ".ttf",
    ".eot",
]

asset_library = rule(
    implementation = _asset_library_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "Static asset source files (images, SVGs, fonts, JSON).",
            allow_files = _ASSET_EXTENSIONS,
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Other asset_library targets this target depends on.",
            providers = [[AssetInfo]],
        ),
    },
    doc = """Collects static asset files and generates ambient TypeScript declarations.

An asset_library target provides AssetInfo and TsDeclarationInfo (with
generated ambient .d.ts declarations) so that:

  1. TypeScript (allowArbitraryExtensions: true) does not error when a .tsx
     or .ts file imports an asset file like './logo.svg'.
  2. ts_compile targets can declare an asset dependency without failing.
  3. The asset files are passed through to the output tree so bundlers
     (e.g. Vite) can copy, hash, and reference them in the bundle.

Supported file types:
  Images:  .svg, .png, .jpg, .jpeg, .gif, .webp
  Fonts:   .woff, .woff2, .ttf, .eot

NOTE: JSON files are handled by json_library (not asset_library). json_library
generates a fully-typed .d.ts by parsing the JSON structure at build time.

Generated declarations:
  Images and fonts → declare const asset: string; export default asset;

Example:
    asset_library(
        name = "icons",
        srcs = ["logo.svg", "close.svg"],
    )

    ts_compile(
        name = "header",
        srcs = ["Header.tsx"],
        deps = [":icons"],
    )
""",
)
