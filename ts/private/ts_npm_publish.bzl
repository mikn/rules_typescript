"""ts_npm_publish rule — assembles a publishable npm package from a ts_compile target.

Usage:

    ts_npm_publish(
        name    = "my_lib_pkg",
        package = ":my_lib",              # ts_compile target
        package_json = ":package.json",   # package.json template
        version = "1.2.3",               # optional version override
    )

    bazel build //:my_lib_pkg            # produces my_lib_pkg_pkg/package/ directory
    bazel build //:my_lib_pkg.tar        # produces my_lib_pkg.tar (npm-publishable tarball)

The rule:
  1. Collects .js, .js.map, and .d.ts outputs from the ts_compile target.
  2. Reads the package.json template (with Node.js, from the js_tool
     toolchain) and:
     a. If `version` is set, overrides the `version` field.
     b. Auto-fills `main`, `types`, and `exports` fields when they are absent
        from the template and the compiled outputs include a deterministic entry
        point (index.js from index.ts, or the sole .js file when there is
        exactly one). This ensures the package is importable by both CommonJS
        and ESM consumers without manual package.json maintenance.
  3. Writes all files into a staging directory whose layout mirrors what
     `npm publish` expects (package.json at root, compiled files alongside it).

The resulting directory can be published directly:

    npm publish $(bazel cquery --output=files //:my_lib_pkg)

Or create a tarball and publish:

    npm publish $(bazel cquery --output=files //:my_lib_pkg.tar)
"""

