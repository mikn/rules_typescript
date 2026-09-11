"""The TsManifest action: a package.json src written as built.

tsc, node and Vite read the nearest package.json for the module's format and
the package's own name, and resolve a self-reference through its `exports`; a
manifest names sources, and the two readers that hold the emit -- a dependent's
program root and the member's store tree -- read this copy, each source-file
target rewritten to the emitted file (docs/guides/npm.md § What a Workspace
Member Is Imported As). The src stays staged as written beside it.
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
