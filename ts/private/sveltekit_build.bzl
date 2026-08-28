"""SvelteKit build rule for rules_typescript.

sveltekit_build wraps `vite build` with SvelteKit's own Vite plugin as a single
opaque Bazel action, producing both halves of a SvelteKit application: the
browser bundle and the server request handler.

Why this cannot be a ts_bundle
------------------------------

SvelteKit reads `process.cwd()`, and only `process.cwd()`. `load_config()` globs
`<cwd>/svelte.config.{js,ts}` and then resolves every `kit.files.*` entry --
`src/app.html`, `src/routes`, `src/lib`, `static` -- against that same cwd. It
does the work from Vite's `config` hook, before a single module is transformed:
it scans the route tree off disk and writes `<cwd>/.svelte-kit`.

Nothing redirects that. The plugin's own `config` hook returns `root: cwd` and
only *warns* when the host config set `root` to something else, so ts_bundle's
staging-root redirection is inert here: an app staged anywhere but cwd fails
with `src/app.html does not exist` no matter what `root` says. Hosting SvelteKit
in ts_bundle would mean changing where the shared Vite wrapper `cd`s, for every
framework, to serve this one. Hence a rule of its own, which owns a staging root
and makes it the cwd.

The plugin also replaces `build.outDir`, `build.rollupOptions.input`, the output
filename patterns, `base`, `publicDir`, `build.manifest` and `build.ssr` with
its own values, and one `vite build` runs two passes -- the SSR half's
`writeBundle` triggers the client half. So the real bundle lands under
`.svelte-kit/output/`, not anywhere a caller declared; the rule moves it into
the Bazel-declared output afterwards.

Output: a single declare_directory holding what SvelteKit wrote to
`.svelte-kit/output` -- `client/` and `server/`.

Usage:

    load("@rules_typescript//ts:defs.bzl", "sveltekit_build")
    load("@rules_typescript//npm:defs.bzl", "node_modules")

    node_modules(
        name = "node_modules",
        deps = [
            "@npm//:sveltejs_kit",
            "@npm//:sveltejs_vite-plugin-svelte",
            "@npm//:svelte",
            "@npm//:vite",
        ],
    )

    sveltekit_build(
        name = "app",
        srcs = glob(["src/**"]),
        svelte_config = "svelte.config.js",
        config = "vite.config.mjs",
        node_modules = ":node_modules",
    )

One glob over the whole tree, not one pattern per extension: src/app.html and
the route tree are read off disk rather than through imports, and a pattern
matching nothing fails the glob outright.

A glob does not descend into a subpackage, so a BUILD file anywhere under the
globbed tree removes that directory from `srcs` -- and a SvelteKit build whose
route tree lost a directory reports success for the routes that survived.
`sveltekit_build` is a macro for that reason: it asks `native.subpackages()`
which of those directories are packages and fails naming them, before the glob's
hole can reach a build. What the glob does deliver is cross-checked against
SvelteKit's own record afterwards -- every staged `+page*`/`+server*` file must
appear in `server/.vite/manifest.json`.

Both configs stay plain and portable -- no Bazel-specific lines. Pin
kit.version.name: unpinned it is a build timestamp, hashed into every chunk
name, so no two builds agree and nothing downstream ever cache-hits.

    // svelte.config.js
    export default { kit: { version: { name: "1" } } };

    // vite.config.mjs
    import { sveltekit } from "@sveltejs/kit/vite";
    export default { plugins: [await sveltekit()] };
"""

load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")

# The Vite config sits here rather than at the staging root, because Vite's
# config loader mkdirs `.vite-temp` in the nearest node_modules above the
# config -- which at the staging root is the read-only Bazel tree artifact.
_VITE_CONFIG_DIR = ".bazel_vite"

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

# SvelteKit reserves a leading `+` for its own route files, and nothing else in
# a project uses one.
def _is_route_file(basename):
    return basename.startswith("+")

