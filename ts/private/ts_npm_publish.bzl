"""ts_npm_publish rule — assembles a publishable npm package from a ts_compile target.

Usage:

    ts_npm_publish(
        name    = "my_lib_pkg",
        package = ":my_lib",              # ts_compile target
        package_json = ":package.json",   # package.json template
        version = "1.2.3",               # optional version override
    )

    bazel build //:my_lib_pkg            # produces my_lib_pkg/ directory
    bazel build //:my_lib_pkg.tar        # produces my_lib_pkg.tar (npm-publishable tarball)

The rule:
  1. Collects .js, .js.map, and .d.ts outputs from the ts_compile target.
  2. Reads the package.json template and:
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
    # --- Collect outputs from the ts_compile target ----------------------------
    js_info = ctx.attr.package[JsInfo]
    dts_info = ctx.attr.package[TsDeclarationInfo]

    js_files = js_info.js_files.to_list()
    js_map_files = js_info.js_map_files.to_list()
    dts_files = dts_info.declaration_files.to_list()

    # --- Determine the output directory name ----------------------------------
    pkg_name = ctx.label.name
    out_dir = ctx.actions.declare_directory("{}_pkg".format(pkg_name))

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

    # --- Copy script ----------------------------------------------------------
    # We use a small shell script executed via ctx.actions.run_shell that
    # copies each file to its destination inside the output directory.

    copy_cmds = []
    for f in all_srcs:
        dest_rel = _dest_rel(f)
        copy_cmds.append(
            'mkdir -p "{dir}/{dest_dir}" && cp -f "{src}" "{dir}/{dest}"'.format(
                src = f.path,
                dir = out_dir.path,
                dest = dest_rel,
                dest_dir = dest_rel.rsplit("/", 1)[0] if "/" in dest_rel else ".",
            ),
        )

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
    pkg_json_src = ctx.file.package_json

    # Generate the final package.json using a shell/awk script.
    # All values that may be injected (version, main, types, exports) are known
    # at analysis time, so we write an awk script that:
    #   1. Reads the template JSON line by line.
    #   2. Replaces "version" value when ctx.attr.version is set.
    #   3. Appends missing fields (main, types, exports) before the last "}"
    #      only when they are absent from the template.
    # This approach is hermetic — no Python, Node.js, or jq required.

    # Build the list of fields to inject if absent.
    # We defer the "present?" check to the awk script at action time, since
    # we cannot read the template file content at Starlark analysis time.
    inject_version = ctx.attr.version  # "" means don't override
    inject_main = "./" + entry_js_name if entry_js_name else ""
    inject_types = "./" + entry_dts_name if entry_dts_name else ""

    # Build the exports JSON fragment to inject (only when entry_js is set).
    if entry_js_name:
        if entry_dts_name:
            inject_exports = (
                '{".":{' +
                '"import":"./' + entry_js_name + '",' +
                '"types":"./' + entry_dts_name + '"' +
                "}}"
            )
        else:
            inject_exports = '{".":{' + '"import":"./' + entry_js_name + '"' + "}}"
    else:
        inject_exports = ""

    # Write an awk script that does the JSON patching.
    # Values to inject are embedded directly into the awk script as string
    # literals so that no shell interpolation is needed — eliminating both
    # the shell-injection vector and the need for gawk-only -v quoting tricks.
    #
    # Strategy:
    #  - Read the entire file into an array.
    #  - Track which top-level keys are already present using POSIX sub().
    #  - On the last line (the closing "}"), prepend any missing injections.

    # Escape backslash and double-quote for safe embedding in an awk string literal.
    def _awk_str(s):
        return s.replace("\\", "\\\\").replace('"', '\\"')

    awk_script_content = (
        # Embed all inject values as awk variables at the top of the script.
        # This avoids passing them through the shell entirely.
        'BEGIN {\n' +
        '    ver       = "' + _awk_str(inject_version) + '"\n' +
        '    inj_main  = "' + _awk_str(inject_main) + '"\n' +
        '    inj_types = "' + _awk_str(inject_types) + '"\n' +
        '    inj_exports = "' + _awk_str(inject_exports) + '"\n' +
        '}\n' +
        r"""
{
    lines[NR] = $0
    # Detect top-level string keys: "key" at the start of the line (after
    # optional whitespace), followed by a colon.  We only match depth-1 keys
    # (lines that start the field at column 1-3).
    # POSIX-compatible key extraction: strip leading whitespace+quote, then
    # everything from the first remaining quote onward, leaving just the key.
    tmp = $0
    gsub(/^[[:space:]]*"/, "", tmp)
    gsub(/".*/, "", tmp)
    key = tmp
    # Only record the key when the original line actually had the key pattern.
    if ($0 ~ /^[[:space:]]*"[^"]*"[[:space:]]*:/) {
        if (key == "version") has_version = 1
        if (key == "main")    has_main    = 1
        if (key == "types")   has_types   = 1
        if (key == "exports") has_exports = 1
    }
}
END {
    # Collect injections to add before the closing "}"
    n_inject = 0

    if (ver != "" && !has_version) {
        inject[n_inject++] = "  \"version\": \"" ver "\","
    }
    if (inj_main != "" && !has_main) {
        inject[n_inject++] = "  \"main\": \"" inj_main "\","
    }
    if (inj_types != "" && !has_types) {
        inject[n_inject++] = "  \"types\": \"" inj_types "\","
    }
    if (inj_exports != "" && !has_exports) {
        inject[n_inject++] = "  \"exports\": " inj_exports ","
    }

    # Find the last non-blank line (the closing "}") and note its index.
    last = NR
    while (last > 0 && lines[last] ~ /^[[:space:]]*$/) last--

    # Print lines up to (but not including) the last closing "}", inserting
    # version override if requested, then injections, then the closing "}".
    for (i = 1; i < last; i++) {
        line = lines[i]
        # Override "version" value in-place when ver is set and key exists.
        # POSIX-compatible: use sub() to replace the value portion in-place.
        # The pattern matches the colon+whitespace+quoted-value, preserving
        # any trailing comma or other suffix on the same line.
        if (ver != "" && has_version && line ~ /^[[:space:]]*"version"[[:space:]]*:/) {
            sub(/:[[:space:]]*"[^"]*"/, ": \"" ver "\"", line)
        }
        # Remove trailing comma from the last real field before we append more.
        if (i == last - 1 && n_inject > 0) {
            sub(/,[[:space:]]*$/, "", line)
        }
        print line
    }

    # Print injections (all but the last with commas — they already have commas).
    # Remove the trailing comma from the very last injection if the closing "}"
    # is the final line (standard JSON — no trailing comma).
    if (n_inject > 0) {
        for (j = 0; j < n_inject - 1; j++) {
            print inject[j]
        }
        # Last injection: strip trailing comma (valid JSON requires no trailing comma).
        last_inj = inject[n_inject - 1]
        sub(/,$/, "", last_inj)
        print last_inj
    }

    # Print the closing line.
    print lines[last]
}
"""
    )

    awk_script_file = ctx.actions.declare_file("{}_gen_pkg_json.awk".format(pkg_name))
    ctx.actions.write(output = awk_script_file, content = awk_script_content)

    updated_pkg_json = ctx.actions.declare_file("{}_package.json".format(pkg_name))
    ctx.actions.run_shell(
        inputs = [pkg_json_src, awk_script_file],
        outputs = [updated_pkg_json],
        # Pass files as positional args so no values go through shell expansion.
        command = 'awk -f "$1" "$2" > "$3"',
        arguments = [awk_script_file.path, pkg_json_src.path, updated_pkg_json.path],
        mnemonic = "TsNpmPackageJson",
        progress_message = "Generating package.json for {}".format(ctx.label),
    )
    package_json_file = updated_pkg_json

    copy_cmds.append(
        'cp -f "{src}" "{dir}/package.json"'.format(
            src = package_json_file.path,
            dir = out_dir.path,
        ),
    )

    # --- Run the copy action --------------------------------------------------
    all_inputs = all_srcs + [package_json_file]
    ctx.actions.run_shell(
        inputs = all_inputs,
        outputs = [out_dir],
        command = "\n".join(copy_cmds),
        mnemonic = "TsNpmPublishStage",
        progress_message = "Staging npm package for {}".format(ctx.label),
    )

    # --- Optional tarball -----------------------------------------------------
    # Emit a .tar file that `npm publish` can consume directly.
    # npm publish accepts a tarball whose top-level directory is named "package/".
    tarball = ctx.actions.declare_file("{}_pkg.tar".format(pkg_name))
    ctx.actions.run_shell(
        inputs = [out_dir],
        outputs = [tarball],
        command = (
            # Change into the parent directory of the staging dir so that the
            # archive entries start with just the directory basename.
            # Capture $PWD (the execroot) before cd so we can use an absolute
            # path for the tar output file.
            # Use a "package" symlink for cross-platform compatibility:
            # GNU tar --transform is not available on macOS.
            'execroot="$PWD" && cd "{parent}" && '.format(
                parent = out_dir.path.rsplit("/", 1)[0],
            ) +
            'ln -sf "{base}" package && '.format(
                base = out_dir.basename,
            ) +
            'tar chf "$execroot/{tar}" package && '.format(
                tar = tarball.path,
            ) +
            "rm -f package"
        ),
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
    attrs = {
        "package": attr.label(
            doc = "A ts_compile target whose .js, .js.map, and .d.ts outputs are included.",
            mandatory = True,
            providers = [JsInfo, TsDeclarationInfo],
        ),
        "package_json": attr.label(
            doc = "A package.json template. The file is used as-is unless `version` is set.",
            mandatory = True,
            allow_single_file = True,
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
everything into a staging directory.  An additional .tar output is produced
in the npm-publish tarball format (top-level directory named "package/").

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