load("//ts/private:providers.bzl", "JsInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

# ─── Provider ─────────────────────────────────────────────────────────────────

NpmPublishInfo = provider(
    doc = "Provider emitted by ts_npm_publish targets.",
    fields = {
        "pkg_dir": "File: The assembled package directory ready for `npm publish`.",
        "tarball": "File or None: The .tar archive of the package directory (if requested).",
        "package_json": "File: The final package.json inside the assembled directory.",
    },
)

# ─── Implementation ────────────────────────────────────────────────────────────

def _ts_npm_publish_impl(ctx):
    js_tool = get_js_tool(ctx)

    # --- Collect outputs from the ts_compile target ----------------------------
    js_info = ctx.attr.package[JsInfo]
    dts_info = ctx.attr.package[TsDeclarationInfo]

    js_files = js_info.js_files.to_list()
    js_map_files = js_info.js_map_files.to_list()
    dts_files = dts_info.declaration_files.to_list()

    # --- Determine the output directory name ----------------------------------
    pkg_name = ctx.label.name

    # The staging dir is literally named "package": npm publish wants that as
    # the tarball's top-level entry, and naming it so keeps the tar action from
    # having to invent one.
    out_dir = ctx.actions.declare_directory("{}_pkg/package".format(pkg_name))

    # --- Build the list of source file pairs (src_path, dest_relative_path) --
    # We want to place each file at a path relative to the package root.
    # The compile target lives at ctx.attr.package.label.package (e.g. "lib").
    # We strip that prefix so the resulting layout is flat inside the package.

    compile_pkg = ctx.attr.package.label.package  # e.g. "src/lib"

    def _dest_rel(f):
        """Strip the compile package prefix from a file's short_path."""
        p = f.short_path

        # short_path for generated files starts with the package path.
        # e.g. "src/lib/index.js" or (in bazel-out) something under the pkg dir.
        prefix = compile_pkg + "/"
        if p.startswith(prefix):
            return p[len(prefix):]

        # If the file lives in a different package (unusual), keep the full
        # path so nothing is silently dropped.
        return p

    # Collect all files we want to include in the package.
    all_srcs = js_files + js_map_files + dts_files

    # --- Determine entry point file names for main/types/exports fields --------
    # Heuristic: prefer index.js (index.ts compiled), otherwise use the single
    # .js file when there is exactly one. Used to auto-fill main/types/exports
    # in package.json when those fields are not already present.

    entry_js_name = None
    entry_dts_name = None

    if js_files:
        js_basenames = [f.basename for f in js_files]
        if "index.js" in js_basenames:
            entry_js_name = "index.js"
        elif len(js_files) == 1:
            entry_js_name = js_files[0].basename

    if dts_files and entry_js_name:
        # .d.ts filename mirrors the .js filename.
        dts_base = entry_js_name[:-3] + ".d.ts"
        dts_basenames = [f.basename for f in dts_files]
        if dts_base in dts_basenames:
            entry_dts_name = dts_base

    # --- package.json handling ------------------------------------------------
    # The template supplies everything except the fields Bazel knows better:
    # the stamped version and the entry points derived from the compiled
    # outputs.  Those go into a patch file that the generator applies.
    patch = {}
    if ctx.attr.version:
        patch["version"] = ctx.attr.version
    if entry_js_name:
        patch["main"] = "./" + entry_js_name
        exports_entry = {"import": "./" + entry_js_name}
        if entry_dts_name:
            patch["types"] = "./" + entry_dts_name
            exports_entry["types"] = "./" + entry_dts_name
        patch["exports"] = {".": exports_entry}

    patch_file = ctx.actions.declare_file("{}_package_json_patch.json".format(pkg_name))
    ctx.actions.write(output = patch_file, content = json.encode(patch))

    updated_pkg_json = ctx.actions.declare_file("{}_package.json".format(pkg_name))
    ctx.actions.run(
        inputs = [ctx.file.package_json, patch_file, ctx.file._generator],
        outputs = [updated_pkg_json],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [
            ctx.file._generator.path,
            ctx.file.package_json.path,
            patch_file.path,
            updated_pkg_json.path,
        ],
        mnemonic = "TsNpmPackageJson",
        progress_message = "Generating package.json for {}".format(ctx.label),
    )
    package_json_file = updated_pkg_json

    # --- Stage the package directory ------------------------------------------
    stage_args = ctx.actions.args()
    stage_args.add("-out", out_dir.path)
    for f in all_srcs:
        stage_args.add(f)
        stage_args.add(_dest_rel(f))
    stage_args.add(package_json_file)
    stage_args.add("package.json")
    stage_args.use_param_file("@%s", use_always = False)
    stage_args.set_param_file_format("multiline")

    ctx.actions.run(
        inputs = all_srcs + [package_json_file],
        outputs = [out_dir],
        executable = ctx.executable._tsaction,
        arguments = ["stage", stage_args],
        mnemonic = "TsNpmPublishStage",
        progress_message = "Staging npm package for {}".format(ctx.label),
    )

    # --- Optional tarball -----------------------------------------------------
    # npm publish accepts a tarball whose top-level directory is named "package/".
    tarball = ctx.actions.declare_file("{}_pkg.tar".format(pkg_name))
    tar_args = ctx.actions.args()
    tar_args.add("-out", tarball)
    tar_args.add("-dir", out_dir.path)
    tar_args.add("-prefix", "package")

    ctx.actions.run(
        inputs = [out_dir],
        outputs = [tarball],
        executable = ctx.executable._tsaction,
        arguments = ["tar", tar_args],
        mnemonic = "TsNpmPublishTar",
        progress_message = "Creating npm tarball for {}".format(ctx.label),
    )

    return [
        NpmPublishInfo(
            pkg_dir = out_dir,
            tarball = tarball,
            package_json = package_json_file,
        ),
        DefaultInfo(
            files = depset([out_dir, tarball]),
            runfiles = ctx.runfiles(transitive_files = depset([out_dir, tarball])),
        ),
    ]

# ─── Rule definition ──────────────────────────────────────────────────────────

ts_npm_publish = rule(
    implementation = _ts_npm_publish_impl,
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    attrs = {
        "package": attr.label(
            doc = "A ts_compile target whose .js, .js.map, and .d.ts outputs are included.",
            mandatory = True,
            providers = [JsInfo, TsDeclarationInfo],
        ),
        "package_json": attr.label(
            doc = "A package.json template. Its fields are kept except for the ones this rule fills in.",
            mandatory = True,
            allow_single_file = [".json"],
        ),
        "_generator": attr.label(
            default = Label("//ts/private:npm_package_json.mjs"),
            allow_single_file = True,
        ),
        "_tsaction": attr.label(
            default = Label("//ts/tools/tsaction"),
            executable = True,
            cfg = "exec",
        ),
        "version": attr.string(
            doc = (
                "If non-empty, overrides the `version` field in package.json. " +
                "Useful for stamping the version at build time without editing the " +
                "package.json template."
            ),
            default = "",
        ),
    },
    doc = """\
Assembles a publishable npm package from a ts_compile target.

The rule collects the .js, .js.map, and .d.ts outputs from the given
`package` target, merges them with a package.json template, and writes
everything into a staging directory named "package".  An additional .tar output
is produced in the npm-publish tarball format (top-level directory "package/").

Auto-filling package.json entry-point fields
────────────────────────────────────────────
When the package.json template does not already contain `main`, `types`, or
`exports` fields, the rule auto-fills them using the compiled output files:

- `main` → `./index.js` (from index.ts) or the single .js output file.
- `types` → the corresponding .d.ts file (e.g. `./index.d.ts`).
- `exports` → `{"." : {"import": "./index.js", "types": "./index.d.ts"}}`.

If the template already has any of these fields, they are left unchanged.
This behaviour can be suppressed by including empty strings for the fields
you do not want auto-generated (e.g. `"main": ""`).


Example:

    load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_npm_publish")

    ts_compile(
        name = "lib",
        srcs = ["index.ts", "math.ts"],
        visibility = ["//visibility:public"],
    )

    ts_npm_publish(
        name   = "lib_pkg",
        package      = ":lib",
        package_json = ":package.json",
        version      = "1.0.0",
    )

Build and inspect:

    bazel build //:lib_pkg
    ls $(bazel cquery --output=files //:lib_pkg)

Publish:

    npm publish $(bazel cquery --output=files //:lib_pkg | grep '\\.tar$')
""",
)
