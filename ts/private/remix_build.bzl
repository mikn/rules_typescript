"""Remix build rule for rules_typescript.

remix_build wraps `remix vite:build` as a single opaque Bazel action, producing
BOTH halves of a Remix application: the browser bundle under `client/` and the
request handler under `server/`.

Why this cannot be a ts_bundle
------------------------------

`remix vite:build` is not one `vite build`. It loads the Vite config once to
read the Remix plugin's own options, then runs `vite build` twice against that
same config -- first with `build.ssr = false`, then with `build.ssr = true` --
and the Remix plugin's `config` hook replaces `build.outDir` and
`build.rollupOptions.input` differently for each half.

The order is load-bearing. The SSR half's `writeBundle` reads the SSR Vite
manifest and MOVES the SSR-emitted assets into the client directory; the
browser asset manifest (`client/assets/manifest-<hash>.js`, which defines the
`window.__remixManifest` a hydrating client reads) is emitted by the SERVER
build and only afterwards belongs to the client. The two halves are therefore
not independently cacheable, and a build that declares only `client/` as its
output produces a bundle that cannot boot.

ts_bundle's Vite wrapper hardcodes exactly one `vite build --config`, and the
config it generates reads only `plugins` and `root` out of a user `vite_config`
-- it throws on any other key. A real framework config exceeds that. Hence a
rule of its own, whose `config` attr is the config Remix itself loads: `define`,
`resolve.alias`, `build.target` and the rest all reach the build.

Output: a single declare_directory holding what Remix wrote to its build
directory -- `client/`, `server/`, `.vite/` and `.remix/`.

Usage:

    load("@rules_typescript//ts:defs.bzl", "remix_build")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    node_modules(
        name = "node_modules",
        deps = [
            "@npm//:remix-run_dev",
            "@npm//:remix-run_node",
            "@npm//:remix-run_react",
            "@npm//:react",
            "@npm//:react-dom",
            "@npm//:vite",
        ],
    )

    remix_build(
        name = "app",
        srcs = glob(["app/**/*.tsx", "app/**/*.ts"]),
        config = "vite.config.mjs",
        node_modules = ":node_modules",
    )

And the config stays a plain, portable Remix config -- no Bazel-specific lines,
because the rule builds inside a staging root it owns and moves the result out:

    import { vitePlugin as remix } from "@remix-run/dev";

    export default { plugins: [remix()] };
"""

load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")
load("//ts/private:vite_config.bzl", "VITE_CONFIG_EXTENSIONS")

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

def _package_relative(short_path, pkg):
    """The staging-root-relative path for a file, relative to the rule's package."""
    if pkg and short_path.startswith(pkg + "/"):
        return short_path[len(pkg) + 1:]
    return short_path

