"""The Workers pool: vitest's tests inside workerd.

The pool's half of a ts_test's environment, in one file: its attributes, the
config layer copied beside the generated config, the WranglerTestConfig
action and the runfiles and symlinks both add. ts_test reaches it through the
struct workers_pool_environment returns, and no other file names wrangler.
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
    "_workers_pool": attr.label(
        default = Label("//ts/private:vitest_workers_pool.mjs"),
        allow_single_file = True,
    ),
    "_wrangler_patch": attr.label(
        default = Label("//ts/private:wrangler_test_config.mjs"),
        allow_single_file = True,
    ),
}

def workers_pool_environment(ctx, node_modules_files, runtime_data_sets):
    """The pool's half of the test environment, or its absence.

    Returns struct(layer, files, symlinks, runtime_data_sets): the config layer
    module (None without a `config`), the runfiles the pool adds, the patched
    wrangler config over the source's path, and the data sets less that source.
    """
    files = []
    symlinks = {}

    # The pool's half of the Bazel layer, copied beside the generated config:
    # vitest resolves the config's imports from its real path in bin.
    layer = None
    if ctx.file.config:
        layer = ctx.actions.declare_file(
            "_{}_workers_pool.mjs".format(ctx.label.name),
        )
        ctx.actions.expand_template(
            template = ctx.file._workers_pool,
            output = layer,
            substitutions = {},
        )
        files.append(layer)

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
        if not node_modules_files:
            fail(("ts_test {}: wrangler_config needs a node_modules tree " +
                  "holding wrangler; the pool in deps brings it.").format(
                ctx.label,
            ))
        patched = ctx.actions.declare_file(
            "_{}_wrangler.{}".format(ctx.label.name, src.extension),
        )
        ctx.actions.run(
            inputs = depset(
                [src, ctx.file._wrangler_patch] + node_modules_files,
            ),
            outputs = [patched],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [
                ctx.file._wrangler_patch.path,
                "--config",
                src.path,
                "--out",
                patched.path,
                "--node-modules",
                node_modules_files[0].path,
            ],
            mnemonic = "WranglerTestConfig",
            progress_message = "WranglerTestConfig %{label}",
        )
        files.append(patched)
        symlinks[src.short_path] = patched

    return struct(
        layer = layer,
        files = files,
        symlinks = symlinks,
        runtime_data_sets = runtime_data_sets,
    )
