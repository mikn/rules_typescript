"""The TsManifest action: a package.json src staged as built.

tsc, node and Vite read the nearest package.json for the module's format and
the package's own name, and resolve a self-reference through its `exports`; a
manifest names sources, and every reader of the staged copy holds the emit, so
tsaction rewrites each source-file target to the emitted file
(docs/guides/npm.md § What a Workspace Member Is Imported As).
"""

def manifest_action(ctx, src, out, tsx_extension):
    """Writes `src`, a package.json, to `out` as built; `tsx_extension` is
    what a .tsx emits under the target's tsconfig, ".js" or ".jsx"."""
    args = ctx.actions.args()
    args.add("-tsx=" + tsx_extension)
    args.add(src)
    args.add(out)
    ctx.actions.run(
        inputs = [src],
        outputs = [out],
        executable = ctx.executable._tsaction,
        arguments = ["manifest", args],
        mnemonic = "TsManifest",
        progress_message = "TsManifest %{label}",
    )
