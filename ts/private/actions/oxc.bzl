"""The OxcCompile action: oxc transforms one root's sources into .js.

oxc's --strip-dir-prefix takes a single value, so a target's sources are
grouped by the root their package-relative path hangs off, one run each.
"""

def oxc_compile_action(
        ctx,
        oxc,
        srcs,
        outputs,
        out_base,
        root,
        options_file,
        dep_dts,
        source_map,
        emit_dts,
        gate):
    """Registers one oxc run over `srcs`, all under `root`, writing `outputs`.

    `gate` holds the strict-deps stamp, an input so that the check runs first;
    `emit_dts` adds the isolated-declarations emit of --//ts:declarations=oxc.
    """
    args = ctx.actions.args()
    args.add("--files")
    args.add_all(srcs)
    args.add("--out-dir", out_base)
    if root:
        args.add("--strip-dir-prefix", root)
    if source_map:
        args.add("--source-map")
    if emit_dts:
        args.add("--declaration")
        args.add("--isolated-declarations")
    ctx.actions.run(
        inputs = depset(srcs + gate + [options_file], transitive = [dep_dts]),
        outputs = outputs,
        executable = ctx.executable._tsaction,
        tools = [oxc.oxc_binary],
        arguments = [
            "oxc",
            "-options=" + options_file.path,
            "--",
            oxc.oxc_binary.path,
            args,
        ],
        mnemonic = "OxcCompile",
        progress_message = "OxcCompile %{label}",
    )
