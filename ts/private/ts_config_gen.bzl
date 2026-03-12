"""Generates tsconfig.json from the Bazel target graph.

ts_config_gen produces a hermetic, Bazel-tracked tsconfig.json that reflects
the exact set of source files and project references that tsgo needs in order
to type-check a ts_compile target.

Generated tsconfig options:
  - strict: true
  - isolatedDeclarations: true
  - emitDeclarationOnly: true   (type-checking; full emit is handled by oxc)
  - declaration: true
  - references:                 (one entry per dep with its own TsConfigInfo)
  - include:                    (the srcs from the current target)

Note: composite: true is intentionally omitted because ts_check uses
`tsgo --project tsconfig.json --noEmit` (not --build mode). composite + noEmit
is invalid for TypeScript; avoiding composite avoids that conflict entirely.

Notes on paths:
  tsgo resolves all paths relative to the tsconfig file's physical directory.
  Since Bazel actions run in the execroot, all File.path values are
  execroot-relative.  The tsconfig lives under bazel-out/<cfg>/bin/<pkg>/
  while source files live at <pkg>/ (no bazel-out prefix).  We compute
  correct relative paths using _relative_path which handles this mismatch
  by climbing out of bazel-out/ with the appropriate number of "../" segments.
"""

load("//ts/private:providers.bzl", "TsConfigInfo", "TsDeclarationInfo")

# ─── Path helpers ─────────────────────────────────────────────────────────────

