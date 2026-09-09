"""The tsgo action: TsgoDeclare emits the declarations, TsgoCheck a stamp.

tsaction runs tsgo from a program root that mirrors the exec root with the
forest at its node_modules, so every bare specifier resolves as over pnpm.
"""

def tsgo_action(
        ctx,
        tsgo,
        tsconfig,
        forest,
        srcs,
        chain,
        gate,
        dep_dts,
        emit_outputs):
    """Registers the one tsgo run a target makes.

    With `emit_outputs` it is TsgoDeclare and they are its outputs, so a type
    error fails the build and no stale declaration survives; without, it is
    TsgoCheck under --noEmit, and the stamp returned is its output, for the
    _validation group.
    """
    stamp = None
    if not emit_outputs:
        stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
    run_args = ctx.actions.args()
    run_args.add(
        "-root={}/{}.program".format(tsconfig.dirname, ctx.label.name),
    )
    run_args.add("-node_modules=" + forest.path)
    if stamp:
        run_args.add(stamp, format = "-stamp=%s")
    run_args.add("--")
    run_args.add(tsgo.tsgo_binary)
    run_args.add("--project", tsconfig)
    if stamp:
        run_args.add("--noEmit")
    mnemonic = "TsgoCheck" if stamp else "TsgoDeclare"
    ctx.actions.run(
        inputs = depset(
            srcs + [tsconfig, forest, tsgo.tsgo_binary] + chain + gate,
            transitive = [dep_dts],
        ),
        outputs = [stamp] if stamp else emit_outputs,
        executable = ctx.executable._tsaction,
        arguments = ["tsgo", run_args],
        mnemonic = mnemonic,
        progress_message = mnemonic + " %{label}",
    )
    return stamp
