"""oj dev server implementation for rules_typescript.

oj (https://github.com/raphamorim/oj) is a Rust-native build tool that adopts
Vite's config and runs Vite/Rollup plugins through a persistent Node host, so it
serves the same generated config Vite gets. It is selected per target:

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        server = "@rules_typescript//oj:dev_server",
    )

Two differences from Vite are structural rather than incidental, and both are
declared in the provider rather than handled in the launcher.

oj takes the directory it serves from a positional argument; the `root` in the
config is not read. So `{root}` is in its argv, and `root` is in
`ignored_config_fields` -- a config field oj does without rather than one it
drops.

The rest of `ignored_config_fields` is what oj genuinely does not read. oj
adopts `base`, `publicDir`, `server.port`, `server.host`, `server.fs.allow`,
`server.proxy`, `server.headers`, `define`, `resolve.alias`, `resolve.dedupe`,
`optimizeDeps` and `build.rollupOptions` from a Vite config, and loads
`plugins` through its Node plugin host. It does not read `server.open` or
`server.watch`: it has its own watcher, so a target relying on Vite's watcher
paths to see a rebuild has to say so. Nor `cacheDir`: oj's cache root is
`<root>/.oj-cache`, with no setting to move it.

oj applies React Fast Refresh itself, so `react_refresh = True` is an error
against it rather than a no-op: @vitejs/plugin-react on top of a transform that
already ran would instrument every component twice.

oj is a native binary and needs no npm package to run, but it is not Node-free:
its plugin host is a Node process, which is why `ts_dev_server` puts the
toolchain Node on PATH for it.
"""

load("//ts/private:providers.bzl", "DevServerInfo")

def _oj_dev_server_impl(ctx):
    return [DevServerInfo(
        server_binary = ctx.executable.oj,
        server_in_tree = "",
        argv = ["dev", "--config", "{config}", "{root}"],
        config_dialect = "vite",
        runs_in_js_runtime = False,
        ignored_config_fields = [
            "root",
            "cacheDir",
            "server.open",
            "server.watch.paths",
        ],
        native_react_refresh = True,
        runtime_deps = depset([ctx.executable.oj]),
    )]

oj_dev_server = rule(
    implementation = _oj_dev_server_impl,
    attrs = {
        "oj": attr.label(
            doc = "The oj binary. Defaults to the crate pinned in this module's MODULE.bazel; " +
                  "point it elsewhere to serve a different build of oj.",
            default = "@oj_crates//:oj__oj",
            executable = True,
            cfg = "target",
        ),
    },
    doc = """Declares oj as a `ts_dev_server` implementation.

oj has no npm package and publishes no release binary, so this depends on the
crate built from source by `MODULE.bazel`'s `oj_crates` extension. That build is
hermetic and cached, but it is a Rust compile on a cold cache.

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",
        server = "@rules_typescript//oj:dev_server",
    )

The `node_modules` attr is still required and still needs the app's own
dependencies: oj resolves bare specifiers through that tree via the generated
config's `bazel:npm-resolve` plugin, exactly as Vite does. What it no longer
needs there is `@npm//:vite`.
""",
)