def _relative_path(from_dir, to_dir):
    """Computes a relative path from from_dir to to_dir.

    Both arguments are /-separated directory paths (no trailing slash).
    Returns a string like "../../other/pkg" or "sibling".

    This is a simplified implementation that works for paths that share
    a common prefix (which is always true within the Bazel execroot).
    """
    from_parts = [p for p in from_dir.split("/") if p]
    to_parts = [p for p in to_dir.split("/") if p]

    # Find the length of the common prefix.
    common_len = 0
    min_len = len(from_parts) if len(from_parts) < len(to_parts) else len(to_parts)
    for i in range(min_len):
        if from_parts[i] == to_parts[i]:
            common_len += 1
        else:
            break

    # Number of ".." segments needed to back up from from_dir to common ancestor.
    up_count = len(from_parts) - common_len
    up_parts = [".."] * up_count

    # Remaining components in to_dir after the common prefix.
    down_parts = to_parts[common_len:]

    result_parts = up_parts + down_parts
    if not result_parts:
        return "."
    return "/".join(result_parts)

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_config_gen_impl(ctx):
    # Declare the output file first so we know its path for relative-path
    # computation.
    tsconfig_out = ctx.actions.declare_file(
        "{}.tsconfig.json".format(ctx.label.name),
    )

    # Collect dep tsconfigs for the `references` array.
    dep_tsconfigs = []
    deps_tsconfig_sets = []
    for dep in ctx.attr.deps:
        if TsConfigInfo in dep:
            dep_tsconfigs.append(dep[TsConfigInfo].tsconfig)
            deps_tsconfig_sets.append(dep[TsConfigInfo].deps_tsconfigs)

    # Use .path (execroot-relative) for both references and includes so that
    # tsgo can physically resolve the paths at action runtime.
    #
    # The tsconfig lives in bazel-out/<cfg>/bin/<pkg>/, while source files
    # live at <pkg>/ (no bazel-out prefix).  _relative_path handles this
    # correctly: it finds no common prefix and emits the right number of
    # "../" segments to climb out of bazel-out before descending to the
    # source directory.
    #
    # Dep tsconfigs are also in bazel-out, so reference paths share the
    # common bazel-out prefix and produce short relative paths.
    tsconfig_dir = tsconfig_out.dirname

    # rootDirs: tell tsgo that the execroot root and the bin dir both
    # form a single virtual tree.  This enables cross-package relative
    # imports to resolve dep .d.ts files in the output tree.
    # Use ctx.bin_dir.path for stable computation independent of the
    # Bazel output configuration (fastbuild, opt, etc.).
    execroot_rel = _relative_path(tsconfig_dir, "")
    bin_dir_rel = _relative_path(tsconfig_dir, ctx.bin_dir.path)

    # Build the references JSON array.
    # Each reference points at the directory containing the dep's tsconfig,
    # expressed as a path relative to the current tsconfig's physical dir.
    references_items = []
    for dep_tsconfig in dep_tsconfigs:
        dep_dir = dep_tsconfig.dirname
        rel = _relative_path(tsconfig_dir, dep_dir) if tsconfig_dir or dep_dir else "."
        references_items.append('    {{"path": "{}"}}'.format(rel))

    if references_items:
        references_json = "[\n{}\n  ]".format(",\n".join(references_items))
    else:
        references_json = "[]"

    # Build the include array from srcs.
    # Paths are relative to the tsconfig's physical directory (in bazel-out).
    # Source files live at <pkg>/file.ts (execroot root), while the tsconfig
    # is at bazel-out/<cfg>/bin/<pkg>/name.tsconfig.json.  _relative_path
    # correctly handles this mismatch by climbing out of bazel-out.
    include_items = []
    for src in ctx.files.srcs:
        src_dir = src.dirname if src.dirname else ""
        if src_dir == tsconfig_dir:
            include_items.append('    "{}"'.format(src.basename))
        else:
            rel_src = _relative_path(tsconfig_dir, src_dir) if tsconfig_dir or src_dir else "."
            include_items.append('    "{}/{}"'.format(rel_src, src.basename))

    if include_items:
        include_json = "[\n{}\n  ]".format(",\n".join(include_items))
    else:
        include_json = "[]"

    # Derive target and jsx from attributes (defaulting to ES2022 / react-jsx).
    ts_target = ctx.attr.target if ctx.attr.target else "ES2022"
    jsx_val = ctx.attr.jsx_mode if ctx.attr.jsx_mode else ""

    jsx_entry = ""
    if jsx_val:
        jsx_entry = ',\n    "jsx": "{}"'.format(jsx_val)

    # Compose the tsconfig JSON.
    # composite: true is omitted — ts_check uses --noEmit (not --build), so
    # composite is neither needed nor valid with noEmit.
    # emitDeclarationOnly + declaration: true is harmless since --noEmit
    # overrides emit at the CLI level.
    #
    # The jsx_entry is inserted AFTER esModuleInterop.  When present it
    # includes a leading comma; when empty the template still produces valid
    # JSON because "esModuleInterop": true is the last compilerOptions entry.
    tsconfig_content = """\
{{
  "compilerOptions": {{
    "strict": true,
    "isolatedDeclarations": true,
    "declaration": true,
    "emitDeclarationOnly": true,
    "declarationMap": true,
    "sourceMap": true,
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "target": "{target}",
    "skipLibCheck": true,
    "esModuleInterop": true,
    "rootDirs": ["{execroot_rel}", "{bin_dir_rel}"]{jsx_entry}
  }},
  "references": {references},
  "include": {include}
}}
""".format(target = ts_target, jsx_entry = jsx_entry, references = references_json, include = include_json, execroot_rel = execroot_rel, bin_dir_rel = bin_dir_rel)

    ctx.actions.write(
        output = tsconfig_out,
        content = tsconfig_content,
    )

    # Propagate the transitive tsconfig depset.
    transitive_tsconfigs = depset(
        [tsconfig_out],
        transitive = deps_tsconfig_sets,
        order = "postorder",
    )

    return [
        DefaultInfo(files = depset([tsconfig_out])),
        TsConfigInfo(
            tsconfig = tsconfig_out,
            deps_tsconfigs = transitive_tsconfigs,
        ),
    ]

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_config_gen = rule(
    implementation = _ts_config_gen_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "TypeScript source files to include in the generated tsconfig.",
            allow_files = [".ts", ".tsx"],
        ),
        "deps": attr.label_list(
            doc = "ts_compile or ts_config_gen deps whose output dirs populate references.",
            providers = [[TsConfigInfo]],
        ),
        "target": attr.string(
            doc = "ECMAScript target version for the tsconfig (e.g. 'ES2022').",
            default = "ES2022",
        ),
        "jsx_mode": attr.string(
            doc = "JSX mode for the tsconfig (e.g. 'react-jsx'). Empty string omits the jsx option.",
            default = "react-jsx",
        ),
    },
    doc = """Generates a tsconfig.json from the Bazel target graph.

The generated tsconfig uses project references so that tsgo can perform
type-checking across the full target graph.  Each dep that provides
TsConfigInfo becomes a project reference.

composite mode is intentionally NOT used — ts_check invokes tsgo with
--noEmit (not --build), making composite unnecessary and avoiding the
TypeScript restriction that composite and noEmit cannot be combined.

Example:
    ts_config_gen(
        name = "button_tsconfig",
        srcs = ["Button.tsx"],
        deps = ["//components/icon"],
        target = "ES2022",
        jsx_mode = "react-jsx",
    )
""",
)
