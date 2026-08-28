"""Wiring for the checked-in Go launcher, //tools/launcher:ts_launcher.

An executable rule writes one JSON config and points its executable at a
symlink of that single prebuilt launcher.  Nothing generates shell text, so
there is no quoting layer to get wrong, and every path is resolved at runtime
through the runfiles library (which handles manifest-only layouts).
"""

LAUNCHER_ATTRS = {
    "_launcher": attr.label(
        default = Label("//tools/launcher:ts_launcher"),
        allow_single_file = True,
        doc = "The Go launcher every rules_typescript executable runs.",
    ),
}

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

    Args:
        ctx: the rule context.
        config: the config dict, serialised as the launcher's JSON contract.
        basename: name of the executable; defaults to "<target>_launcher".

    Returns:
        A struct with the `executable` File, its `config` File, the `files`
        that must reach runfiles, and the `root_symlinks` dict that stages the
        config where the launcher can find it however it was started.
    """
    base = basename if basename else "{}_launcher".format(ctx.label.name)
    executable = ctx.actions.declare_file(base)
    config_file = ctx.actions.declare_file(base + ".json")

    ctx.actions.write(
        output = config_file,
        content = json.encode_indent(config, indent = "  "),
    )

    # The launcher is one prebuilt binary shared by every target; only the
    # config next to this symlink is per-target.
    ctx.actions.symlink(
        output = executable,
        target_file = ctx.file._launcher,
        is_executable = True,
    )

    # At the runfiles root, under the launcher's own basename: the one place
    # reachable from `bazel run`, `bazel test`, and from another rule's action
    # (where argv[0] is an exec path with nothing beside it).
    return struct(
        executable = executable,
        config = config_file,
        files = [executable, config_file, ctx.file._launcher],
        root_symlinks = {base + ".json": config_file},
    )
