"""wrangler over a worker Bazel built: the dry run, and the deploy.

A dry run is the deployable half that can be checked hermetically. It does
everything a deploy does up to the upload -- resolves the config, bundles the
worker, applies the compatibility date and every binding -- and writes the
result to disk instead of sending it, so it needs no credentials and no network.
What it answers is the question CI actually wants: does this worker still
deploy?

The upload is the other half, and it is a `bazel run` target for the same reason
`rules_oci`'s `oci_push` is: it needs credentials, it is not reproducible, and it
must not happen because something in the build graph changed. `bazel build` and
`bazel test` construct the launcher and never fire it; only `bazel run` does.

Three rules over one implementation and one attribute set:

| rule                     | uploads | belongs in                      |
| ------------------------ | ------- | ------------------------------- |
| `ts_worker_dry_run_test` | no      | CI                              |
| `ts_worker_dry_run`      | no      | a local look at the bundle      |
| `ts_worker_deploy`       | **yes** | a deliberate `bazel run`        |

`ts_worker_types` reads the same config for the other thing it declares: the
bindings, as the declarations the worker's sources are typed against. It is a
build action rather than a launcher, so it is a macro over `ts_codegen`.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:node_modules.bzl", "build_node_modules_action", "collect_npm_packages")
load("//ts/private:providers.bzl", "JsInfo", "NpmPackageInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_codegen.bzl", "ts_codegen")

# The launcher's contract for what to do once the worker is bundled. Its own
# default is the dry run, so a config that names neither cannot upload.
_COMMAND_DRY_RUN = "dry-run"
_COMMAND_DEPLOY = "deploy"

def _worker_launcher(ctx, rule_name, command):
    js_runtime = get_js_runtime(ctx)
    if not js_runtime:
        fail(
            "{}: no JS runtime toolchain resolved for '{}'.\n".format(rule_name, ctx.label) +
            "Did you mean to register the toolchains in MODULE.bazel?\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    node_modules_files = ctx.files.node_modules
    if not node_modules_files:
        fail(
            "{}: '{}' needs a node_modules() target carrying wrangler.\n".format(rule_name, ctx.label) +
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
            "command": command,
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

def _ts_worker_dry_run_impl(ctx):
    return _worker_launcher(ctx, "ts_worker_dry_run", _COMMAND_DRY_RUN)

def _ts_worker_deploy_impl(ctx):
    return _worker_launcher(ctx, "ts_worker_deploy", _COMMAND_DEPLOY)

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
writes the result to a scratch directory. Nothing is uploaded, and the launcher
removes the `CLOUDFLARE_*` variables from the environment it hands wrangler so
that a dry run cannot authenticate even where credentials are present. Use
`ts_worker_dry_run_test` for the same thing as a test, and `ts_worker_deploy` to
upload.
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

ts_worker_deploy = rule(
    implementation = _ts_worker_deploy_impl,
    executable = True,
    attrs = _ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Uploads a Cloudflare worker. **This one deploys.**

    ts_worker_deploy(
        name = "deploy",
        config = "wrangler.jsonc",
        node_modules = ":node_modules",
        deps = [":worker"],
    )

`bazel run //path:deploy` bundles the worker the way `ts_worker_dry_run` does and
then sends it, so it needs `CLOUDFLARE_API_TOKEN` (and usually
`CLOUDFLARE_ACCOUNT_ID`) in the environment, or a `wrangler login` under the real
`HOME`. Those are passed through, never read or printed by the ruleset.

Only `bazel run` uploads. `bazel build` writes the launcher and its config;
`bazel test` cannot select a non-test rule; and `bazel run` takes one target, so
no wildcard reaches this. The rule therefore sets no tags of its own -- add
`tags = ["manual"]` yourself if you would rather the target stayed out of
`bazel build //...` too.

Arguments after `--` go to wrangler last, so `bazel run //path:deploy -- --dry-run`
downgrades a deploy to a dry run. The reverse is refused: a `--no-dry-run` handed
to a `ts_worker_dry_run` target is an error, not an upload.
""",
)

def ts_worker_types(
        name,
        config,
        node_modules,
        srcs = [],
        wrangler_args = [],
        out = "worker-configuration.d.ts",
        **kwargs):
    """Generates a worker's `worker-configuration.d.ts` from its wrangler config.

    The bindings a worker reads off `env` are declared in the wrangler config,
    and `wrangler types` is what turns them into an `Env` interface -- plus the
    runtime's own globals, `Request`, `KVNamespace` and the rest, matched to the
    config's compatibility date. A worker repo checks the file in and re-runs the
    command by hand; this runs it as a build action, so the declarations follow
    the config on every build.

    The output is an ambient .d.ts. A tsconfig names one in
    `compilerOptions.types`, and so does a target: `types` with the entry and
    `types_srcs` with this target, which is the pair Gazelle writes under a
    tsconfig that names the file.

        ts_worker_types(
            name = "worker_types",
            config = "wrangler.jsonc",
            node_modules = ":node_modules",
        )

        ts_compile(
            name = "src",
            srcs = ["index.ts"],
            tsconfig = "//workers/api:tsconfig",
            types = ["../worker-configuration.d.ts"],
            types_srcs = ["//workers/api:worker_types"],
        )

    Args:
        name:          Name of the ts_codegen target.
        config:        The wrangler config. Its bindings are what the declarations
                       name, and its compatibility date is what the runtime half
                       is generated for.
        node_modules:  A node_modules() target carrying wrangler.
        srcs:          Worker sources to stage beside the config. Not needed for
                       a binding: the config is read alone. With the file `main`
                       names present, wrangler also writes
                       `Cloudflare.GlobalProps.mainModule` as `typeof import` of
                       it; without it that block is left out.
        wrangler_args: Flags for `wrangler types`, as written on its command
                       line: `--strict-vars=false`,
                       `--env-interface CloudflareBindings`,
                       `--include-runtime=false`, `--env staging`.
        out:           Name of the generated file. The default is what wrangler
                       writes and what a checked-in one is called, so a package
                       holding both gets Bazel's overlap error rather than two
                       answers.
        **kwargs:      Passed to the underlying ts_codegen (`visibility`, `tags`).
    """
    ts_codegen(
        name = name,
        srcs = [config] + srcs,
        outs = [out],
        generator = Label("//tools/codegen:wrangler_types"),
        node_modules = node_modules,
        args = [
            "--config",
            config.rsplit(":", 1)[-1].rsplit("/", 1)[-1],
            "--out",
            "{out}",
            "--srcs",
            "{srcs}",
        ] + wrangler_args,
        **kwargs
    )
