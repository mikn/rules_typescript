"""Source-built oj backend for ts_dev_server."""

load("//ts/private:providers.bzl", "DevServerInfo")

def _oj_dev_server_impl(ctx):
    return [DevServerInfo(
        server_binary = ctx.executable.oj,
        server_in_tree = "",
        argv = ["dev", "--config", "{config}", "--port", "{port}", "{root}"],
        config_dialect = "vite",
        runs_in_js_runtime = False,
        ignored_config_fields = ["root", "cacheDir"],
        native_react_refresh = True,
        runtime_deps = depset([ctx.executable.oj]),
    )]

oj_dev_server = rule(
    implementation = _oj_dev_server_impl,
    attrs = {
        "oj": attr.label(
            default = "@oj_crates//:oj__oj",
            executable = True,
            cfg = "target",
        ),
    },
)
