"""The two ways to run a Next.js app: `next dev` over source, `next start` over a build.

Neither rule generates a config. Next.js reads `next.config.*` from its project
directory, and that file is the user's -- so what these rules supply is the npm
tree, through NODE_PATH. Next.js seeds its webpack `resolve.modules` from that
variable, which is how the app's bare imports reach the Bazel-built tree without
a `node_modules` symlink appearing in a source directory.

The two commands need different project directories, and that is the whole
difference between the rules:

  next_dev_server  serves the SOURCE tree, so its project directory is the
                   package this target is declared in, under the workspace
                   `bazel run` reports. Bazel is out of the inner loop: an edit
                   is picked up by Next.js's own watcher, not by a rebuild.

  next_serve       serves the BUILD next_build produced, so the launcher stages
                   that output into a writable scratch directory as `.next`,
                   with the config and the files served from the project root
                   rather than from `.next` (`public/`) beside it.

`next dev` writes into its project directory -- `.next/`, `next-env.d.ts`, and
an `include` entry it adds to tsconfig.json. distDir is a next.config setting
these rules do not own, so that is Next.js's behaviour showing through rather
than something the rule can move; all three are the paths a Next.js project
gitignores anyway.

Turbopack is not supported: `next dev --turbo` replaces the module resolution
NODE_PATH feeds, and nothing here is tested against it.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")

def _runtime(ctx, rule_name):
    js_runtime = get_js_runtime(ctx)
    if not js_runtime:
        fail(
            "{}: no JS runtime toolchain resolved for '{}'.\n".format(rule_name, ctx.label) +
            "The Next.js CLI runs on the toolchain's Node binary out of runfiles; " +
            "it does not fall back to whatever `node` is on your PATH.\n" +
            "Did you mean to register the toolchains in MODULE.bazel?\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )
    return js_runtime

def _npm_tree(ctx, rule_name):
    files = ctx.files.node_modules
    if not files:
        fail(
            "{}: '{}' needs a node_modules() target carrying next.\n".format(rule_name, ctx.label) +
            "    node_modules(\n" +
            "        name = \"node_modules\",\n" +
            "        deps = [\"@npm//:next\", \"@npm//:react\", \"@npm//:react-dom\"],\n" +
            "    )",
        )
    return files

def _package_prefix(ctx):
    if ctx.label.package:
        return "{}/{}/".format(ctx.workspace_name, ctx.label.package)
    return ctx.workspace_name + "/"

def _declare(ctx, rule_name, next_config, files):
    js_runtime = _runtime(ctx, rule_name)
    launcher = declare_launcher(ctx, {
        "label": str(ctx.label),
        "mode": "next",
        "workspace": ctx.workspace_name,
        "runtime": rlocation_path(ctx, js_runtime.runtime_binary),
        "runtime_args": js_runtime.args_prefix,
        "env": ctx.attr.env,
        "next": next_config,
    })
    runfiles = ctx.runfiles(
        files = [js_runtime.runtime_binary] + launcher.files + files,
        root_symlinks = launcher.root_symlinks,
    )
    return [DefaultInfo(executable = launcher.executable, runfiles = runfiles)]

def _next_dev_server_impl(ctx):
    tree = _npm_tree(ctx, "next_dev_server")
    return _declare(
        ctx,
        "next_dev_server",
        {
            "command": "dev",
            "node_modules": rlocation_path(ctx, tree[0]),
            "project_dir": ctx.label.package,
            "port": ctx.attr.port,
        },
        tree + ctx.files.data,
    )

def _next_serve_impl(ctx):
    tree = _npm_tree(ctx, "next_serve")
    build = ctx.attr.build[DefaultInfo].files.to_list()
    if len(build) != 1:
        fail(
            "next_serve: build = '{}' produces {} files; a next_build target ".format(
                ctx.attr.build.label,
                len(build),
            ) +
            "produces exactly one, the .next directory.",
        )
    next_config = {
        "command": "start",
        "node_modules": rlocation_path(ctx, tree[0]),
        "build_dir": rlocation_path(ctx, build[0]),
        "project_files": [rlocation_path(ctx, f) for f in ctx.files.srcs],
        "package_prefix": _package_prefix(ctx),
        "port": ctx.attr.port,
    }
    files = build + tree + ctx.files.srcs + ctx.files.data
    if ctx.file.config:
        next_config["config_file"] = rlocation_path(ctx, ctx.file.config)
        files = files + [ctx.file.config]
    return _declare(ctx, "next_serve", next_config, files)

_COMMON_ATTRS = LAUNCHER_ATTRS | {
    "node_modules": attr.label(
        doc = "A node_modules() target carrying next, react and react-dom. " +
              "The Next.js CLI is run from this tree and the app's bare " +
              "imports resolve through it.",
        allow_files = True,
        mandatory = True,
    ),
    "port": attr.int(
        doc = "The port to listen on. `--port N` past the launcher replaces it, " +
              "which is how a test takes a kernel-assigned one.",
        default = 3000,
    ),
    "env": attr.string_dict(
        doc = "Environment variables for the server process.",
        default = {},
    ),
    "data": attr.label_list(
        doc = "Extra files to place in runfiles.",
        allow_files = True,
    ),
}

next_dev_server = rule(
    implementation = _next_dev_server_impl,
    executable = True,
    attrs = _COMMON_ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Runs `next dev` over the source tree.

    next_dev_server(
        name = "dev",
        node_modules = ":node_modules",
        port = 3000,
    )

`bazel run //:dev` serves the package this target is declared in, reading
`next.config.*`, the route tree and the app's dependencies from source. No
`srcs` attr: Next.js discovers its own routes, and nothing is staged.

`next dev` writes `.next/`, `next-env.d.ts` and a tsconfig.json `include` entry
into that directory -- the same files it writes outside Bazel. Turbopack
(`next dev --turbo`) is untested and unsupported.
""",
)

next_serve = rule(
    implementation = _next_serve_impl,
    executable = True,
    attrs = _COMMON_ATTRS | {
        "build": attr.label(
            doc = "The next_build target whose .next directory to serve.",
            mandatory = True,
        ),
        "config": attr.label(
            doc = "The same next.config file the next_build target was given. " +
                  "`next start` reads it for the settings that apply at request " +
                  "time -- rewrites, headers, image domains, basePath.",
            allow_single_file = True,
        ),
        "srcs": attr.label_list(
            doc = "Files staged beside the build output at their package-relative " +
                  "paths: `public/**`, which Next.js serves from the project root " +
                  "and never copies into .next, and any module the config imports.",
            allow_files = True,
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Runs `next start` over the build next_build produced.

    next_serve(
        name = "serve",
        build = ":app",
        config = "next.config.mjs",
        node_modules = ":node_modules",
        srcs = glob(["public/**"]),
        port = 3000,
    )

`bazel run //:serve` server-renders the built app: dynamic App Router routes,
getServerSideProps, route handlers, API routes and middleware all run, which is
what distinguishes this from serving a static export.

The build output is copied into a scratch directory rather than served from
bazel-bin, because the image optimizer writes its cache into `.next/cache` at
request time and a Bazel output tree is read-only. The copy is discarded when
the server exits.

This is a way to run the build, not a deployment artifact: the deployable unit
is the build output plus the config, `public/` and the npm tree, which is
exactly what this rule assembles at run time. `output: "standalone"` is untested
here.
""",
)
