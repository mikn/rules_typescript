"""A Remix app built with remix_build: client half, server half, one action.

The server half is not just compiled here -- :server_probe runs it. It imports
build/server/index.js through @remix-run/node's createRequestHandler and writes
the real responses to a file the test asserts on, which is the difference
between compiling Remix and building it.
"""

load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "remix_build")

node_modules(
    name = "node_modules",
    deps = [
        "@npm//:react",
        "@npm//:react-dom",
        "@npm//:remix-run_dev",
        "@npm//:remix-run_node",
        "@npm//:remix-run_react",
        "@npm//:vite",
    ],
)

remix_build(
    name = "app",
    srcs = glob([
        "app/**/*.ts",
        "app/**/*.tsx",
    ]),
    config = "remix-vite.config.mjs",
    node_modules = ":node_modules",
)

# The server bundle imports @remix-run/react, react-dom/server and
# react/jsx-runtime as bare specifiers, so it has to be executed from a
# directory with the node_modules tree above it -- and with "type": "module",
# since Remix emits ESM into a plain .js.
genrule(
    name = "server_probe",
    srcs = [
        "drive.mjs",
        ":app",
        ":node_modules",
    ],
    outs = ["server_probe.report.txt"],
    cmd = """
set -euo pipefail
WORK="$$(mktemp -d)"
trap 'rm -rf "$$WORK"' EXIT
ln -s "$$PWD/$(location :node_modules)" "$$WORK/node_modules"
cp -R "$(location :app)/server" "$$WORK/server"
cp "$(location drive.mjs)" "$$WORK/drive.mjs"
printf '{"type":"module"}\\n' > "$$WORK/package.json"
"$$PWD/$(location @rules_typescript//ts/toolchain:node_resolved)" \\
  "$$WORK/drive.mjs" > "$@"
""",
    tools = ["@rules_typescript//ts/toolchain:node_resolved"],
)
