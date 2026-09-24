"""The Workers pool: vitest's tests inside workerd.

The pool's half of a ts_test's environment, in one file: its attributes, the
WranglerTestConfig action and the symlink it adds. ts_test reaches it through
the struct workers_pool_environment returns, and no other file names wrangler.
"""

load("//ts/private:runtime.bzl", "get_js_tool")

WORKERS_POOL_ATTRS = {
    "wrangler_config": attr.label(
        doc = "The wrangler config a Workers-pool `config` names through " +
              "`wrangler.configPath`. A copy projecting matching `main` and " +
              "`env.<name>.main` entries to declared runtime artifacts is " +
              "staged at this file's own runfiles path; the file is not " +
              "also listed in `data`.",
        allow_single_file = [".jsonc", ".json", ".toml"],
    ),
    "_wrangler_patch": attr.label(
        default = Label("//ts/private:wrangler_test_config.mjs"),
        allow_single_file = True,
    ),
}

def _runtime_path(file):
    return file.short_path

def workers_pool_environment(ctx, chain, runtime_data_sets, runtime_sources, runtime_js):
    """The pool's half of the test environment, or its absence.

    `chain` is ts_test's struct(dirs, rlocations, npm_files). Returns
    struct(symlinks, runtime_data_sets): the patched wrangler config over the
    source's path, and the data sets less that source.
    """
    symlinks = {}

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
        if not chain.dirs:
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
        args.add("--config-path", src.short_path)
        args.add_all(runtime_sources, before_each = "--runtime-source", map_each = _runtime_path, expand_directories = False)
        args.add_all(runtime_js, before_each = "--runtime-js", map_each = _runtime_path, expand_directories = False)
        args.add_all(chain.dirs, before_each = "--node-modules")
        ctx.actions.run(
            inputs = depset(
                [src, ctx.file._wrangler_patch],
                transitive = [chain.npm_files],
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
