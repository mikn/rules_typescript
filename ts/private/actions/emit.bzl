"""The TsEmit action: tsaction writes the program's JavaScript.

oxc transforms an ES-module program's sources, one run per root; tsgo emits a
CommonJS-shaped one, with --noCheck, from the program root the check runs in.
Under `es_modules` the emit is oxc's whatever the module, with oxc's inputs
alone. docs/rules/ts-compile.md § The Module Format.
"""

load("//ts/private:toolchain.bzl", "get_tools_toolchain")
load(":tsgo.bzl", "source_path")

def emit_action(
        ctx,
        oxc,
        tsgo,
        srcs,
        roots,
        outputs,
        out_base,
        tsconfig,
        chain,
        importers,
        overlays,
        manifests,
        program_inputs,
        dep_dts,
        npm_files,
        options_file,
        scratch,
        source_map,
        emit_dts,
        declared_module,
        es_modules = False):
    """The configuration action validates declared_module before emission runs."""
    args = ctx.actions.args()
    args.use_param_file("@%s", use_always = False)
    args.set_param_file_format("multiline")
    args.add(options_file, format = "-options=%s")

    full_program = not es_modules and bool(declared_module)
    if full_program:
        args.add(tsconfig, format = "-tsconfig=%s")
        args.add_all(
            depset(program_inputs + chain, transitive = [dep_dts]),
            map_each = source_path,
            format_each = "-source=%s",
        )
        args.add_all(importers, format_each = "-node_modules=%s")
        args.add_all(overlays, format_each = "-overlay=%s")
        args.add_all(manifests, format_each = "-manifest=%s")
        args.add("-scratch=" + scratch)
    args.add("-out_dir=" + out_base)
    args.add(oxc.oxc_binary, format = "-oxc=%s")
    if full_program:
        args.add(tsgo.tsgo_binary, format = "-tsgo=%s")
    args.add_all(roots, format_each = "-root=%s")
    if source_map:
        args.add("-source_map")
    if emit_dts:
        args.add("-declarations")
    if es_modules:
        args.add("-es_modules")
    args.add_all(srcs)
    if not full_program:
        inputs = depset(srcs + [options_file])
        tools = [oxc.oxc_binary]
    else:
        inputs = depset(
            program_inputs + [options_file, tsconfig] + chain,
            transitive = [dep_dts, npm_files, tsgo.files],
        )
        tools = [oxc.oxc_binary]
    ctx.actions.run(
        inputs = inputs,
        outputs = outputs,
        executable = get_tools_toolchain(ctx).tsaction,
        tools = tools,
        arguments = ["emit", args],
        mnemonic = "TsEmit",
        progress_message = "TsEmit %{label}",
    )