def _is_route_producing(basename):
    return basename.startswith("+page") or basename.startswith("+server")

def _package_relative(short_path, pkg):
    """The staging-root-relative path for a file, relative to the rule's package."""
    if pkg and short_path.startswith(pkg + "/"):
        return short_path[len(pkg) + 1:]
    return short_path

def _sveltekit_build_impl(ctx):
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "sveltekit_build: no JS tool toolchain registered for '{}'.\n".format(ctx.label) +
            "Register one:\n" +
            "  register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )
    runtime_binary = js_tool.runtime_binary
    runtime_args = js_tool.args_prefix

    nm_files = ctx.attr.node_modules[DefaultInfo].files.to_list()
    if not nm_files:
        fail(
            "sveltekit_build: 'node_modules' attr must be set to a node_modules() target " +
            "containing @sveltejs/kit, @sveltejs/vite-plugin-svelte, svelte, vite and " +
            "their transitive dependencies.\n" +
            "Example:\n" +
            "  node_modules(\n" +
            "      name = \"node_modules\",\n" +
            "      deps = [\"@npm//:sveltejs_kit\", \"@npm//:svelte\", \"@npm//:vite\"],\n" +
            "  )",
        )
    node_modules_dir = nm_files[0]

    svelte_config = ctx.file.svelte_config
    svelte_config_name = svelte_config.basename
    if svelte_config_name.endswith(".ts"):
        fail(
            "sveltekit_build: svelte_config = {} is a .ts file.\n".format(svelte_config_name) +
            "SvelteKit's load_config() does glob svelte.config.{js,ts}, but it imports what " +
            "it finds through Node, and the toolchain Node cannot load a .ts file: the build " +
            "dies with ERR_UNKNOWN_FILE_EXTENSION. Write the config in JavaScript.",
        )
    if not svelte_config_name.endswith(".js"):
        fail(
            "sveltekit_build: svelte_config = {} is not a .js file.\n".format(svelte_config_name) +
            "This rule stages the config as svelte.config.js, so any other extension would " +
            "build here and nowhere else -- SvelteKit's own load_config() globs " +
            "svelte.config.{js,ts} at its cwd and would never find it outside Bazel.",
        )

    config_file = ctx.file.config
    tsconfig_file = ctx.file.tsconfig
    srcs = ctx.files.srcs
    staging_srcs_files = ctx.files.staging_srcs
    config_srcs_files = ctx.files.config_srcs

    out_dir = ctx.actions.declare_directory("{}_sveltekit_out".format(ctx.label.name))

    pkg = ctx.label.package

    # Each line: <dest_rel_path> TAB <src_exec_path>, dest relative to the
    # staging root. srcs and the svelte config are staged package-relative (the
    # package dir is the SvelteKit project root, which is also the cwd);
    # staging_srcs land at their workspace-relative paths so relative imports
    # across packages resolve.
    manifest_lines = []
    staged_routes = []
    for src in srcs:
        dest = _package_relative(src.short_path, pkg)
        manifest_lines.append("{}\t{}".format(dest, src.path))
        if _is_route_file(src.basename):
            staged_routes.append(dest)
    for src in staging_srcs_files:
        if src.short_path.startswith("../"):
            continue
        manifest_lines.append("{}\t{}".format(src.short_path, src.path))
        if _is_route_file(src.basename):
            staged_routes.append(src.short_path)

    if not [r for r in staged_routes if _is_route_producing(r.rsplit("/", 1)[-1])]:
        fail(
            "sveltekit_build: srcs carries no SvelteKit route file.\n" +
            "A build with no route tree staged still exits 0: SvelteKit emits its " +
            "entries/fallbacks either way. srcs must carry src/routes/** and src/app.html, " +
            "which the build reads off disk at the staging root rather than through imports.",
        )

    manifest_lines.append("svelte.config.js\t{}".format(svelte_config.path))

    config_dest = "{}/{}".format(_VITE_CONFIG_DIR, config_file.basename)
    manifest_lines.append("{}\t{}".format(config_dest, config_file.path))
    for src in config_srcs_files:
        manifest_lines.append("{}/{}\t{}".format(
            _VITE_CONFIG_DIR,
            _package_relative(src.short_path, pkg),
            src.path,
        ))
    if tsconfig_file:
        manifest_lines.append("tsconfig.json\t{}".format(tsconfig_file.path))

    manifest = ctx.actions.declare_file("{}_sveltekit_manifest.txt".format(ctx.label.name))
    ctx.actions.write(output = manifest, content = "\n".join(manifest_lines) + "\n")

    routes_list = ctx.actions.declare_file("{}_sveltekit_routes.txt".format(ctx.label.name))
    ctx.actions.write(output = routes_list, content = "".join([r + "\n" for r in staged_routes]))

    env_exports = ""
    for k, v in ctx.attr.env.items():
        env_exports += 'export {}="{}"\n'.format(k, _shell_escape(v))

    runtime_args_str = " ".join(['"{}"'.format(_shell_escape(a)) for a in runtime_args])

    wrapper_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated SvelteKit build wrapper for " + str(ctx.label) + "\n" +
        "set -euo pipefail\n" +
        "\n" +
        'EXEC_ROOT="$(pwd)"\n' +
        'MANIFEST="${EXEC_ROOT}/' + _shell_escape(manifest.path) + '"\n' +
        'ROUTES_LIST="${EXEC_ROOT}/' + _shell_escape(routes_list.path) + '"\n' +
        'NM_ACTUAL="${EXEC_ROOT}/' + _shell_escape(node_modules_dir.path) + '"\n' +
        'OUT_DIR="${EXEC_ROOT}/' + _shell_escape(out_dir.path) + '"\n' +
        "\n" +
        "# The staging root lives inside OUT_DIR so it is always within the\n" +
        "# sandbox-writable subtree, on RBE as well as locally.\n" +
        'STAGING_DIR="${OUT_DIR}/_staging"\n' +
        'mkdir -p "${STAGING_DIR}/' + _VITE_CONFIG_DIR + '/node_modules"\n' +
        "\n" +
        "while IFS=$'\\t' read -r DEST SRC; do\n" +
        '  [[ -z "${DEST}" ]] && continue\n' +
        '  DEST_ABS="${STAGING_DIR}/${DEST}"\n' +
        '  mkdir -p "$(dirname "${DEST_ABS}")"\n' +
        '  cp -f "${EXEC_ROOT}/${SRC}" "${DEST_ABS}"\n' +
        'done < "${MANIFEST}"\n' +
        "\n" +
        "# Named node_modules, at the cwd SvelteKit builds from: Node realpaths\n" +
        "# through it for each package's own dependencies.\n" +
        'ln -sf "${NM_ACTUAL}" "${STAGING_DIR}/node_modules"\n' +
        "\n" +
        "# Synthesised because the staging root has no package.json of its own;\n" +
        '# "type": "module" is what a SvelteKit project declares, so .js in the\n' +
        "# staged tree is ESM the way it is in the user's own checkout.\n" +
        'if [[ ! -f "${STAGING_DIR}/package.json" ]]; then\n' +
        "  printf '{\"name\":\"%s\",\"version\":\"0.0.0\",\"private\":true,\"type\":\"module\"}\\n' " +
        '"' + _shell_escape(ctx.label.name) + '" > "${STAGING_DIR}/package.json"\n' +
        "fi\n" +
        "\n" +
        env_exports +
        "\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        'RUNTIME="${EXEC_ROOT}/' + _shell_escape(runtime_binary.path) + '"\n' +
        'VITE_CLI="${NM_ACTUAL}/vite/bin/vite.js"\n' +
        'if [[ ! -f "${VITE_CLI}" ]]; then\n' +
        '  echo "sveltekit_build: vite is not in the node_modules tree (${VITE_CLI} missing)." >&2\n' +
        '  echo "Add \\"@npm//:vite\\" to the node_modules target." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        'if [[ ! -d "${NM_ACTUAL}/@sveltejs/kit" ]]; then\n' +
        '  echo "sveltekit_build: @sveltejs/kit is not in the node_modules tree." >&2\n' +
        '  echo "Add \\"@npm//:sveltejs_kit\\" to the node_modules target." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "\n" +
        "# cwd is the whole contract: SvelteKit finds its config, app.html and\n" +
        "# route tree here, and writes .svelte-kit here.\n" +
        'cd "${STAGING_DIR}"\n' +
        '"$RUNTIME" ${RUNTIME_ARGS[@]+"${RUNTIME_ARGS[@]}"} "${VITE_CLI}" build --config "' +
        _shell_escape(config_dest) + '"\n' +
        "\n" +
        'if [[ ! -d "${STAGING_DIR}/.svelte-kit/output" ]]; then\n' +
        '  echo "sveltekit_build: the build wrote no .svelte-kit/output directory." >&2\n' +
        '  echo "The vite_config must carry SvelteKit\'s own plugin:" >&2\n' +
        '  echo "  import { sveltekit } from \\"@sveltejs/kit/vite\\";" >&2\n' +
        '  echo "  export default { plugins: [await sveltekit()] };" >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "shopt -s dotglob\n" +
        'mv "${STAGING_DIR}/.svelte-kit/output/"* "${OUT_DIR}/"\n' +
        "shopt -u dotglob\n" +
        'rm -rf "${STAGING_DIR}"\n' +
        "\n" +
        "for half in client server; do\n" +
        '  if [[ ! -d "${OUT_DIR}/${half}" ]]; then\n' +
        '    echo "sveltekit_build: no ${half}/ directory in the build output." >&2\n' +
        "    exit 1\n" +
        "  fi\n" +
        "done\n" +
        "\n" +
        "# A build that lost part of its route tree exits 0 with a normal-looking\n" +
        "# summary, so the staged route files are checked against the framework's\n" +
        "# own record of what it compiled. Vite's manifest keys are the staged\n" +
        "# paths verbatim, which is what makes the comparison exact.\n" +
        'VITE_MANIFEST="${OUT_DIR}/server/.vite/manifest.json"\n' +
        'if [[ ! -f "${VITE_MANIFEST}" ]]; then\n' +
        '  echo "sveltekit_build: the server build wrote no .vite/manifest.json, so the" >&2\n' +
        '  echo "staged routes could not be checked against the ones SvelteKit compiled." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "UNCOMPILED=()\n" +
        "while IFS= read -r ROUTE; do\n" +
        '  [[ -z "${ROUTE}" ]] && continue\n' +
        '  grep -Fq "\\"${ROUTE}\\":" "${VITE_MANIFEST}" || UNCOMPILED+=("${ROUTE}")\n' +
        'done < "${ROUTES_LIST}"\n' +
        "if (( ${#UNCOMPILED[@]} > 0 )); then\n" +
        '  echo "sveltekit_build: ${#UNCOMPILED[@]} staged route file(s) are missing from the" >&2\n' +
        '  echo "build SvelteKit produced:" >&2\n' +
        '  printf \'    %s\\n\' "${UNCOMPILED[@]}" >&2\n' +
        '  echo "SvelteKit scans the route tree off disk and compiles what it finds, so a" >&2\n' +
        '  echo "route it did not compile is one it did not see: check kit.files.routes in" >&2\n' +
        '  echo "svelte.config.js against where these files were staged." >&2\n' +
        "  exit 1\n" +
        "fi\n" +
        "\n" +
        "# kit.version.name defaults to Date.now(), which is hashed into every\n" +
        "# client chunk name: unpinned, no two builds agree and nothing ever\n" +
        "# cache-hits. Visible beats silent, so say so rather than failing.\n" +
        'VERSION_JSON="${OUT_DIR}/client/_app/version.json"\n' +
        'if [[ -f "${VERSION_JSON}" ]] && grep -Eq \'"version":"[0-9]{13}"\' "${VERSION_JSON}"; then\n' +
        '  echo "sveltekit_build: WARNING: kit.version.name reads as epoch-milliseconds," >&2\n' +
        '  echo "which is what leaving it unpinned produces. Every build then emits different" >&2\n' +
        '  echo "chunk names and no cache hit is possible. Pin it in svelte.config.js:" >&2\n' +
        '  echo "  kit: { version: { name: \\"1\\" } }" >&2\n' +
        "fi\n"
    )

    wrapper = ctx.actions.declare_file("{}_sveltekit_build_wrapper.sh".format(ctx.label.name))
    ctx.actions.write(output = wrapper, content = wrapper_content, is_executable = True)

    direct_inputs = (
        [manifest, routes_list, wrapper, runtime_binary, config_file, svelte_config] +
        srcs + staging_srcs_files + config_srcs_files + nm_files
    )
    if tsconfig_file:
        direct_inputs.append(tsconfig_file)

    # SvelteKit itself needs no network, but a vite_config plugin is arbitrary
    # code -- font fetchers, image optimisers -- so the sandbox takes the option
    # away rather than the rule trusting every caller's plugin list.
    execution_requirements = {"block-network": ""}
    if ctx.attr.allow_network:
        execution_requirements = {"requires-network": ""}

    ctx.actions.run(
        inputs = depset(direct_inputs),
        outputs = [out_dir],
        executable = wrapper,
        mnemonic = "SvelteKitBuild",
        progress_message = "SvelteKitBuild %{label}",
        execution_requirements = execution_requirements,
    )

    return [DefaultInfo(files = depset([out_dir]))]

