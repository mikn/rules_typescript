"""Optional Vite implementation selected with ts_dev_server(server = "@rules_typescript//vite:dev_server")."""

load("//ts/private:providers.bzl", "DevServerInfo")

def _vite_dev_server_impl(_ctx):
    return [DevServerInfo(
        server_binary = None,
        server_in_tree = "vite/bin/vite.js",
        argv = ["dev", "--config", "{config}"],
        config_dialect = "vite",
        runs_in_js_runtime = True,
        ignored_config_fields = [],
        native_react_refresh = False,
        runtime_deps = depset(),
    )]

vite_dev_server = rule(
    implementation = _vite_dev_server_impl,
    doc = "Declares the optional Vite server; its node_modules must include Vite.",
)