def _remix_build_impl(ctx):
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "remix_build: no JS tool toolchain registered for '{}'.\n".format(ctx.label) +
            "Register one:\n" +
            "  register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )
    runtime_binary = js_tool.runtime_binary
    runtime_args = js_tool.args_prefix

    nm_files = ctx.attr.node_modules[DefaultInfo].files.to_list()
    if not nm_files:
        fail(
            "remix_build: 'node_modules' attr must be set to a node_modules() target " +
            "containing @remix-run/dev, @remix-run/node, @remix-run/react, react, " +
            "react-dom, vite and their transitive dependencies.\n" +
            "Example:\n" +
            "  node_modules(\n" +
            "      name = \"node_modules\",\n" +
            "      deps = [\"@npm//:remix-run_dev\", \"@npm//:vite\"],\n" +
            "  )",
        )
    node_modules_dir = nm_files[0]

    config_file = ctx.file.config
    tsconfig_file = ctx.file.tsconfig
    srcs = ctx.files.srcs
    staging_srcs_files = ctx.files.staging_srcs
    config_srcs_files = ctx.files.config_srcs

    out_dir = ctx.actions.declare_directory("{}_remix_out".format(ctx.label.name))

    pkg = ctx.label.package

    # Each line: <dest_rel_path> TAB <src_exec_path>, dest relative to the
    # staging root. srcs and the config are staged package-relative (the package
    # dir is the Remix project root); staging_srcs land at their
    # workspace-relative paths so relative imports across packages resolve.
    manifest_lines = []
    for src in srcs:
        manifest_lines.append("{}\t{}".format(_package_relative(src.short_path, pkg), src.path))
    for src in config_srcs_files:
        manifest_lines.append("{}\t{}".format(_package_relative(src.short_path, pkg), src.path))
    for src in staging_srcs_files:
        if src.short_path.startswith("../"):
            continue
        manifest_lines.append("{}\t{}".format(src.short_path, src.path))

    config_dest = _package_relative(config_file.short_path, pkg)
    manifest_lines.append("{}\t{}".format(config_dest, config_file.path))
    if tsconfig_file:
        manifest_lines.append("tsconfig.json\t{}".format(tsconfig_file.path))

    manifest = ctx.actions.declare_file("{}_remix_manifest.txt".format(ctx.label.name))
    ctx.actions.write(output = manifest, content = "\n".join(manifest_lines) + "\n")

    env_exports = ""
    for k, v in ctx.attr.env.items():
        env_exports += 'export {}="{}"\n'.format(k, _shell_escape(v))

    runtime_args_str = " ".join(['"{}"'.format(_shell_escape(a)) for a in runtime_args])

    wrapper_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated Remix build wrapper for " + str(ctx.label) + "\n" +
        "set -euo pipefail\n" +
        "\n" +
        'EXEC_ROOT="$(pwd)"\n' +
        'MANIFEST="${EXEC_ROOT}/' + _shell_escape(manifest.path) + '"\n' +
        'NM_ACTUAL="${EXEC_ROOT}/' + _shell_escape(node_modules_dir.path) + '"\n' +
        'OUT_DIR="${EXEC_ROOT}/' + _shell_escape(out_dir.path) + '"\n' +
        "\n" +
        "# The staging root lives inside OUT_DIR so it is always within the\n" +
        "# sandbox-writable subtree, on RBE as well as locally.\n" +
        'STAGING_DIR="${OUT_DIR}/_staging"\n' +
        'mkdir -p "${STAGING_DIR}"\n' +
        "\n" +
        "while IFS=$'\\t' read -r DEST SRC; do\n" +
        '  [[ -z "${DEST}" ]] && continue\n' +
        '  DEST_ABS="${STAGING_DIR}/${DEST}"\n' +
        '  mkdir -p "$(dirname "${DEST_ABS}")"\n' +
        '  cp -f "${EXEC_ROOT}/${SRC}" "${DEST_ABS}"\n' +
        'done < "${MANIFEST}"\n' +
        "\n" +
        "# Named node_modules, beside the staged config: Node walks up from the\n" +
        "# config to find it, and realpaths through it for the packages' own deps.\n" +
        'ln -sf "${NM_ACTUAL}" "${STAGING_DIR}/node_modules"\n' +
        "\n" +
        "# Remix reads the project package.json; without \"type\": \"module\" Node\n" +
        "# reads the ESM server bundle Remix emits as CommonJS.\n" +
        'if [[ ! -f "${STAGING_DIR}/package.json" ]]; then\n' +
        "  printf '{\"name\":\"%s\",\"version\":\"0.0.0\",\"private\":true,\"type\":\"module\"}\\n' " +
        '"' + _shell_escape(ctx.label.name) + '" > "${STAGING_DIR}/package.json"\n' +
        "fi\n" +
        "\n" +
        env_exports +
        "\n" +
        "# REMIX_ROOT is the plugin's own root hook; cd makes it Vite's root too,\n" +
        "# so the config needs no Bazel-specific `root`.\n" +
        'export REMIX_ROOT="${STAGING_DIR}"\n' +
        "\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        'RUNTIME="${EXEC_ROOT}/' + _shell_escape(runtime_binary.path) + '"\n' +
        'REMIX_CLI="${NM_ACTUAL}/@remix-run/dev/dist/cli.js"\n' +
        'if [[ ! -f "${REMIX_CLI}" ]]; then\n' +
        '  echo "remix_build: @remix-run/dev is not in the node_modules tree (${REMIX_CLI} missing)." >&2\n' +
        '  echo "Add \\"@npm//:remix-run_dev\\" to the node_modules target." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "\n" +
        'cd "${STAGING_DIR}"\n' +
        '"$RUNTIME" ${RUNTIME_ARGS[@]+"${RUNTIME_ARGS[@]}"} "${REMIX_CLI}" vite:build --config "' +
        _shell_escape(config_dest) + '" "${STAGING_DIR}"\n' +
        "\n" +
        'if [[ ! -d "${STAGING_DIR}/build" ]]; then\n' +
        '  echo "remix_build: remix vite:build wrote no build directory." >&2\n' +
        '  echo "The vite_config must not set the Remix plugin\'s buildDirectory: the rule" >&2\n' +
        '  echo "moves Remix\'s default <root>/build into the Bazel-declared output." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "shopt -s dotglob\n" +
        'mv "${STAGING_DIR}/build/"* "${OUT_DIR}/"\n' +
        "shopt -u dotglob\n" +
        'rm -rf "${STAGING_DIR}"\n' +
        "\n" +
        "for half in client server; do\n" +
        '  if [[ ! -d "${OUT_DIR}/${half}" ]]; then\n' +
        '    echo "remix_build: no ${half}/ directory in the build output." >&2\n' +
        '    echo "A Remix build with ssr: false deletes server/ and cannot serve a loader," >&2\n' +
        '    echo "an action, or a resource route. Drop ssr: false from the vite_config." >&2\n' +
        "    exit 1\n" +
        "  fi\n" +
        "done\n"
    )

    wrapper = ctx.actions.declare_file("{}_remix_build_wrapper.sh".format(ctx.label.name))
    ctx.actions.write(output = wrapper, content = wrapper_content, is_executable = True)

    direct_inputs = (
        [manifest, wrapper, runtime_binary, config_file] +
        srcs + staging_srcs_files + config_srcs_files + nm_files
    )
    if tsconfig_file:
        direct_inputs.append(tsconfig_file)

    ctx.actions.run(
        inputs = depset(direct_inputs),
        outputs = [out_dir],
        executable = wrapper,
        mnemonic = "RemixBuild",
        progress_message = "RemixBuild %{label}",
    )

    return [DefaultInfo(files = depset([out_dir]))]

