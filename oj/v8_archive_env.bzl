def _v8_archive_env_impl(ctx):
    output = ctx.actions.declare_file(ctx.label.name + ".env")
    ctx.actions.write(output, "RUSTY_V8_ARCHIVE=${pwd}/" + ctx.file.archive.path + "\n")
    return [DefaultInfo(files = depset([output]))]

v8_archive_env = rule(
    implementation = _v8_archive_env_impl,
    attrs = {"archive": attr.label(mandatory = True, allow_single_file = True)},
)
