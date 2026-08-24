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

load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsConfigInfo", "TsDeclarationInfo")
load("//ts/private:toolchain.bzl", "OXC_TOOLCHAIN_TYPE", "TSGO_TOOLCHAIN_TYPE", "get_oxc_toolchain")

TsModuleInfo = provider(
    doc = """The bare specifier a ts_compile target is importable as.

A first-party package imported as `@scope/pkg` rather than by relative path
needs a paths entry pointing at the .d.ts files Bazel produced for it. Only the
producing target knows where those land under the current configuration, so the
name travels with the target instead of being written into a consumer's
tsconfig by hand.
""",
    fields = {
        "module_name": "string: the bare specifier for this target, or '' if it declared none.",
        "declaration_root": "string: exec-root-relative directory the target's generated .d.ts files land in.",
        "source_root": "string: exec-root-relative package directory, where .d.ts files passed straight through stay.",
        "transitive_modules": "depset of struct(module_name, declaration_root, source_root): this target's modules and its deps'.",
    },
)

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

# ─── Tsconfig generation ─────────────────────────────────────────────────────

# Options whose values encode the sandbox layout or the action's declared
# outputs: a user value here breaks the build rather than configuring it, so
# setting one fails with the mapped text as the pointer to the real knob.
_BAZEL_OWNED_OPTIONS = {
    "baseUrl": "tsgo removed baseUrl (TS5102); use path_aliases, whose values Bazel rewrites",
    "rootDirs": "rootDirs bridges the source tree and the output tree",
    "paths": "use path_aliases for source aliases, or module_name on the target that produces the declarations",
    "outDir": "outDir must be the directory Bazel declared the outputs in",
    "rootDir": "rootDir must be the source directory oxc strips",
    "declarationDir": "declarations must land next to the .js Bazel declared",
    "declaration": "declaration emit follows from the `declarations` attr",
    "emitDeclarationOnly": "declaration emit follows from the `declarations` attr",
    "declarationMap": "Bazel declares no .d.ts.map output",
    "noEmit": "declaration emit follows from the `declarations` attr",
    "noEmitOnError": "a target that fails to check must not leave a declaration on disk",
    "isolatedDeclarations": "isolated declarations follow from declarations = \"oxc\"",
    "composite": "cross-target wiring is Bazel's job, not tsc's",
    "incremental": "Bazel declares no .tsbuildinfo output",
    "tsBuildInfoFile": "Bazel declares no .tsbuildinfo output",
}

# Required by the .d.ts this ruleset generates for css_module, css_library,
# asset_library and json_library deps, whose extensions TypeScript otherwise
# refuses. Beneath the user's options, so an explicit value still wins.
_RULESET_OPTIONS = {
    "allowArbitraryExtensions": True,
}

