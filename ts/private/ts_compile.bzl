"""Core TypeScript compilation rule using oxc-bazel.

ts_compile transforms .ts/.tsx source files into .js + .js.map + .d.ts outputs
using the oxc-bazel CLI as a Bazel action.

The .d.ts output is the compilation boundary artifact: downstream targets
depend only on .d.ts files, so Bazel's content-based caching means that if a
dep's .d.ts doesn't change (e.g. because an internal implementation detail
changed but the public API did not), dependents are not recompiled.

When a tsgo toolchain is available, ts_compile also runs type-checking as a
Bazel validation action in the _validation output group. Validation actions
run unconditionally during `bazel build` but do NOT block downstream
compilation.
"""

load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsDeclarationInfo")
load("//ts/private:toolchain.bzl", "OXC_TOOLCHAIN_TYPE", "TSGO_TOOLCHAIN_TYPE", "get_oxc_toolchain")

# ─── Helpers ──────────────────────────────────────────────────────────────────

def _is_ts_source(f):
    """Returns True if the file is a TypeScript source file."""
    return f.extension in ("ts", "tsx")

def _is_dts_source(f):
    """Returns True if the file is an ambient declaration file (.d.ts)."""
    return f.basename.endswith(".d.ts")

def _package_relative_stem(f, pkg):
    """Returns the package-relative path with TypeScript extension stripped."""
    p = f.short_path
    if pkg and p.startswith(pkg + "/"):
        p = p[len(pkg) + 1:]
    for ext in (".tsx", ".ts"):
        if p.endswith(ext):
            return p[:-len(ext)]
    return p

def _relative_path(from_dir, to_dir):
    """Computes a relative path from from_dir to to_dir.

    Both arguments are /-separated directory paths. Returns a string like
    "../../other/pkg" or "." when from_dir == to_dir.
    """
    from_parts = [p for p in from_dir.split("/") if p]
    to_parts = [p for p in to_dir.split("/") if p]
    common_len = 0
    for i in range(min(len(from_parts), len(to_parts))):
        if from_parts[i] == to_parts[i]:
            common_len += 1
        else:
            break
    up_parts = [".."] * (len(from_parts) - common_len)
    down_parts = to_parts[common_len:]
    result = up_parts + down_parts
    return "/".join(result) if result else "."

# ─── Tsconfig generation for validation action ───────────────────────────────

