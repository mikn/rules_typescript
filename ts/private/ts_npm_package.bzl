"""Rule for individual npm package targets.

ts_npm_package is the target produced by npm_translate_lock for each npm
package.  It wraps a downloaded npm package directory and exposes:
  - JsInfo          (.js files in the package)
  - TsDeclarationInfo (.d.ts files, either bundled or from a paired @types/* pkg)
  - NpmPackageInfo  (package metadata + transitive dep graph)

Key behaviour:
  - @types/* packages are paired with their untyped counterparts: when
    npm_translate_lock creates a `react` target it also attaches the
    declarations from `@types/react` if present.
  - The `package_dir` field points at the root of the extracted package.
  - Transitive deps are expressed as NpmPackageInfo.transitive_deps.
"""

load("//ts/private:providers.bzl", "JsInfo", "NpmPackageInfo", "TsDeclarationInfo")

# ─── Helpers ──────────────────────────────────────────────────────────────────

def _is_dts(f):
    """Returns True for .d.ts and .d.mts/.d.cts files."""
    return f.basename.endswith(".d.ts") or f.basename.endswith(".d.mts") or f.basename.endswith(".d.cts")

def _is_js(f):
    """Returns True for .js/.mjs/.cjs files (excludes .d.ts which has extension 'ts')."""
    return f.extension in ("js", "mjs", "cjs") and not _is_dts(f)

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_npm_package_impl(ctx):
    package_dir = ctx.file.package_dir

    # All files in this package directory.
    all_files = ctx.files.package_files

    js_files = [f for f in all_files if _is_js(f)]
    dts_files = [f for f in all_files if _is_dts(f)]

    # Also collect declarations from an explicitly linked @types dep.
    # Do NOT call .to_list() — use depset transitive to avoid materialization.
    types_dts_direct = depset()
    types_roots_sets = []
    if ctx.attr.types_dep:
        types_info = ctx.attr.types_dep
        if TsDeclarationInfo in types_info:
            # Pull the direct declaration_files (not full transitive) of the
            # @types package as the direct contribution of this npm target.
            types_dts_direct = types_info[TsDeclarationInfo].declaration_files
            types_roots_sets.append(types_info[TsDeclarationInfo].type_roots)

    # Collect transitive data from npm dep targets.
    transitive_js_sets = [depset(js_files)]
    # Start the dts transitive set with this package's own files plus the
    # types dep's full transitive declarations (without materializing them).
    transitive_dts_sets = [depset(dts_files), types_dts_direct]
    if ctx.attr.types_dep and TsDeclarationInfo in ctx.attr.types_dep:
        transitive_dts_sets.append(ctx.attr.types_dep[TsDeclarationInfo].transitive_declaration_files)

    # direct_npm_dep_infos: the NpmPackageInfo instances of direct deps
    # (to be included as direct items in the transitive_deps depset).
    direct_npm_dep_infos = []
    transitive_npm_dep_sets = []
    transitive_pkg_dir_sets = [depset([package_dir])]

    for dep in ctx.attr.deps:
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            types_roots_sets.append(dep[TsDeclarationInfo].type_roots)
        if NpmPackageInfo in dep:
            direct_npm_dep_infos.append(dep[NpmPackageInfo])
            transitive_npm_dep_sets.append(dep[NpmPackageInfo].transitive_deps)
            transitive_pkg_dir_sets.append(dep[NpmPackageInfo].transitive_package_dirs)

    # For @types/* packages, the type_roots field should contain .d.ts files
    # from this package so downstream consumers can resolve types.  TypeScript
    # typeRoots requires directories, but Bazel File objects are always files.
    # Instead we populate type_roots with the .d.ts files themselves — the
    # consuming rule (ts_config_gen) uses the files' dirname to derive the
    # typeRoots entry.  This avoids the mismatch of passing a package.json
    # file where a directory is expected.
    type_roots = []
    if ctx.attr.is_types_package:
        type_roots = dts_files

    # Direct declaration files = this package's own .d.ts + the types dep's
    # direct declarations (not the full transitive closure).
    direct_decls = depset(dts_files, transitive = [types_dts_direct])

    return [
        DefaultInfo(
            files = depset(all_files),
        ),
        JsInfo(
            js_files = depset(js_files),
            js_map_files = depset([]),
            transitive_js_files = depset(transitive = transitive_js_sets, order = "postorder"),
            transitive_js_map_files = depset([]),
        ),
        TsDeclarationInfo(
            declaration_files = direct_decls,
            transitive_declaration_files = depset(
                transitive = transitive_dts_sets,
                order = "postorder",
            ),
            type_roots = depset(
                type_roots,
                transitive = types_roots_sets,
            ),
        ),
        NpmPackageInfo(
            package_name = ctx.attr.package_name,
            package_version = ctx.attr.package_version,
            package_dir = package_dir,
            all_files = depset(all_files),
            js_files = depset(js_files),
            declaration_files = direct_decls,
            transitive_deps = depset(
                # Include direct dep NpmPackageInfo instances so that
                # consumers can find them in the flattened transitive list.
                direct_npm_dep_infos,
                transitive = transitive_npm_dep_sets,
                order = "postorder",
            ),
            transitive_package_dirs = depset(
                transitive = transitive_pkg_dir_sets,
                order = "postorder",
            ),
            exports_types_file = ctx.file.exports_types,
        ),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_npm_package = rule(
    implementation = _ts_npm_package_impl,
    attrs = {
        "package_name": attr.string(
            doc = "The npm package name, e.g. 'react' or '@types/react'.",
            mandatory = True,
        ),
        "package_version": attr.string(
            doc = "The resolved npm package version.",
            mandatory = True,
        ),
        "package_dir": attr.label(
            doc = "The root directory of the extracted npm package.",
            allow_single_file = True,
        ),
        "package_files": attr.label_list(
            doc = "All files in the package directory.",
            allow_files = True,
        ),
        "deps": attr.label_list(
            doc = "Other ts_npm_package targets that this package depends on.",
            providers = [[NpmPackageInfo]],
        ),
        "types_dep": attr.label(
            doc = "The @types/* package that provides declarations for this package (if separate).",
            providers = [[TsDeclarationInfo]],
        ),
        "is_types_package": attr.bool(
            doc = "True if this package is a @types/* declaration package.",
            default = False,
        ),
        "exports_types": attr.label(
            doc = "The specific .d.ts file exposed via exports['.']['types'] in package.json. " +
                  "When set, this file is used as the primary declaration entry point instead " +
                  "of globbing all .d.ts files. Generated by npm_translate_lock from the " +
                  "package.json 'exports' field.",
            allow_single_file = True,
        ),
    },
    doc = """Wraps a downloaded npm package as a Bazel target.

Exposes JsInfo, TsDeclarationInfo, and NpmPackageInfo providers so that
ts_compile targets can depend on npm packages using the same dep mechanism
as first-party TypeScript targets.

@types/* packages paired via types_dep contribute their .d.ts files to the
TsDeclarationInfo of the runtime package.

Example (generated by npm_translate_lock):
    ts_npm_package(
        name = "react",
        package_name = "react",
        package_version = "19.0.0",
        package_dir = ":react_pkg_dir",
        package_files = glob(["**/*"]),
        types_dep = "@npm//@types/react",
    )
""",
)