remix_build = rule(
    implementation = _remix_build_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "The Remix application sources: app/root.tsx, app/entry.client.tsx, " +
                  "app/entry.server.tsx, app/routes/**, and anything they import from " +
                  "inside this package.",
            allow_files = True,
            mandatory = True,
        ),
        "staging_srcs": attr.label_list(
            doc = "Filegroup targets whose files are staged at their " +
                  "workspace-relative paths, for sources the app imports from " +
                  "other packages. Same contract as next_build's staging_srcs.",
            allow_files = True,
        ),
        "config": attr.label(
            doc = "The Vite config whose default export carries the Remix plugin " +
                  "(`vitePlugin as remix` from @remix-run/dev). Remix loads this " +
                  "file itself, so every Vite option in it reaches the build. It " +
                  "must NOT set the plugin's `buildDirectory`, which the rule owns.",
            allow_single_file = VITE_CONFIG_EXTENSIONS,
            mandatory = True,
        ),
        "config_srcs": attr.label_list(
            doc = "Local modules the config imports, staged at their paths " +
                  "relative to this package so `./plugins/foo` resolves.",
            allow_files = True,
        ),
        "tsconfig": attr.label(
            doc = "An optional tsconfig.json, staged at the project root.",
            allow_single_file = True,
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target containing @remix-run/dev and the app's " +
                  "dependencies. Required.",
            allow_files = True,
            mandatory = True,
        ),
        "env": attr.string_dict(
            doc = "Additional environment variables exported for the build.",
            default = {},
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    doc = """Builds a Remix application with `remix vite:build`, both halves.

Produces one directory artifact holding what Remix wrote to its build
directory:

    client/index.html              (SPA fallback, when the app has one)
    client/assets/*.js             route chunks, the client entry, shared chunks
    client/assets/manifest-*.js    window.__remixManifest -- emitted by the
                                   SERVER half and moved here afterwards
    server/index.js                the request handler build
    .remix/manifest.json           route id -> file / path / parentId
    .vite/{client,server}-manifest.json  when the config sets build.manifest

Both halves come from one action, because they are not independent: the SSR
half moves assets into `client/` after the client half has run. The rule fails
rather than declaring a partial output when either half is missing -- which is
what a config with `ssr: false` produces, and why SPA mode cannot serve a
loader, an action, or a resource route.

Requires the js_tool toolchain (Node.js on the exec platform).
""",
)