def _generate_tsconfig(ctx, srcs, target, jsx_mode, npm_pkg_dirs = None, type_roots = None, path_aliases = None):
    """Generates a tsconfig.json for tsgo type-checking.

    Uses ctx.bin_dir.path for stable path computation independent of the
    Bazel output configuration (fastbuild, opt, etc.).

    Args:
        ctx:          Rule context.
        srcs:         Source files to include.
        target:       ECMAScript target string.
        jsx_mode:     JSX mode string.
        npm_pkg_dirs: Optional list of (package_name, package_dir_path) pairs
                      for npm packages that need paths mappings so tsgo can
                      resolve bare specifier imports (e.g. "import from 'zod'").
        type_roots:   Optional list of .d.ts files from @types/* packages, used
                      to derive typeRoots directories for tsgo so that ambient
                      type packages (@types/react, @types/node, etc.) are found.
        path_aliases: Optional dict mapping path alias prefixes (e.g. "@/") to
                      workspace-relative directory paths (e.g. "src/"). These
                      are merged into the generated tsconfig paths section so
                      that tsgo can resolve source-level path aliases like
                      `import { Button } from "@/components"`.
    """
    tsconfig = ctx.actions.declare_file("{}.tsconfig.json".format(ctx.label.name))
    tsconfig_dir = tsconfig.dirname

    # rootDirs bridges the source tree (execroot root) and the output tree
    # (bin dir) so tsgo can resolve dep .d.ts files via the same relative
    # paths used for source imports.
    execroot_rel = _relative_path(tsconfig_dir, "")
    bin_dir_rel = _relative_path(tsconfig_dir, ctx.bin_dir.path)

    # include: source files relative to tsconfig location.
    include_items = []
    for src in srcs:
        rel = _relative_path(tsconfig_dir, src.dirname) if src.dirname else "."
        include_items.append('    "{}/{}"'.format(rel, src.basename))

    include_json = "[\n{}\n  ]".format(",\n".join(include_items)) if include_items else "[]"

    ts_target = target if target else "ES2022"
    jsx_entry = ',\n    "jsx": "{}"'.format(jsx_mode) if jsx_mode else ""

    # Build paths entries for npm packages so tsgo can resolve bare module
    # specifiers like `import { z } from "zod"` to the extracted package dir.
    #
    # npm_pkg_dirs entries: (pkg_name, path, is_file)
    # When is_file is True, path is a .d.ts file path (not a directory).
    # In this case we emit: "pkg": ["path/to/index.d.ts"]
    # When is_file is False, path is a directory.
    # In this case we emit: "pkg": ["dir"] and "pkg/*": ["dir/*"]
    path_items = []

    # First: source-level path aliases (e.g. "@/" → "src/") so tsgo can
    # resolve user-defined path aliases like `import { Button } from "@/components"`.
    # These entries use execroot-relative paths (relative to the tsconfig location
    # which itself lives in the bin dir).
    if path_aliases:
        for alias_key, alias_dir in path_aliases.items():
            # alias_dir is workspace-relative (e.g. "src/"), so we resolve it
            # relative to the tsconfig output directory.
            # Strip trailing slash for path computation, then re-add for the
            # wildcard variant so TypeScript sees both exact and sub-path forms.
            dir_no_slash = alias_dir[:-1] if alias_dir.endswith("/") else alias_dir
            rel_dir = _relative_path(tsconfig_dir, dir_no_slash) if dir_no_slash else _relative_path(tsconfig_dir, "")

            # Emit both the exact alias and the wildcard form.
            # For "@/" we emit: "@/*": ["./<rel>/*"] and "@": ["./<rel>"]
            # This covers both `import "@/index"` and `import "@/components/Button"`.
            if alias_key.endswith("/"):
                # Wildcard alias (e.g. "@/"): emit both bare and glob forms.
                alias_no_slash = alias_key[:-1]
                path_items.append('      "{}*": ["{}/*"]'.format(alias_key, rel_dir))
                path_items.append('      "{}": ["{}"]'.format(alias_no_slash, rel_dir))
            else:
                # Exact alias (e.g. "@utils"): emit exact and sub-path wildcard.
                path_items.append('      "{}": ["{}"]'.format(alias_key, rel_dir))
                path_items.append('      "{}/*": ["{}/*"]'.format(alias_key, rel_dir))

    # Second: npm package paths so tsgo can resolve bare specifier imports.
    if npm_pkg_dirs:
        for entry in npm_pkg_dirs:
            if len(entry) == 3:
                pkg_name, path, is_file = entry[0], entry[1], entry[2]
            else:
                pkg_name, path = entry[0], entry[1]
                is_file = False
            if is_file:
                rel_path = _relative_path(tsconfig_dir, path[:path.rfind("/")] if "/" in path else "") + "/" + path.split("/")[-1]
                path_items.append(
                    '      "{}": ["{}"]'.format(pkg_name, rel_path),
                )
                # Also add directory-wildcard for sub-path imports.
                rel_dir = _relative_path(tsconfig_dir, path[:path.rfind("/")] if "/" in path else path)
                path_items.append(
                    '      "{}/*": ["{}/*"]'.format(pkg_name, rel_dir),
                )
            else:
                rel_dir = _relative_path(tsconfig_dir, path)
                # Map the bare specifier to both the package root (for
                # package.json#types resolution) and the common index.d.ts pattern.
                path_items.append(
                    '      "{}": ["{}"]'.format(pkg_name, rel_dir),
                )
                path_items.append(
                    '      "{}/*": ["{}/*"]'.format(pkg_name, rel_dir),
                )

    paths_entry = ""
    if path_items:
        paths_entry = ',\n    "paths": {{\n{}\n    }}'.format(",\n".join(path_items))

    # Build typeRoots entries from collected @types/* package directories.
    # typeRoots tells TypeScript where to find ambient type packages.
    # Each @types/* package lives in its own directory; typeRoots needs the
    # *parent* of those directories (i.e., the directory that contains
    # @types/react, @types/react-dom, etc. as subdirectories).
    # Since we have the .d.ts files from each @types package, we derive the
    # parent by going two levels up: .d.ts → package_dir → parent (= @types/).
    # We deduplicate since multiple @types/* packages share the same parent.
    type_roots_entry = ""
    if type_roots:
        seen_roots = {}
        for dts_file in type_roots:
            # dts_file.dirname = ".../types_react__19_1_6"  (the @types/react dir)
            # dts_file.dirname's parent  = the @npm repo root
            # We need the parent of the package dir to use as typeRoots.
            pkg_dir = dts_file.dirname
            # Navigate up two levels: package_dir → parent
            parent_dir = pkg_dir[:pkg_dir.rfind("/")] if "/" in pkg_dir else pkg_dir
            if parent_dir not in seen_roots:
                seen_roots[parent_dir] = True

        if seen_roots:
            root_items = []
            for root_dir in seen_roots:
                rel_dir = _relative_path(tsconfig_dir, root_dir)
                root_items.append('      "{}"'.format(rel_dir))
            type_roots_entry = ',\n    "typeRoots": [\n{}\n    ]'.format(",\n".join(root_items))

    tsconfig_content = """\
{{
  "compilerOptions": {{
    "strict": true,
    "isolatedDeclarations": true,
    "declaration": true,
    "emitDeclarationOnly": true,
    "sourceMap": true,
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "target": "{target}",
    "skipLibCheck": true,
    "esModuleInterop": true,
    "allowArbitraryExtensions": true,
    "rootDirs": ["{execroot_rel}", "{bin_dir_rel}"]{jsx_entry}{paths_entry}{type_roots_entry}
  }},
  "include": {include}
}}
""".format(
        target = ts_target,
        jsx_entry = jsx_entry,
        paths_entry = paths_entry,
        type_roots_entry = type_roots_entry,
        include = include_json,
        execroot_rel = execroot_rel,
        bin_dir_rel = bin_dir_rel,
    )

    ctx.actions.write(output = tsconfig, content = tsconfig_content)
    return tsconfig

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_compile_impl(ctx):
    oxc = get_oxc_toolchain(ctx)

    # Separate .ts/.tsx sources from pre-existing .d.ts declaration inputs.
    compile_srcs = []
    passthrough_dts = []
    for f in ctx.files.srcs:
        if _is_dts_source(f):
            passthrough_dts.append(f)
        elif _is_ts_source(f):
            compile_srcs.append(f)
        else:
            fail(
                "ts_compile: srcs must contain only .ts, .tsx, or .d.ts files; " +
                "got '{}' (extension: .{}).\n".format(f.short_path, f.extension) +
                "Remove this file from srcs, or if you need to pass through assets " +
                "use a filegroup or a dedicated rule for that file type.\n" +
                "Did you mean to add it to a different attribute?",
            )

    # Collect transitive deps.
    transitive_dts_sets = []
    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_css_sets = []
    transitive_css_module_sets = []
    transitive_asset_sets = []
    # npm_pkg_dirs: list of (package_name, package_dir_path) for tsconfig paths.
    # We collect ALL transitive npm deps so that tsgo can resolve bare module
    # specifiers in transitively-imported .d.ts files (e.g. vitest's index.d.ts
    # imports from @vitest/runner which must be in the tsconfig paths).
    #
    # Pass 1: collect ALL transitive package dirs from all direct npm deps.
    # This builds a complete map of pkg_name → dir_path that covers both direct
    # and transitive packages. Materializing transitive_deps is O(transitive npm
    # packages) which is bounded (typically tens to low hundreds of packages).
    #
    # We separate this into two passes:
    #  Pass 1: collect all transitive package infos (name → dir_path).
    #  Pass 2: for @types/* packages, find which runtime package they type-annotate
    #           and override the runtime package's dir with the @types dir.
    # This avoids the bug where transitive type_roots from unrelated deps pollute
    # the mapping (e.g. vitest's transitive @types/estree dep being used for vitest).
    # type_root_files: .d.ts files from @types/* packages that provide ambient
    # type declarations for runtime packages (e.g. @types/react for react).
    # We collect these so _generate_tsconfig can add a typeRoots entry to the
    # tsconfig, enabling tsgo to resolve bare module specifiers like 'react'.
    type_root_files = []
    # transitive_package_dir_sets: depset of package.json Files from all
    # direct npm deps, used as inputs to the tsgo validation action so that
    # moduleResolution:"Bundler" can read exports/types fields.
    transitive_package_dir_sets = []

    # Step 1a: collect ALL package info entries (direct + transitive) into a map.
    # pkg_info_map: pkg_name → NpmPackageInfo (first seen wins for dedup).
    pkg_info_map = {}

    for dep in ctx.attr.deps:
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            # Collect type_roots files from @types/* packages that are DIRECT
            # deps (not transitively accumulated from random dep subtrees).
            # We only want files from packages whose package_name starts with
            # "@types/", which are the ambient declaration packages.
            if NpmPackageInfo in dep:
                direct_npm_info = dep[NpmPackageInfo]
                if direct_npm_info.package_name.startswith("@types/"):
                    type_root_files.extend(
                        dep[TsDeclarationInfo].type_roots.to_list(),
                    )
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_js_map_sets.append(dep[JsInfo].transitive_js_map_files)
        if CssInfo in dep:
            transitive_css_sets.append(dep[CssInfo].transitive_css_files)
        if CssModuleInfo in dep:
            transitive_css_module_sets.append(dep[CssModuleInfo].transitive_css_files)
        if AssetInfo in dep:
            transitive_asset_sets.append(dep[AssetInfo].transitive_asset_files)
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]

            # Add the direct dep itself.
            pkg_name = npm_info.package_name
            if pkg_name not in pkg_info_map and npm_info.package_dir:
                pkg_info_map[pkg_name] = npm_info

            # Add ALL transitive deps for full coverage in tsconfig paths.
            for transitive_info in npm_info.transitive_deps.to_list():
                trans_name = transitive_info.package_name
                if trans_name not in pkg_info_map and transitive_info.package_dir:
                    pkg_info_map[trans_name] = transitive_info

                # Collect type_roots from @types/* transitive deps for typeRoots.
                if trans_name.startswith("@types/"):
                    type_root_files.extend(
                        transitive_info.declaration_files.to_list(),
                    )

            # Collect transitive package.json files as a depset (no to_list).
            transitive_package_dir_sets.append(npm_info.transitive_package_dirs)

    # Step 1b: build a map from runtime package name → @types package dir.
    # When a package like 'react' has a separate @types/react package, TypeScript
    # must resolve 'react' to the @types/react directory (since react itself ships
    # no .d.ts files).  We detect this pairing by looking at the direct deps:
    # for each direct npm dep, check if its TsDeclarationInfo.declaration_files
    # contains files from a different directory than the runtime package dir.
    # That different directory is the @types/* package dir.
    types_override = {}  # pkg_name → @types_dir (when a types dep is paired)
    for dep in ctx.attr.deps:
        if NpmPackageInfo not in dep or TsDeclarationInfo not in dep:
            continue
        npm_info = dep[NpmPackageInfo]
        pkg_name = npm_info.package_name
        if pkg_name.startswith("@types/"):
            continue  # @types/* packages don't need an override
        runtime_pkg_dir = npm_info.package_dir.dirname
        # Check declaration_files: if any file lives outside the runtime package
        # dir, it must be from the paired @types/* package.
        for dts_file in dep[TsDeclarationInfo].declaration_files.to_list():
            if not dts_file.path.startswith(runtime_pkg_dir):
                # This file is from a @types/* package dir.
                types_override[pkg_name] = dts_file.dirname
                break

    # Step 1c: build npm_pkg_dirs from pkg_info_map using types_override.
    # npm_pkg_dirs entries: (pkg_name, pkg_dir_or_file_path, is_file)
    #   When is_file is True, pkg_dir_or_file_path points directly to a .d.ts file
    #   (from exports_types_file). This generates a more precise paths entry like:
    #     "pkg": ["path/to/index.d.ts"]
    #   rather than:
    #     "pkg": ["path/to/pkg/dir"]
    npm_pkg_dirs = []
    for pkg_name, npm_info in pkg_info_map.items():
        pkg_dir = npm_info.package_dir.dirname

        # Override with @types/* dir when the runtime package has separate types.
        if pkg_name in types_override:
            pkg_dir = types_override[pkg_name]
            npm_pkg_dirs.append((pkg_name, pkg_dir, False))
        elif npm_info.exports_types_file:
            # Package has conditional exports with a 'types' entry.
            # Point directly at the .d.ts file for precise resolution.
            npm_pkg_dirs.append((pkg_name, npm_info.exports_types_file.path, True))
        else:
            npm_pkg_dirs.append((pkg_name, pkg_dir, False))

    dep_dts_depset = depset(transitive = transitive_dts_sets, order = "postorder")

    # Declare outputs: one .js, .js.map, .d.ts per source file.
    pkg = ctx.label.package
    js_outputs = []
    js_map_outputs = []
    dts_outputs = []
    for src in compile_srcs:
        stem = _package_relative_stem(src, pkg)
        js_outputs.append(ctx.actions.declare_file(stem + ".js"))
        js_map_outputs.append(ctx.actions.declare_file(stem + ".js.map"))
        dts_outputs.append(ctx.actions.declare_file(stem + ".d.ts"))

    all_outputs = js_outputs + js_map_outputs + dts_outputs

    # ── Compile action ────────────────────────────────────────────────────
    if compile_srcs:
        args = ctx.actions.args()
        args.add("--files")
        args.add_all(compile_srcs)
        args.add("--out-dir", js_outputs[0].dirname)

        # Determine the correct strip-dir-prefix for the source files.
        #
        # oxc uses --strip-dir-prefix to compute the relative part of each
        # input path when building the output path inside --out-dir.
        #
        # For source files: the strip prefix is the common directory of all
        # input files relative to the exec root. For a ts_compile target in
        # package "tests/foo" this equals the package path (e.g. "tests/foo").
        # For targets in the root package (//) with files in a subdirectory
        # (e.g. "app/root.tsx"), the strip prefix is the common dirname
        # (e.g. "app") — NOT the empty package path.
        #
        # We use compile_srcs[0].dirname for all cases:
        #   - Source files in non-root packages: dirname == pkg ✓
        #   - Source files in root package in a subdir: dirname == subdir ✓
        #   - Source files in root package in root dir: dirname == "" ✓
        #   - Generated files: dirname includes bazel-out prefix ✓
        #
        # Note: this assumes all srcs share the same common directory, i.e.
        # all source files live directly in one directory (no mixing of
        # top-level and subdirectory files in the same ts_compile target).
        # Split targets by directory when files span multiple levels.
        strip_prefix = compile_srcs[0].dirname
        if strip_prefix:
            args.add("--strip-dir-prefix", strip_prefix)

        args.add("--target", ctx.attr.target)
        if ctx.attr.jsx_mode:
            args.add("--jsx", ctx.attr.jsx_mode)
        args.add("--source-map")
        args.add("--declaration")
        if ctx.attr.isolated_declarations:
            args.add("--isolated-declarations")

        ctx.actions.run(
            inputs = depset(compile_srcs, transitive = [dep_dts_depset]),
            outputs = all_outputs,
            executable = oxc.oxc_binary,
            arguments = [args],
            mnemonic = "OxcCompile",
            progress_message = "OxcCompile %{label}",
        )

    # ── Validation action: type-checking with tsgo ────────────────────────
    validation_outputs = []
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if tsgo_toolchain_info and ctx.attr.enable_check and compile_srcs:
        tsgo = tsgo_toolchain_info.tsgo_info

        # Include both .ts/.tsx sources and ambient .d.ts files in the
        # tsconfig — ambient declarations provide type context for checking.
        check_srcs = compile_srcs + passthrough_dts
        tsconfig = _generate_tsconfig(
            ctx = ctx,
            srcs = check_srcs,
            target = ctx.attr.target,
            jsx_mode = ctx.attr.jsx_mode,
            npm_pkg_dirs = npm_pkg_dirs if npm_pkg_dirs else None,
            type_roots = type_root_files if type_root_files else None,
            path_aliases = ctx.attr.path_aliases if ctx.attr.path_aliases else None,
        )

        # Build the depset of transitive npm package.json files so that
        # moduleResolution:"Bundler" can read exports/types fields from each
        # package. This must be computed before the action is registered.
        npm_pkg_dirs_depset = depset(transitive = transitive_package_dir_sets)

        stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
        ctx.actions.run_shell(
            inputs = depset(
                check_srcs + [tsconfig, tsgo.tsgo_binary],
                transitive = [dep_dts_depset, npm_pkg_dirs_depset],
            ),
            outputs = [stamp],
            command = '"{tsgo}" --project "{tsconfig}" --noEmit && /bin/touch "{stamp}"'.format(
                tsgo = tsgo.tsgo_binary.path,
                tsconfig = tsconfig.path,
                stamp = stamp.path,
            ),
            env = {"PATH": "/bin:/usr/bin"},
            mnemonic = "TsgoCheck",
            progress_message = "TsgoCheck %{label}",
        )
        validation_outputs.append(stamp)

    # ── Build providers ───────────────────────────────────────────────────
    direct_dts = depset(dts_outputs + passthrough_dts, order = "postorder")
    direct_js = depset(js_outputs, order = "postorder")
    direct_js_map = depset(js_map_outputs, order = "postorder")

    transitive_dts = depset(
        dts_outputs + passthrough_dts,
        transitive = transitive_dts_sets,
        order = "postorder",
    )
    transitive_js = depset(
        js_outputs,
        transitive = transitive_js_sets,
        order = "postorder",
    )
    transitive_js_map = depset(
        js_map_outputs,
        transitive = transitive_js_map_sets,
        order = "postorder",
    )

    # Build the transitive CSS depset for CssInfo propagation.
    transitive_css = depset(
        transitive = transitive_css_sets,
        order = "postorder",
    )

    # Build transitive CSS modules depset.
    transitive_css_modules = depset(
        transitive = transitive_css_module_sets,
        order = "postorder",
    )

    # Build transitive assets depset.
    transitive_assets = depset(
        transitive = transitive_asset_sets,
        order = "postorder",
    )

    type_roots_sets = []
    for dep in ctx.attr.deps:
        if TsDeclarationInfo in dep:
            type_roots_sets.append(dep[TsDeclarationInfo].type_roots)

    # Include transitive CSS, CSS module, and asset files in DefaultInfo so
    # bundlers and tests can access them via the runfiles / output tree.
    providers = [
        DefaultInfo(files = depset(
            all_outputs + passthrough_dts,
            transitive = [transitive_css, transitive_css_modules, transitive_assets],
        )),
        JsInfo(
            js_files = direct_js,
            js_map_files = direct_js_map,
            transitive_js_files = transitive_js,
            transitive_js_map_files = transitive_js_map,
        ),
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            type_roots = depset(transitive = type_roots_sets),
        ),
    ]

    # Always propagate CssInfo so ts_compile targets can be used as CSS deps.
    providers.append(CssInfo(
        css_files = depset(transitive = transitive_css_sets),
        transitive_css_files = transitive_css,
    ))

    # Propagate CssModuleInfo so ts_compile targets can carry CSS Module deps.
    providers.append(CssModuleInfo(
        css_files = depset(transitive = transitive_css_module_sets),
        transitive_css_files = transitive_css_modules,
    ))

    # Propagate AssetInfo so ts_compile targets can carry asset deps.
    providers.append(AssetInfo(
        asset_files = depset(transitive = transitive_asset_sets),
        transitive_asset_files = transitive_assets,
    ))

    if validation_outputs:
        providers.append(OutputGroupInfo(_validation = depset(validation_outputs)))

    return providers

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_compile = rule(
    implementation = _ts_compile_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "TypeScript source files (.ts, .tsx) and ambient declarations (.d.ts) to compile.",
            allow_files = [".ts", ".tsx", ".d.ts"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Other ts_compile, ts_npm_package, css_library, css_module, asset_library, or json_library targets that this target depends on.",
            providers = [[TsDeclarationInfo, JsInfo], [TsDeclarationInfo], [CssInfo], [CssModuleInfo], [AssetInfo]],
        ),
        "target": attr.string(
            doc = "ECMAScript target version passed to oxc-bazel (e.g. 'es2022').",
            default = "es2022",
        ),
        "jsx_mode": attr.string(
            doc = "JSX transform mode: 'react-jsx', 'react', 'preserve'. Empty disables JSX.",
            default = "react-jsx",
        ),
        "isolated_declarations": attr.bool(
            doc = "Whether to enable isolated declarations mode for faster .d.ts emit.",
            default = True,
        ),
        "enable_check": attr.bool(
            doc = "Run tsgo type-checking as a validation action (requires tsgo toolchain).",
            default = True,
        ),
        "path_aliases": attr.string_dict(
            doc = """Source-level path alias mappings to inject into the tsgo validation tsconfig.

Maps alias prefixes (as they appear in import statements) to workspace-relative
directory paths. These are added to the compilerOptions.paths section of the
generated tsconfig so that tsgo can resolve path aliases that are defined in
the project's tsconfig.json but are not automatically visible to the Bazel
build's generated tsconfig.

Examples:
    # tsconfig.json has: {"@/*": ["./src/*"]}
    path_aliases = {"@/": "src/"}

    # Multiple aliases
    path_aliases = {
        "@/": "src/",
        "@components/": "src/components/",
        "@utils": "src/utils",
    }

Gazelle auto-populates this attr when it reads compilerOptions.paths from
the project tsconfig.json. Users can also set it manually when Gazelle is
not in use or when the alias mapping differs from the tsconfig paths.
""",
        ),
    },
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Compiles TypeScript source files using oxc-bazel.

Produces one .js, .js.map, and .d.ts output per .ts/.tsx input file.
The .d.ts outputs are the compilation boundary: downstream ts_compile targets
only depend on the .d.ts files, enabling fine-grained Bazel caching.

When a tsgo toolchain is registered, type-checking runs as a validation
action in the _validation output group — it executes during `bazel build`
but does not block downstream targets.
""",
)
