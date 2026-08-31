"""`wrangler deploy --dry-run` over a worker Bazel built.

A dry run is the deployable half that can be checked hermetically. It does
everything a deploy does up to the upload -- resolves the config, bundles the
worker, applies the compatibility date and every binding -- and writes the
result to disk instead of sending it, so it needs no credentials and no network.
What it answers is the question CI actually wants: does this worker still
deploy?

Publishing is deliberately not a rule. It needs credentials, it is not
reproducible, and it is the one step that must not happen because something in
the graph changed -- so it stays a command a human or a release job runs.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:node_modules.bzl", "build_node_modules_action", "collect_npm_packages")
load("//ts/private:providers.bzl", "JsInfo", "NpmPackageInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

def _ts_worker_dry_run_impl(ctx):
    js_runtime = get_js_runtime(ctx)
    if not js_runtime:
        fail(
            "ts_worker_dry_run: no JS runtime toolchain resolved for '{}'.\n".format(ctx.label) +
            "Did you mean to register the toolchains in MODULE.bazel?\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    node_modules_files = ctx.files.node_modules
    if not node_modules_files:
        fail(
            "ts_worker_dry_run: '{}' needs a node_modules() target carrying wrangler.\n".format(ctx.label) +
            "    node_modules(name = \"node_modules\", deps = [\"@npm_workers//:wrangler\"])",
        )

    worker_files = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            worker_files.append(dep[JsInfo].transitive_js_files)
    worker_depset = depset(transitive = worker_files)

    launcher = declare_launcher(ctx, {
        "label": str(ctx.label),
        "mode": "wrangler",
        "workspace": ctx.workspace_name,
        "runtime": rlocation_path(ctx, js_runtime.runtime_binary),
        "runtime_args": js_runtime.args_prefix,
        "wrangler": {
            "config_file": rlocation_path(ctx, ctx.file.config),
            "node_modules": rlocation_path(ctx, node_modules_files[0]),
            "wrangler_in_tree": "wrangler/bin/wrangler.js",
            "env_name": ctx.attr.env_name,
            "worker_files": [rlocation_path(ctx, f) for f in worker_depset.to_list()],
            # The worker .js are staged package-relative so that a `main` of
            # "src/index.js" in the config still names them.
            "package_prefix": "{}/{}/".format(ctx.workspace_name, ctx.label.package) if ctx.label.package else ctx.workspace_name + "/",
        },
    })

    runfiles = ctx.runfiles(
        files = [ctx.file.config, js_runtime.runtime_binary] + launcher.files + node_modules_files,
        root_symlinks = launcher.root_symlinks,
        transitive_files = worker_depset,
    )
    return [DefaultInfo(executable = launcher.executable, runfiles = runfiles)]

_ATTRS = LAUNCHER_ATTRS | {
    "config": attr.label(
        doc = "The wrangler config (wrangler.jsonc / wrangler.toml). Its `main` is " +
              "resolved relative to the config, and the worker's compiled .js are " +
              "staged package-relative beside it, so `main` names build output.",
        allow_single_file = [".jsonc", ".json", ".toml"],
        mandatory = True,
    ),
    "deps": attr.label_list(
        doc = "The ts_compile targets whose .js the config's `main` names.",
        providers = [JsInfo],
    ),
    "env_name": attr.string(
        doc = "The wrangler environment to deploy, i.e. `--env`. A config declaring " +
              "several `[env.*]` sections deploys none of them without it, and the " +
              "test form has no command line to pass the flag on.",
    ),
    "node_modules": attr.label(
        doc = "A node_modules() target carrying wrangler.",
        allow_files = True,
        mandatory = True,
    ),
}

ts_worker_dry_run = rule(
    implementation = _ts_worker_dry_run_impl,
    executable = True,
    attrs = _ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Checks that a Cloudflare worker still deploys, without deploying it.

    ts_worker_dry_run(
        name = "deploy_check",
        config = "wrangler.jsonc",
        node_modules = ":node_modules",
        deps = [":worker"],
    )

`bazel run //path:deploy_check` bundles the worker exactly as a deploy would and
writes the result to a scratch directory. Nothing is uploaded and no credentials
are read. Use `ts_worker_dry_run_test` for the same thing as a test.
""",
)

ts_worker_dry_run_test = rule(
    implementation = _ts_worker_dry_run_impl,
    test = True,
    attrs = _ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """The `ts_worker_dry_run` check as a test, which is where it belongs in CI.

A worker that stops bundling -- a binding removed, a compatibility date that no
longer supports something it uses, an entry point that moved -- fails here rather
than at deploy time.
""",
)
