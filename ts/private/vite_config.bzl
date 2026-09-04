"""Staging for a user-supplied Vite config and the modules it imports.

Two problems, one shared by both rules that accept a `vite_config`.

The config cannot be loaded from the source tree. The generated config imports
it by path, and Node resolves that path to its real location before resolving
the config's own bare imports -- so a source-tree config looks for its npm
dependencies in a source-tree `node_modules`, which this ruleset does not have.
The fix is to load a copy under bazel-bin, beside the tree the `node_modules`
attr built.

And one file is not a config. A real one imports local plugin modules, and
copying only the entry leaves those relative imports pointing at nothing. So
`vite_config_srcs` declares them and they are staged together, each at its path
relative to the entry config's package, which is what makes `./plugins/foo`
resolve inside the staged tree the same way it does in the source tree.

A file outside that package would stage above the staging root, so it is
rejected rather than silently flattened.
"""

def _package_relative(ctx, file, config_package):
    """The path to stage `file` at, relative to the staging root."""
    short = file.short_path
    if not config_package:
        return short
    prefix = config_package + "/"
    if not short.startswith(prefix):
        fail(
            "{}: vite_config_srcs contains '{}', which is not under the vite_config's\n".format(
                ctx.label,
                short,
            ) +
            "package ('{}').\n".format(config_package) +
            "The config and the modules it imports are staged relative to that package, " +
            "so a file above it has nowhere to land and its relative import would not " +
            "resolve.\nMove it under '{}', or import it as a bare npm specifier through ".format(config_package) +
            "a package in the node_modules tree.",
        )
    return short[len(prefix):]

def stage_vite_config(ctx, config_file, extra_srcs, subdir):
    """Copies a vite_config and its local imports into bazel-bin.

    Args:
        ctx: The rule context.
        config_file: File, the entry config named by the vite_config attr.
        extra_srcs: list of File, the modules it imports (vite_config_srcs).
        subdir: string, the directory under the target's output root to stage in.

    Returns:
        struct(entry = File, files = list of File): the staged entry config, and
        every staged file for the action or runfiles to carry.
    """
    if not config_file:
        return struct(entry = None, files = [])

    config_package = ctx.label.package
    if not config_file.short_path.startswith(config_package + "/") and config_package:
        # A config from another package still stages, but its own package is the
        # root, so its siblings keep resolving relative to it.
        config_package = config_file.short_path.rsplit("/", 1)[0] if "/" in config_file.short_path else ""

    # Nothing above the staging root says what module system these files are in,
    # so Node and Vite's native config loader both read them as CommonJS and the
    # ESM syntax in them becomes a warning today and an error once that loader is
    # the default. The manifest is what the staged tree is missing, not a
    # workaround: it is the same file that makes the source tree unambiguous.
    manifest = ctx.actions.declare_file("{}/package.json".format(subdir))
    ctx.actions.write(output = manifest, content = '{"type": "module"}\n')

    staged = [manifest]
    entry = None
    for f in [config_file] + extra_srcs:
        rel = _package_relative(ctx, f, config_package)
        out = ctx.actions.declare_file("{}/{}".format(subdir, rel))
        ctx.actions.expand_template(
            template = f,
            output = out,
            substitutions = {},
        )
        staged.append(out)
        if f == config_file:
            entry = out
    return struct(entry = entry, files = staged)

# A .ts config cannot be reached by a plain dynamic import: Node does not load
# TypeScript. Vite's own loader does -- it is what Vite runs on a root config --
# and it also resolves the extensionless relative imports that bundler-resolution
# configs are written with. So that is tried first, and the dynamic import is the
# fallback for a plain .mjs when vite is not in the tree.
# What the generated config actually reads out of a user vite_config. Everything
# else it would silently discard, and a real framework config sets several of
# them -- `define`, `resolve.alias`, `build.target`, `optimizeDeps` -- so the
# build fails naming them rather than producing a bundle that quietly ignores
# half its configuration.
#
# The check runs where the config is loaded rather than at analysis time, because
# only the loaded object says what keys it has.
def unhandled_keys_js(honoured, label):
    return (
        "const _honoured = " + json.encode(sorted(honoured)) + ";\n" +
        "if (_userCfg) {\n" +
        "  const _unhandled = Object.keys(_userCfg).filter(\n" +
        "    (key) => !_honoured.includes(key),\n" +
        "  );\n" +
        "  if (_unhandled.length) {\n" +
        "    throw new Error(\n" +
        "      '[rules_typescript] " + label + ": the vite_config sets ' +\n" +
        "        _unhandled.join(', ') +\n" +
        "        ', which the generated config does not read. Only ' +\n" +
        "        _honoured.join(', ') + ' reach the build; the rest would be\\n' +\n" +
        "        'silently discarded. Move what you need into a plugin, or open an\\n' +\n" +
        "        'issue for the option.',\n" +
        "    );\n" +
        "  }\n" +
        "}\n"
    )

LOAD_USER_CONFIG_JS = (
    "// The user's vite_config, staged under bazel-bin so its own bare imports\n" +
    "// resolve beside the Bazel npm tree rather than in the source tree.\n" +
    "const userConfigPath = process.env['VITE_USER_CONFIG_PATH'];\n" +
    "let _userPlugins = [];\n" +
    "if (userConfigPath) {\n" +
    "  let _userCfg = null;\n" +
    "  let _viteLoadErr = null;\n" +
    "  try {\n" +
    "    const { loadConfigFromFile } = await import('vite');\n" +
    "    const _loaded = await loadConfigFromFile(\n" +
    "      { command: 'serve', mode: 'development' },\n" +
    "      userConfigPath,\n" +
    "    );\n" +
    "    _userCfg = _loaded && _loaded.config;\n" +
    "  } catch (err) {\n" +
    "    _viteLoadErr = err;\n" +
    "  }\n" +
    "  if (!_userCfg) {\n" +
    "    try {\n" +
    "      const _userMod = await import(userConfigPath);\n" +
    "      _userCfg = _userMod.default || _userMod;\n" +
    "    } catch (err) {\n" +
    "      const _detail = _viteLoadErr\n" +
    "        ? err.message + ' (vite loader: ' + _viteLoadErr.message + ')'\n" +
    "        : err.message;\n" +
    "      throw new Error('[rules_typescript] Failed to load vite_config: ' + _detail);\n" +
    "    }\n" +
    "  }\n" +
    "  if (_userCfg && Array.isArray(_userCfg.plugins)) {\n" +
    "    _userPlugins = _userCfg.plugins.flat(Infinity).filter(Boolean);\n" +
    "  }\n" +
    unhandled_keys_js(["plugins"], "ts_dev_server") +
    "}\n" +
    "\n"
)

VITE_CONFIG_EXTENSIONS = [".mjs", ".js", ".mts", ".ts"]

VITE_CONFIG_SRCS_DOC = (
    "The local modules the vite_config imports. The config and these are staged " +
    "together under bazel-bin, each at its path relative to the config's package, " +
    "so a relative import resolves in the staged tree exactly as it does in the " +
    "source tree. Without this attr only the config itself is staged and its " +
    "relative imports fail, naming the file. A file outside the config's package " +
    "is an error: it would have to stage above the staging root."
)
