"""TypeScript source libraries generated from proto_library sources."""

load("@protobuf//bazel/common:proto_common.bzl", "proto_common")
load("@protobuf//bazel/common:proto_info.bzl", "ProtoInfo")
load("//ts/private/rules:ts_compile.bzl", "ts_compile")

_SOURCE_INFO = str(Label("@protobuf//bazel/flags:experimental_proto_descriptor_sets_include_source_info"))

def _source_info_impl(_settings, _attr):
    return {_SOURCE_INFO: True}

_source_info = transition(
    implementation = _source_info_impl,
    inputs = [],
    outputs = [_SOURCE_INFO],
)

def _generate_impl(ctx):
    proto = ctx.attr.proto[0][ProtoInfo]
    if not proto.direct_sources:
        fail("ts_proto_library: proto must have direct sources")
    paths = [proto_common.get_import_path(src).removesuffix(".proto") + "_pb.ts" for src in proto.direct_sources]
    outputs = [ctx.actions.declare_file(ctx.attr.out_dir + "/" + path) for path in paths]
    toolchain = proto_common.ProtoLangToolchainInfo(
        out_replacement_format_flag = "--es_out=%s",
        output_files = "legacy",
        plugin_format_flag = "--plugin=protoc-gen-es=%s",
        plugin = ctx.attr._plugin[DefaultInfo].files_to_run,
        proto_compiler = ctx.attr._protoc[DefaultInfo].files_to_run,
        protoc_opts = ["--es_opt=" + ",".join(ctx.attr.options)],
        mnemonic = "TsProtoGenerate",
        progress_message = "Generating TypeScript protobuf sources %{label}",
    )
    proto_common.compile(
        actions = ctx.actions,
        proto_info = proto,
        proto_lang_toolchain_info = toolchain,
        generated_files = outputs,
        plugin_output = outputs[0].path.removesuffix(paths[0]),
    )
    return [DefaultInfo(files = depset(outputs))]

_generate = rule(
    implementation = _generate_impl,
    attrs = {
        "proto": attr.label(mandatory = True, providers = [ProtoInfo], cfg = _source_info),
        "_allowlist_function_transition": attr.label(default = "@bazel_tools//tools/allowlists/function_transition_allowlist"),
        "out_dir": attr.string(mandatory = True),
        "options": attr.string_list(),
        "_protoc": attr.label(default = "@protobuf//:protoc", executable = True, cfg = "exec"),
        "_plugin": attr.label(default = "//proto/private:protoc_gen_es", executable = True, cfg = "exec"),
    },
)

def ts_proto_library(name, proto, out_dir, tsconfig, deps = [], node_modules = None, options = ["target=ts"], **kwargs):
    """Generates direct proto files through the existing source-mode compiler owner."""
    if "target=ts" not in options or any([option.startswith("target=") and option != "target=ts" for option in options]):
        fail("ts_proto_library: options must select target=ts for declared TypeScript outputs")
    generated = name + "_generate"
    _generate(
        name = generated,
        proto = proto,
        out_dir = out_dir,
        options = options,
        visibility = ["//visibility:private"],
        testonly = kwargs.get("testonly", False),
    )
    ts_compile(
        name = name,
        srcs = [":" + generated],
        deps = deps,
        tsconfig = tsconfig,
        node_modules = node_modules,
        emit = False,
        **kwargs
    )