_sveltekit_build = rule(
    implementation = _sveltekit_build_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = "The SvelteKit application sources: src/app.html, the route tree " +
                  "under src/routes/ (.svelte and .ts alike), src/lib/** and anything " +
                  "they import from inside this package. SvelteKit reads app.html and " +
                  "scans the route tree off disk, so both must be listed here even " +
                  "though nothing imports them.",
            allow_files = True,
            mandatory = True,
        ),
        "staging_srcs": attr.label_list(
            doc = "Filegroup targets whose files are staged at their " +
                  "workspace-relative paths, for sources the app imports from " +
                  "other packages. Same contract as next_build's staging_srcs.",
            allow_files = True,
        ),
        "svelte_config": attr.label(
            doc = "svelte.config.js. The rule stages it under that name, so a .mjs " +
                  "would build here and nowhere else, and a .ts cannot be loaded by " +
                  "the toolchain Node at all -- both are rejected at analysis time. " +
                  "Pin kit.version.name here: unpinned it is a build timestamp, and " +
                  "no two builds produce the same chunk names.",
            allow_single_file = True,
            mandatory = True,
        ),
        "config": attr.label(
            doc = "The Vite config whose default export carries SvelteKit's plugin " +
                  "(`sveltekit` from @sveltejs/kit/vite). The rule stages it in a " +
                  "subdirectory of the project root, so its relative imports resolve " +
                  "against config_srcs rather than against the app sources.",
            allow_single_file = True,
            mandatory = True,
        ),
        "config_srcs": attr.label_list(
            doc = "Local modules the Vite config imports, staged beside it.",
            allow_files = True,
        ),
        "tsconfig": attr.label(
            doc = "An optional tsconfig.json, staged at the project root.",
            allow_single_file = True,
        ),
        "node_modules": attr.label(
            doc = "A node_modules() target containing @sveltejs/kit, " +
                  "@sveltejs/vite-plugin-svelte, svelte, vite and the app's " +
                  "dependencies. Required.",
            allow_files = True,
            mandatory = True,
        ),
        "env": attr.string_dict(
            doc = "Additional environment variables exported for the build.",
            default = {},
        ),
        "allow_network": attr.bool(
            doc = "Let the build reach the network. The action is otherwise run " +
                  "with `block-network`, which fails any build that depends on a " +
                  "download -- a Vite plugin fetching Google Fonts is the usual " +
                  "one. Setting this to True makes the output depend on a remote " +
                  "host, so the same inputs no longer imply the same output.",
            default = False,
        ),
    },
    toolchains = [
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    doc = """Builds a SvelteKit application with SvelteKit's own Vite plugin.

Produces one directory artifact holding what SvelteKit wrote to
`.svelte-kit/output`:

    client/_app/immutable/{entry,nodes,chunks,assets}/**   hashed browser assets
    client/_app/version.json                               kit.version.name
    client/.vite/manifest.json
    server/index.js                                        the request handler
    server/entries/pages/**                                one per route node
    server/entries/fallbacks/{layout,error}.svelte.js
    server/chunks/**, server/.vite/manifest.json

One action, because one `vite build` is two passes: the SSR half's `writeBundle`
triggers the client half, and neither is independently cacheable.

Both halves come out of a staging root the rule owns and makes the process cwd,
which is the only thing SvelteKit looks at. The rule fails rather than declaring
a partial output when a half is missing, and fails when a staged `+page`/`+server`
file is absent from `server/.vite/manifest.json` -- which is what a route tree
SvelteKit never saw produces, silently and with exit code 0.

No adapter is run: the output is SvelteKit's own build, not a
platform-specific deployment bundle.

Requires the js_tool toolchain (Node.js on the exec platform).
""",
)

