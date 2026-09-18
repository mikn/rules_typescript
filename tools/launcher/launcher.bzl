"""Wiring for the Go launcher the launcher toolchain resolves to.

An executable rule writes one JSON config and points its executable at a
symlink of the launcher.  Nothing generates shell text, so there is no quoting
layer to get wrong, and every path is resolved at runtime through the runfiles
library (which handles manifest-only layouts).
"""

load(
    "//ts/private:toolchain.bzl",
    "LAUNCHER_TOOLCHAIN_TYPE",
    "TSGO_PLATFORMS",
    "get_launcher_toolchain",
)

LAUNCHER_TOOLCHAINS = [
    config_common.toolchain_type(LAUNCHER_TOOLCHAIN_TYPE, mandatory = False),
]

def rlocation_path(ctx, file):
    """Returns the runfiles path of `file`, in the form the runfiles library accepts.

    Args:
        ctx: the rule context, for the workspace name.
        file: the File to locate.

    Returns:
        A "repo/path/to/file" string.
    """
    if file.short_path.startswith("../"):
        return file.short_path[3:]
    return ctx.workspace_name + "/" + file.short_path

def declare_launcher(ctx, config, basename = None):
    """Writes a launcher config and the launcher symlink that reads it.

    The rule declares LAUNCHER_TOOLCHAINS and `fragments = ["platform"]`; a
    target platform no launcher toolchain covers fails here naming it.

    Args:
        ctx: the rule context.
        config: the config dict, serialised as the launcher's JSON contract.
        basename: name of the executable; defaults to "<target>_launcher".

    Returns:
        A struct with the `executable` File, its `config` File, the `files`
        that must reach runfiles, and the `root_symlinks` dict that stages the
        config where the launcher can find it however it was started.
    """
    toolchain = get_launcher_toolchain(ctx)
    if toolchain == None:
        fail(("{}: no launcher toolchain resolved for the target platform " +
              "{}; rules_typescript ships the launcher for {} " +
              "(COMPATIBILITY.md#platforms).").format(
            ctx.label,
            ctx.fragments.platform.platform,
            ", ".join(TSGO_PLATFORMS),
        ))
    launcher = toolchain.launcher
    base = basename if basename else "{}_launcher".format(ctx.label.name)
    executable = ctx.actions.declare_file(base)
    config_file = ctx.actions.declare_file(base + ".json")

    ctx.actions.write(
        output = config_file,
        content = json.encode_indent(config, indent = "  "),
    )

    # One launcher binary for every target; the config beside the symlink is
    # per-target.
    ctx.actions.symlink(
        output = executable,
        target_file = launcher,
        is_executable = True,
    )

    # At the runfiles root, under the launcher's own basename: the one place
    # reachable from `bazel run`, `bazel test`, and from another rule's action
    # (where argv[0] is an exec path with nothing beside it).
    return struct(
        executable = executable,
        config = config_file,
        files = [executable, config_file, launcher],
        root_symlinks = {base + ".json": config_file},
    )
