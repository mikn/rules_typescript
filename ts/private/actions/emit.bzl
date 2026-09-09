"""The TsEmit action: tsaction writes the program's JavaScript.

oxc transforms an ES-module program's sources, one run per root; tsgo emits a
CommonJS-shaped one, with --noCheck, from the program root the check runs in.
docs/rules/ts-compile.md § The Module Format.
"""

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
        forest,
        program_inputs,
        dep_dts,
        options_file,
        scratch,
        source_map,
        emit_dts):
    """Registers the one emit over `srcs`, which hang off `roots`.

    `program_inputs` are the tsgo check's inputs, since the emit is tsgo's
    when the options file says so. `emit_dts` adds the isolated-declarations
    emit of --//ts:declarations=oxc.
    """
    args = ctx.actions.args()
    args.use_param_file("@%s", use_always = False)
    args.set_param_file_format("multiline")
    args.add(options_file, format = "-options=%s")
    args.add(tsconfig, format = "-tsconfig=%s")
    args.add("-node_modules=" + forest.path)
    args.add("-scratch=" + scratch)
    args.add("-out_dir=" + out_base)
    args.add(oxc.oxc_binary, format = "-oxc=%s")
    args.add(tsgo.tsgo_binary, format = "-tsgo=%s")
    args.add_all(roots, format_each = "-root=%s")
    if source_map:
        args.add("-source_map")
    if emit_dts:
        args.add("-declarations")
    args.add_all(srcs)
    ctx.actions.run(
        inputs = depset(
            program_inputs + [options_file, tsconfig, forest] + chain,
            transitive = [dep_dts],
        ),
        outputs = outputs,
        executable = ctx.executable._tsaction,
        tools = [oxc.oxc_binary, tsgo.tsgo_binary],
        arguments = ["emit", args],
        mnemonic = "TsEmit",
        progress_message = "TsEmit %{label}",
    )