def sveltekit_build(name, srcs = None, allow_subpackages = [], **kwargs):
    """Builds a SvelteKit application, guarding the `srcs` glob against subpackages.

    A wrapper around the `sveltekit_build` rule. `srcs` is conventionally one
    `glob()` over the project tree, and a glob does not descend into a
    subpackage: a BUILD file under that tree silently removes its whole
    directory from `srcs`, and SvelteKit then compiles the route tree minus that
    directory and reports success. This macro asks `native.subpackages()` which
    of the globbed directories are packages of their own and fails naming them.

    Args:
        name: Target name.
        srcs: The application sources, conventionally `glob(["src/**"])`.
        allow_subpackages: Directories under the globbed tree that are Bazel
            packages of their own, accepted as holes in `srcs`. Whatever the app
            imports from them has to reach the staging root through
            `staging_srcs` instead.
        **kwargs: Passed to the `sveltekit_build` rule.
    """
    for hole in _glob_holes(srcs, allow_subpackages):
        fail(
            "sveltekit_build(name = \"{}\"): {}/BUILD.bazel makes {} a Bazel package, ".format(
                name,
                hole,
                hole,
            ) +
            "and the srcs glob does not descend into one.\n" +
            "Every file under it is missing from srcs, so the build would stage a tree with " +
            "a hole in it -- SvelteKit scans the route tree off disk, finds only what was " +
            "staged, and reports success for the routes that survived. Delete that BUILD " +
            "file, or add \"{}\" to allow_subpackages if its contents reach ".format(hole) +
            "the app through staging_srcs instead.",
        )

    _sveltekit_build(name = name, srcs = srcs, **kwargs)

def _glob_holes(srcs, allowed):
    """The subpackages under the top-level directories `srcs` was globbed from."""
    if type(srcs) != "list":
        return []

    roots = {}
    for src in srcs:
        if type(src) != "string" or src[:1] in [":", "/", "@"]:
            continue
        segments = src.split("/")
        if len(segments) > 1:
            roots[segments[0]] = None
    if not roots:
        return []

    subpackages = native.subpackages(
        include = [root + "/**" for root in sorted(roots)],
        allow_empty = True,
    )
    return [pkg for pkg in subpackages if pkg not in allowed]
