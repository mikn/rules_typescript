"""The pinned protobuf runtime's public import mapping for Gazelle."""

def _impl(ctx):
    ctx.actions.run(
        executable = ctx.attr._generator[DefaultInfo].files_to_run,
        arguments = [ctx.outputs.out.path],
        outputs = [ctx.outputs.out],
        mnemonic = "ProtoRuntimeImports",
    )
    return [DefaultInfo(files = depset([ctx.outputs.out]))]

runtime_imports = rule(
    implementation = _impl,
    attrs = {
        "_generator": attr.label(default = ":runtime_imports_generator", executable = True, cfg = "exec"),
    },
    outputs = {"out": "%{name}.json"},
)
