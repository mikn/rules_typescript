"""The Workers pool: vitest's tests inside workerd.

The pool's half of a ts_test's environment, in one file: its attributes, the
WranglerTestConfig action and the symlink it adds. ts_test reaches it through
the struct workers_pool_environment returns, and no other file names wrangler.
"""

load("//ts/private:runtime.bzl", "get_js_tool")

WORKERS_POOL_ATTRS = {
    "wrangler_config": attr.label(
        doc = "The wrangler config a Workers-pool `config` names through " +
              "`wrangler.configPath`.  A copy whose `main` (and every " +
              "`env.<name>.main`) names the compiled entry beside it is " +
              "staged at this file's own runfiles path; the file is not " +
              "also listed in `data`.",
        allow_single_file = [".jsonc", ".json", ".toml"],
    ),
    "_wrangler_patch": attr.label(
        default = Label("//ts/private:wrangler_test_config.mjs"),
        allow_single_file = True,
    ),
}

def workers_pool_environment(ctx, forest, runtime_data_sets):
    """The pool's half of the test environment, or its absence.

    `forest` is ts_test's struct(dirs, rlocations, npm_files). Returns
    struct(symlinks, runtime_data_sets): the patched wrangler config over the
    source's path, and the data sets less that source.
    """
    symlinks = {}

    # The pool boots the file `main` names, the source; a copy naming the
    # compiled entry takes the source's runfiles path, so `configPath` reads it.
    if ctx.file.wrangler_config:
        src = ctx.file.wrangler_config

        # A runfiles file at a symlink's path wins over it silently, and the
        # unpatched `main` with it: the source's copy in data or through a dep.
        for f in ctx.files.data:
            if f.short_path == src.short_path:
                fail(
                    "ts_test {}: {} is staged through wrangler_config; ".format(
                        ctx.label,
                        src.short_path,
                    ) + "do not list it in data too.",
                )
        runtime_data_sets = [depset([
            f
            for f in depset(transitive = runtime_data_sets).to_list()
            if f.short_path != src.short_path
        ])]

        js_tool = get_js_tool(ctx)
        if not js_tool:
            fail(("ts_test {}: wrangler_config needs the JS tool toolchain " +
                  "to patch the config.").format(ctx.label))
        if not forest.dirs:
            fail(("ts_test {}: wrangler_config needs `node_modules`, the " +
                  "importer whose links hold the pool; wrangler is the " +
                  "pool's own edge.").format(ctx.label))
        patched = ctx.actions.declare_file(
            "_{}_wrangler.{}".format(ctx.label.name, src.extension),
        )
        args = ctx.actions.args()
        args.add(ctx.file._wrangler_patch)
        args.add("--config", src)
        args.add("--out", patched)
        args.add_all(forest.dirs, before_each = "--node-modules")
        ctx.actions.run(
            inputs = depset(
                [src, ctx.file._wrangler_patch],
                transitive = [forest.npm_files],
            ),
            outputs = [patched],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [args],
            mnemonic = "WranglerTestConfig",
            progress_message = "WranglerTestConfig %{label}",
        )
        symlinks[src.short_path] = patched

    return struct(
        symlinks = symlinks,
        runtime_data_sets = runtime_data_sets,
    )
