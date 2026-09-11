"""The tsgo action: TsgoDeclare emits the declarations, TsgoCheck a stamp.

tsaction runs tsgo from a program root that mirrors the exec root with each
importer's node_modules at the importer's directory and the outputs of each
first-party dep at or above the target's package laid over that package's
sources, the dep's package.json as built at the package's path, so every bare
specifier resolves as over pnpm's install, the package's own name included, and
checks the --explainFiles listing against
the ownership manifest written here: an edge from one of the target's files
into a file a label outside deps owns fails the action naming that label
(docs/rules/ts-compile.md § Deps Have to Be Direct).
"""

load("//ts/private:providers.bzl", "label_text")

def npm_hub_label(npm_info, package = ""):
    """The hub label a deps list writes for an npm package: the root's
    resolution, or the importer `package`'s.

    The closure carries NpmPackageInfo, not labels: a transitive package was
    never named in any deps list here. Its own repository is
    `<hub>__<package>__<version>...`, so the hub the extension created -- which
    is what a deps list names -- is recoverable from the file it provides.
    """
    name = npm_info.package_name
    label_name = name[1:].replace("/", "_") if name.startswith("@") else name
    hub = "npm"
    owner = npm_info.package_dir.owner if npm_info.package_dir else None
    if owner and owner.repo_name:
        candidate = owner.repo_name.split("__")[0].split("+")[-1]
        if candidate:
            hub = candidate
    return "@{}//{}:{}".format(hub, package, label_name)

def npm_hub_entry(npm_info):
    """struct(name, label, key) for a package the closure carries undeclared."""
    return struct(
        name = npm_info.package_name,
        label = npm_hub_label(npm_info),
        key = npm_info.store.key,
    )

def ownership_manifest(ctx, own, direct, owners, npm_declared, npm_reachable):
    """Writes <name>.ownership: what an edge may resolve to, by owner.

    `own` are the target's tsgo inputs, `direct` its first-party dep labels,
    `owners` the closure's TsInfo.owners records, `npm_declared` struct(name,
    key) per link deps cover -- the store tree it enters -- and
    `npm_reachable` struct(name, label, key) for the rest of the closure.
    Returns the file.
    """
    manifest = ctx.actions.declare_file("{}.ownership".format(ctx.label.name))
    lines = ctx.actions.args()
    lines.set_param_file_format("multiline")
    lines.add("label\t" + label_text(ctx.label))
    lines.add_all(own, format_each = "own\t%s")
    lines.add_all(direct, format_each = "direct\t%s")
    for record in owners.to_list():
        lines.add_all(
            record.files,
            format_each = "file\t{}\t%s".format(record.label),
            expand_directories = False,
        )
    lines.add_all([
        "npm-direct\t{}\t{}".format(link.name, link.key)
        for link in npm_declared
    ])
    lines.add_all([
        "npm\t{}\t{}\t{}".format(package.name, package.label, package.key)
        for package in npm_reachable
    ])
    ctx.actions.write(output = manifest, content = lines)
    return manifest

def tsgo_action(
        ctx,
        tsgo,
        tsconfig,
        importers,
        overlays,
        manifests,
        srcs,
        chain,
        dep_dts,
        npm_files,
        ownership,
        emit_outputs):
    """Registers the one tsgo run a target makes.

    `importers` are the chain's node_modules directories nearest first,
    `overlays` the output directories of the first-party deps at or above the
    target's package, `manifests` those deps' package.json as built, each laid
    at its package's path, and `npm_files` the store files the program
    reaches. With `emit_outputs` it
    is TsgoDeclare and they are its outputs, so a type error fails the build
    and no stale declaration survives; without, it is TsgoCheck under
    --noEmit, and the stamp returned is its output, for the _validation
    group. `ownership` is the manifest the listing is checked against.
    """
    stamp = None
    if not emit_outputs:
        stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
    run_args = ctx.actions.args()
    run_args.add(
        "-root={}/{}.program".format(tsconfig.dirname, ctx.label.name),
    )
    run_args.add_all(importers, format_each = "-node_modules=%s")
    run_args.add_all(overlays, format_each = "-overlay=%s")
    run_args.add_all(manifests, format_each = "-manifest=%s")
    run_args.add(ownership, format = "-check=%s")
    if stamp:
        run_args.add(stamp, format = "-stamp=%s")
    run_args.add("--")
    run_args.add(tsgo.tsgo_binary)
    run_args.add("--project", tsconfig)
    if stamp:
        run_args.add("--noEmit")
    run_args.add("--explainFiles")
    run_args.add("--pretty", "false")
    mnemonic = "TsgoCheck" if stamp else "TsgoDeclare"
    ctx.actions.run(
        inputs = depset(
            srcs + [tsconfig, ownership, tsgo.tsgo_binary] + chain,
            transitive = [dep_dts, npm_files],
        ),
        outputs = [stamp] if stamp else emit_outputs,
        executable = ctx.executable._tsaction,
        arguments = ["tsgo", run_args],
        mnemonic = mnemonic,
        progress_message = mnemonic + " %{label}",
    )
    return stamp