# Applied only when no `tsconfig` is supplied: with one, the ruleset contributes
# no opinions of its own, so tsgo sees what tsc would.
_ZERO_CONFIG_OPTIONS = {
    "strict": True,
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

# Options whose values are paths a user copies out of a package's own
# tsconfig.json, where they are written relative to that package.
_PACKAGE_RELATIVE_OPTIONS = ["types", "typeRoots"]

def _rebase_package_relative(entry, package_rel):
    """Rewrites a package-relative path so it resolves from the generated config.

    Entries that are not relative paths (npm package names such as "node" or
    "@cloudflare/workers-types") are returned untouched.
    """
    if entry == ".":
        return package_rel
    if entry.startswith("./"):
        return package_rel + "/" + entry[2:]
    if entry.startswith("../"):
        return package_rel + "/" + entry
    return entry

def _generate_tsconfig(
        ctx,
        srcs,
        npm_pkg_dirs = None,
        type_roots = None,
        module_paths = None,
        extends_file = None,
        emit_declarations = False,
        emit_root_dir = None,
        emit_out_dir = None):
    """Generates the tsconfig.json tsgo is invoked with.

    Layered lowest precedence first:

      1. `extends_file` -- the user's own tsconfig.json, referenced (not
         copied), so every relative path inside it still resolves against the
         directory it was written for.
      2. _RULESET_OPTIONS, and the derived typeRoots.
      3. _ZERO_CONFIG_OPTIONS -- only when there is no `extends_file`.
      4. compiler_options_json -- what the user asked for, via the ts_compile
         macro's lib / types / target / jsx / compiler_options arguments.
      5. Bazel-owned options: _BAZEL_OWNED_OPTIONS plus paths and include.

    Layers 2-5 all land in this generated file, so they override the user's
    tsconfig wholesale, per key, exactly as TypeScript's own `extends` does.

    Args:
        ctx:          Rule context.
        srcs:         Source files to type-check (.ts/.tsx plus ambient .d.ts).
        npm_pkg_dirs: (package_name, path, is_file) triples for npm deps.
        type_roots:   .d.ts files from @types/* packages, used to derive
                      typeRoots so ambient type packages are discoverable.
        module_paths: struct(module_name, declaration_root, source_root) from
                      every dep that declared a module_name.
        extends_file: The user's tsconfig.json, or None for zero-config.
        emit_declarations: True when tsgo emits the .d.ts (declarations =
                      "tsgo"), False when it only reports diagnostics.
        emit_root_dir: Exec-root-relative common source directory.
        emit_out_dir: Exec-root-relative directory the .d.ts must land in.
    """
    tsconfig = ctx.actions.declare_file("{}.tsconfig.json".format(ctx.label.name))
    tsconfig_dir = tsconfig.dirname
    package_rel = _relative_path(tsconfig_dir, ctx.label.package)

    opts = {}
    opts.update(_RULESET_OPTIONS)

    # typeRoots: TypeScript wants the directory that *contains* the @types
    # packages, so derive it from each @types .d.ts by going up one level from
    # the package directory.
    if type_roots:
        seen_roots = {}
        for dts_file in type_roots:
            pkg_dir = dts_file.dirname
            parent_dir = pkg_dir[:pkg_dir.rfind("/")] if "/" in pkg_dir else pkg_dir
            seen_roots[parent_dir] = True
        if seen_roots:
            opts["typeRoots"] = [_relative_path(tsconfig_dir, d) for d in seen_roots]

    if not extends_file:
        opts.update(_ZERO_CONFIG_OPTIONS)

    # target and jsx come from the attrs in every mode, including over a
    # tsconfig baseline: oxc transforms with them, and the two compilers
    # disagreeing is worse than deferring to the file.
    opts["target"] = ctx.attr.target
    if ctx.attr.jsx_mode:
        opts["jsx"] = ctx.attr.jsx_mode

    user_opts = {}
    if ctx.attr.compiler_options_json:
        decoded = json.decode(ctx.attr.compiler_options_json)
        if type(decoded) != "dict":
            fail("ts_compile: compiler_options must be a dict, got {}.".format(type(decoded)))
        user_opts = decoded

    for key, reason in _BAZEL_OWNED_OPTIONS.items():
        if key in user_opts:
            fail(
                "ts_compile: compilerOptions.{key} is set by the rule and cannot be overridden -- {reason}.\n".format(
                    key = key,
                    reason = reason,
                ) +
                "Remove \"{}\" from compiler_options on {}.".format(key, ctx.label),
            )

    for key in _PACKAGE_RELATIVE_OPTIONS:
        if key in user_opts:
            if type(user_opts[key]) != "list":
                fail("ts_compile: compilerOptions.{} must be a list of strings on {}.".format(key, ctx.label))
            user_opts[key] = [
                _rebase_package_relative(entry, package_rel)
                for entry in user_opts[key]
            ]

    opts.update(user_opts)

    # ── Bazel-owned: module resolution ────────────────────────────────────
    #
    # paths is one key, so it cannot be half-inherited: everything importable
    # has to be represented here.
    paths = {}

    for alias_key, alias_dir in ctx.attr.path_aliases.items():
        if alias_dir.startswith("bazel-out/") or alias_dir.startswith("bazel-bin/"):
            fail(
                "ts_compile: path_aliases[\"{}\"] on {} points into the output tree ({}).\n".format(
                    alias_key,
                    ctx.label,
                    alias_dir,
                ) +
                "That path embeds the build configuration, so it breaks under -c opt or a " +
                "different exec platform.\nTo import another package by bare specifier, set " +
                "module_name on the target that produces its declarations and depend on it.",
            )
        dir_no_slash = alias_dir[:-1] if alias_dir.endswith("/") else alias_dir
        rel_dir = _relative_path(tsconfig_dir, dir_no_slash)
        if alias_key.endswith("/"):
            paths[alias_key + "*"] = [rel_dir + "/*"]
            paths[alias_key[:-1]] = [rel_dir]
        else:
            paths[alias_key] = [rel_dir]
            paths[alias_key + "/*"] = [rel_dir + "/*"]

    for entry in npm_pkg_dirs or []:
        pkg_name, path, is_file = entry[0], entry[1], entry[2]
        pkg_dir = path[:path.rfind("/")] if "/" in path else ""
        rel_dir = _relative_path(tsconfig_dir, pkg_dir if is_file else path)
        if is_file:
            paths[pkg_name] = [rel_dir + "/" + path.split("/")[-1]]
        else:
            paths[pkg_name] = [rel_dir]
        paths[pkg_name + "/*"] = [rel_dir + "/*"]

    # Last, so a first-party module_name wins over a same-named npm package.
    # Both roots are listed because a module's declarations are either generated
    # or passed through from srcs; TypeScript tries each entry in turn.
    for module in module_paths or []:
        roots = [
            _relative_path(tsconfig_dir, module.declaration_root),
            _relative_path(tsconfig_dir, module.source_root),
        ]
        paths[module.module_name] = [r + "/index.d.ts" for r in roots] + roots
        paths[module.module_name + "/*"] = [r + "/*" for r in roots]

    if paths:
        opts["paths"] = paths

    opts["rootDirs"] = [
        _relative_path(tsconfig_dir, ""),
        _relative_path(tsconfig_dir, ctx.bin_dir.path),
    ]

    # ── Bazel-owned: emit shape ───────────────────────────────────────────
    opts["declaration"] = True
    opts["emitDeclarationOnly"] = True
    opts["declarationMap"] = False
    opts["composite"] = False
    opts["incremental"] = False

    if emit_declarations:
        # noEmit off because a tsconfig that sets it -- the usual shape for a
        # bundler-built package -- would starve the action of its declared
        # outputs; noEmitOnError so a target that fails to check leaves no
        # declaration behind for a consumer to read.
        opts["noEmit"] = False
        opts["noEmitOnError"] = True
        opts["outDir"] = _relative_path(tsconfig_dir, emit_out_dir) if emit_out_dir else "."
        opts["declarationDir"] = opts["outDir"]
        opts["rootDir"] = _relative_path(tsconfig_dir, emit_root_dir) if emit_root_dir else "."
    else:
        # oxc's syntactic emit genuinely requires isolated declarations. In tsgo
        # mode the compiler has the full program and infers the types, so
        # demanding annotations would buy nothing.
        opts["isolatedDeclarations"] = True

    # ── Bazel-owned: the file set ─────────────────────────────────────────

    include = []
    for src in srcs:
        rel = _relative_path(tsconfig_dir, src.dirname) if src.dirname else "."
        include.append(rel + "/" + src.basename)

    config = {}
    if extends_file:
        extends_dir = _relative_path(tsconfig_dir, extends_file.dirname)
        # TypeScript reads an `extends` that is not visibly relative as a node
        # module specifier.
        if not extends_dir.startswith("."):
            extends_dir = "./" + extends_dir
        config["extends"] = extends_dir + "/" + extends_file.basename
    config["compilerOptions"] = opts
    config["include"] = include

    if extends_file:
        # srcs is the only file list. Emptying the inherited ones is safe only
        # alongside `extends`: TS18002 rejects an empty `files` otherwise.
        config["files"] = []
        config["exclude"] = []
        config["references"] = []

    ctx.actions.write(output = tsconfig, content = json.encode_indent(config, indent = "  "))
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

    # module_name deps: every module reachable from here, direct or not, since a
    # bare specifier in a dep's .d.ts has to resolve too.
    module_sets = [
        dep[TsModuleInfo].transitive_modules
        for dep in ctx.attr.deps
        if TsModuleInfo in dep
    ]
    module_paths = depset(transitive = module_sets).to_list()

    # The rest of the user's `extends` chain. Starlark cannot read the tsconfig
    # to follow it, so a ts_config target declares it and we make every file in
    # it an action input.
    tsconfig_chain = []
    if ctx.file.tsconfig:
        tsconfig_chain = [ctx.file.tsconfig]
        if TsConfigInfo in ctx.attr.tsconfig:
            tsconfig_chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()

    # Declare outputs: one .js, .js.map, .d.ts per source file.
    pkg = ctx.label.package
    # Who emits the .d.ts decides what each action is on the hook for.
    #   "oxc":  oxc emits declarations syntactically, which REQUIRES isolated
    #           declarations; tsgo then only reports diagnostics, so checking
    #           stays off the critical path.
    #   "tsgo": oxc transpiles JS only and tsgo emits declarations from the full
    #           program, so no source annotations are required.
    #
    # enable_check = False under "tsgo" means there is no type program at all,
    # and therefore no declarations: an opt-out of types, not of correctness.
    # That is the right shape for terminal targets -- app entry points, dev
    # servers, bundle inputs -- whose types nothing consumes.
    oxc_emits_dts = ctx.attr.declarations == "oxc"
    tsgo_emits_dts = not oxc_emits_dts and ctx.attr.enable_check
    emits_dts = oxc_emits_dts or tsgo_emits_dts

    js_outputs = []
    js_map_outputs = []
    dts_outputs = []
    for src in compile_srcs:
        stem = _package_relative_stem(src, pkg)
        js_outputs.append(ctx.actions.declare_file(stem + ".js"))
        js_map_outputs.append(ctx.actions.declare_file(stem + ".js.map"))
        if emits_dts:
            dts_outputs.append(ctx.actions.declare_file(stem + ".d.ts"))

    oxc_outputs = js_outputs + js_map_outputs + (dts_outputs if oxc_emits_dts else [])
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
        if oxc_emits_dts:
            args.add("--declaration")
            args.add("--isolated-declarations")

        ctx.actions.run(
            inputs = depset(compile_srcs, transitive = [dep_dts_depset]),
            outputs = oxc_outputs,
            executable = oxc.oxc_binary,
            arguments = [args],
            mnemonic = "OxcCompile",
            progress_message = "OxcCompile %{label}",
        )

    # ── tsgo action: declaration emit, or diagnostics only ────────────────
    #
    # This action already had to construct the complete program -- every
    # transitive .d.ts and every npm package.json -- to type-check. In tsgo mode
    # it keeps the declarations it computes instead of discarding them, which is
    # what removes the isolated-declarations requirement at near-zero cost.
    validation_outputs = []
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if tsgo_emits_dts and not tsgo_toolchain_info and compile_srcs:
        fail(
            "ts_compile: declarations = \"tsgo\" needs a tsgo toolchain, and none " +
            "is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")\n" +
            "Or set declarations = \"oxc\" to emit declarations with oxc instead " +
            "(which requires an explicit type on every export).",
        )
    if tsgo_toolchain_info and ctx.attr.enable_check and compile_srcs:
        tsgo = tsgo_toolchain_info.tsgo_info

        # Include both .ts/.tsx sources and ambient .d.ts files in the
        # tsconfig — ambient declarations provide type context for checking.
        check_srcs = compile_srcs + passthrough_dts
        tsconfig = _generate_tsconfig(
            ctx = ctx,
            srcs = check_srcs,
            npm_pkg_dirs = npm_pkg_dirs if npm_pkg_dirs else None,
            type_roots = type_root_files if type_root_files else None,
            module_paths = module_paths,
            extends_file = ctx.file.tsconfig,
            emit_declarations = tsgo_emits_dts,
            emit_root_dir = compile_srcs[0].dirname if tsgo_emits_dts else None,
            emit_out_dir = dts_outputs[0].dirname if tsgo_emits_dts else None,
        )

        # Build the depset of transitive npm package.json files so that
        # moduleResolution:"Bundler" can read exports/types fields from each
        # package. This must be computed before the action is registered.
        npm_pkg_dirs_depset = depset(transitive = transitive_package_dir_sets)

        tsgo_inputs = depset(
            check_srcs + [tsconfig, tsgo.tsgo_binary] + tsconfig_chain,
            transitive = [dep_dts_depset, npm_pkg_dirs_depset],
        )
        if not tsgo_emits_dts:
            # Diagnostics only. Stays in the _validation output group so it runs
            # concurrently with downstream compilation.
            stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
            ctx.actions.run_shell(
                inputs = tsgo_inputs,
                outputs = [stamp],
                command = '"{tsgo}" --project "{tsconfig}" --noEmit && touch "{stamp}"'.format(
                    tsgo = tsgo.tsgo_binary.path,
                    tsconfig = tsconfig.path,
                    stamp = stamp.path,
                ),
                env = {"PATH": "/bin:/usr/bin"},
                mnemonic = "TsgoCheck",
                progress_message = "TsgoCheck %{label}",
            )
            validation_outputs.append(stamp)
        else:
            # The .d.ts files are real outputs, so a type error fails the build
            # by construction -- no --output_groups=+_validation needed, and a
            # broken target cannot hand a stale declaration to a consumer.
            tsgo_args = ctx.actions.args()
            tsgo_args.add("--project", tsconfig.path)
            ctx.actions.run(
                inputs = tsgo_inputs,
                outputs = dts_outputs,
                executable = tsgo.tsgo_binary,
                arguments = [tsgo_args],
                mnemonic = "TsgoDeclare",
                progress_message = "TsgoDeclare %{label}",
            )

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

    # Derived from bin_dir rather than from a declared File so that a target
    # with no sources of its own still forwards its deps' modules.
    declaration_root = "/".join([
        p
        for p in [ctx.bin_dir.path, ctx.label.workspace_root, ctx.label.package]
        if p
    ])
    source_root = "/".join([
        p
        for p in [ctx.label.workspace_root, ctx.label.package]
        if p
    ])
    own_modules = []
    if ctx.attr.module_name:
        own_modules.append(struct(
            module_name = ctx.attr.module_name,
            declaration_root = declaration_root,
            source_root = source_root,
        ))
    providers.append(TsModuleInfo(
        module_name = ctx.attr.module_name,
        declaration_root = declaration_root,
        source_root = source_root,
        transitive_modules = depset(own_modules, transitive = module_sets),
    ))

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
        "declarations": attr.string(
            doc = """Which tool emits the .d.ts files.

"tsgo" (default): the tsgo action emits declarations from the full type
program. Source needs no explicit export annotations, and the declarations are
exactly what tsc would produce. Type errors fail the build because the .d.ts
are real outputs. Type-checking is on the critical path.

"oxc": oxc emits declarations syntactically, per file, without a type program.
This REQUIRES isolated declarations -- every export needs an explicit type --
and oxc errors if that does not hold. In exchange, type-checking moves off the
critical path into the _validation output group, so downstream targets compile
while checking runs concurrently.""",
            default = "tsgo",
            values = ["tsgo", "oxc"],
        ),
        "enable_check": attr.bool(
            doc = "Run tsgo type-checking as a validation action (requires tsgo toolchain).",
            default = True,
        ),
        "tsconfig": attr.label(
            doc = """The project's own tsconfig.json, used as the compilerOptions baseline.

Either a .json file or a ts_config target (which additionally declares the
files the tsconfig `extends`). The file is referenced where it lives, not
copied, so relative paths inside it keep resolving against the directory they
were written for.

The generated tsconfig `extends` this file and overrides the options Bazel owns
(see _BAZEL_OWNED_OPTIONS) plus paths and include. Everything else -- strict,
module, moduleResolution, lib, the strict* family, verbatimModuleSyntax and the
rest -- is whatever the file says, so tsgo checks the code under the same
options `tsc` would.

Leave it unset for the zero-config baseline (strict, module Preserve,
moduleResolution Bundler, skipLibCheck, esModuleInterop).""",
            allow_single_file = [".json"],
        ),
        "compiler_options_json": attr.string(
            doc = """JSON object of compilerOptions overrides, on top of `tsconfig`.

Set through the ts_compile macro's lib / types / target / jsx_mode /
jsx_import_source / compiler_options arguments rather than written by hand.
Entries in `types` and `typeRoots` are treated as relative to the target's
package, matching how they are written in a package's own tsconfig.json.""",
        ),
        "module_name": attr.string(
            doc = """The bare specifier this target is importable as, e.g. "@acme/ui".

Dependents get a paths entry mapping this name (and its subpaths) to the .d.ts
files this target produces, wherever the current configuration puts them. The
entry point is index.d.ts.""",
        ),
        "path_aliases": attr.string_dict(
            doc = """Source-level path alias mappings to inject into the tsgo validation tsconfig.

Maps alias prefixes (as they appear in import statements) to workspace-relative
**source** directory paths. These are added to the compilerOptions.paths section
of the generated tsconfig so that tsgo can resolve path aliases that are defined
in the project's tsconfig.json (compilerOptions.paths cannot be inherited from
it: paths is one key, and the rule owns it).

A value pointing into bazel-out/ is rejected -- to make a bare specifier resolve
to another target's generated declarations, set module_name on that target and
depend on it.

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

Compiler options come from `tsconfig` (the project's own file, whatever it
says) and from `compiler_options_json`, in that order, with the options Bazel
owns applied last. Use the ts_compile macro in //ts:defs.bzl rather than this
rule directly: it takes lib / types / compiler_options as Starlark values.
""",
)
