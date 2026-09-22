"""The TsConfig action: tsaction writes the tsconfig the compile actions read.

It extends the baseline written here, then the target's own chain, and sets
the keys Bazel owns, none of them an emit shape: each tsgo run says on its
command line what it emits. The emit reads target, jsx and module from the
same file. The ts_config's jsx and module declarations are checked against
the chain.
"""

load("//ts/private:toolchain.bzl", "get_tools_toolchain")

# The file the action config extends FIRST, so every key the user's chain sets
# wins. moduleResolution is absent: tsgo derives it from the winning `module`.
_BASELINE_OPTIONS = {
    "strict": True,
    "module": "Preserve",
    "target": "es2022",
    "jsx": "react-jsx",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

def write_baseline_tsconfig(ctx):
    """Writes _BASELINE_OPTIONS as a tsconfig for the action's config to extend.

    A file rather than a dict merge because Starlark cannot read the user's
    tsconfig to see which keys it already sets; TypeScript resolves that itself,
    and an `extends` list is the only place a layer can sit *under* the file.
    """
    out = ctx.actions.declare_file(
        "{}.tsconfig_baseline.json".format(ctx.label.name),
    )
    ctx.actions.write(
        output = out,
        content = json.encode_indent(
            {"compilerOptions": _BASELINE_OPTIONS},
            indent = "  ",
        ),
    )
    return out

def tsconfig_action(
        ctx,
        tsgo,
        check_srcs,
        tsconfig_chain,
        baseline_file,
        dep_dts,
        declared_jsx,
        declared_module,
        types_deps,
        isolated_declarations,
        lib_check,
        emit = True):
    """Writes <name>.tsconfig.json and <name>.options.json from the chain.

    Returns struct(tsconfig, options).
    """
    tsconfig = ctx.actions.declare_file(
        "{}.tsconfig.json".format(ctx.label.name),
    )
    options_file = ctx.actions.declare_file(
        "{}.options.json".format(ctx.label.name),
    )
    config_args = ctx.actions.args()
    config_args.use_param_file("@%s", use_always = False)
    config_args.set_param_file_format("multiline")
    config_args.add(tsgo.tsgo_binary, format = "-tsgo=%s")
    if ctx.file.tsconfig:
        config_args.add(ctx.file.tsconfig, format = "-tsconfig=%s")
    config_args.add(baseline_file, format = "-baseline=%s")
    config_args.add(tsconfig, format = "-out=%s")
    config_args.add(options_file, format = "-options=%s")
    config_args.add(ctx.bin_dir.path, format = "-bin_dir=%s")
    if not emit:
        config_args.add("-source_only")
    if declared_jsx:
        config_args.add(declared_jsx, format = "-jsx=%s")
    if declared_module:
        config_args.add(declared_module, format = "-module=%s")
    config_args.add_all(types_deps, format_each = "-types_dep=%s")
    if isolated_declarations:
        config_args.add("-isolated_declarations")
    if lib_check:
        config_args.add("-lib_check")
    config_args.add_all(check_srcs)
    ctx.actions.run(
        inputs = depset(
            check_srcs + tsconfig_chain,
            transitive = [dep_dts, tsgo.files],
        ),
        outputs = [tsconfig, options_file],
        executable = get_tools_toolchain(ctx).tsaction,
        arguments = ["tsconfig", config_args],
        mnemonic = "TsConfig",
        progress_message = "TsConfig %{label}",
    )
    return struct(tsconfig = tsconfig, options = options_file)
