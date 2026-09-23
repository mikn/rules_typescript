"""Read-only formatting validation for program and explicitly selected source files."""

load("//ts/private:toolchain.bzl", "TOOLS_TOOLCHAIN_TYPE", "get_tools_toolchain")
load(":tsgo.bzl", "program_args", "source_path")

FormatConfigInfo = provider(fields = {
    "binary": "FilesToRunProvider, or None when formatting is not configured.",
    "config": "Formatter config File, or None.",
    "data": "depset of File: config imports and other formatter inputs.",
    "args": "list of string: read-only formatter arguments.",
})

def _format_config_impl(ctx):
    return [FormatConfigInfo(
        binary = ctx.attr.binary[DefaultInfo].files_to_run if ctx.attr.binary else None,
        config = ctx.file.config,
        data = depset(ctx.files.data),
        args = ctx.attr.args,
    )]

format_config = rule(
    implementation = _format_config_impl,
    attrs = {
        "binary": attr.label(executable = True, cfg = "exec"),
        "config": attr.label(allow_single_file = True),
        "data": attr.label_list(allow_files = True),
        "args": attr.string_list(default = ["--check"]),
    },
)

def _format_config_repo_impl(rctx):
    rctx.file("BUILD.bazel", "\n".join([
        'load("{}", "format_config")'.format(str(Label("//ts/private/actions:format.bzl"))),
        "format_config(",
        '    name = "format",',
        "    binary = {},".format(repr(rctx.attr.binary) if rctx.attr.binary else "None"),
        "    config = {},".format(repr(rctx.attr.config) if rctx.attr.config else "None"),
        "    data = {},".format(repr(rctx.attr.data)),
        "    args = {},".format(repr(rctx.attr.args)),
        '    visibility = ["//visibility:public"],',
        ")",
        "",
    ]))

format_config_repo = repository_rule(
    implementation = _format_config_repo_impl,
    attrs = {
        "binary": attr.string(),
        "config": attr.string(),
        "data": attr.string_list(),
        "args": attr.string_list(),
    },
)

def format_action(ctx, formatter, files):
    """Returns a validation stamp for checked-in inputs, or None when disabled."""
    sources = [file for file in files if file.is_source]
    if not formatter.binary or not sources:
        return None
    stamp = ctx.actions.declare_file(ctx.label.name + ".tsformat")
    configs = [formatter.config] if formatter.config else []
    args = program_args(ctx, stamp.dirname + "/" + ctx.label.name + ".format", sources, [], depset(), [], [], [])
    args.add_all(depset(sources + configs, transitive = [formatter.data]), map_each = source_path, format_each = "-copy=%s", expand_directories = False)
    args.add("-verify-copies")
    args.add("-absolute-copy-args")
    args.add(stamp, format = "-stamp=%s")
    args.add("--")
    args.add(formatter.binary.executable)
    args.add_all(formatter.args)
    if formatter.config:
        args.add("--config", formatter.config)
    args.add_all(sources)
    ctx.actions.run(
        inputs = depset(sources + configs, transitive = [formatter.data]),
        tools = [formatter.binary],
        outputs = [stamp],
        executable = get_tools_toolchain(ctx).tsaction,
        arguments = ["tsgo", args],
        env = {"PATH": "/bin:/usr/bin"},
        mnemonic = "TsFormat",
        progress_message = "TsFormat %{label}",
    )
    return stamp

FORMAT_ATTR = attr.label(default = Label("//ts:format"), providers = [FormatConfigInfo])

def _ts_format_impl(ctx):
    formatter = ctx.attr._format[FormatConfigInfo]
    if not formatter.binary:
        fail("{} needs ts.format(binary = ...) in MODULE.bazel; did you mean to configure @npm//:oxfmt_bin?".format(ctx.label))
    stamp = format_action(ctx, formatter, ctx.files.srcs)
    return [
        DefaultInfo(files = depset(ctx.files.srcs)),
        OutputGroupInfo(_validation = depset([stamp] if stamp else [])),
    ]

ts_format = rule(
    implementation = _ts_format_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "_format": FORMAT_ATTR,
    },
    toolchains = [TOOLS_TOOLCHAIN_TYPE],
    doc = "Validates formatting for an explicit file set, including non-TypeScript files, using the root ts.format() configuration.",
)
