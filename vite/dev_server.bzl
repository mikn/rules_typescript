"""Vite dev server implementation for rules_typescript.

`vite_dev_server` is the default `ts_dev_server(server = ...)`. Vite ships as an
npm package, so there is no File to hand over: the executable is a path inside
whatever `node_modules` tree the target builds, and `ts_dev_server` joins the
two at runtime.

Vite reads the generated config in full, so `ignored_config_fields` is empty --
which is the point of comparison for any other implementation.
"""

load("//ts/private:providers.bzl", "DevServerInfo")

def _vite_dev_server_impl(_ctx):
    return [DevServerInfo(
        server_binary = None,
        server_in_tree = "vite/bin/vite.js",
        argv = ["dev", "--config", "{config}"],
        config_dialect = "vite",
        runs_in_js_runtime = True,
        ignored_config_fields = [],
        runtime_deps = depset(),
    )]

vite_dev_server = rule(
    implementation = _vite_dev_server_impl,
    doc = """Declares Vite as a `ts_dev_server` implementation.

Vite comes out of the target's own `node_modules` tree rather than from this
rule, so the rule takes no attributes -- add `@npm//:vite` to the
`node_modules()` target the server points at. `ts_dev_server` fails naming that
target when vite is missing from it.

    ts_dev_server(
        name = "dev",
        entry_point = ":app",
        node_modules = ":node_modules",   # deps must include @npm//:vite
        server = "@rules_typescript//vite:dev_server",
    )

This is the default, so a target that wants Vite need not set `server` at all.
""",
)
