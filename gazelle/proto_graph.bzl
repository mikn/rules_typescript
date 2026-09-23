"""Declared native protobuf graph input for TypeScript Gazelle generation."""

load("@protobuf//bazel/common:proto_common.bzl", "proto_common")
load("@protobuf//bazel/common:proto_info.bzl", "ProtoInfo")
load("//proto/private:configuration.bzl", "proto_source_info")

_ProtoGraphInfo = provider(fields = ["nodes"])

def _graph_aspect_impl(target, ctx):
    dependencies = []
    for attribute in ["deps", "exports", "actual"]:
        value = getattr(ctx.rule.attr, attribute, [])
        dependencies.extend(value if type(value) == "list" else [value])
    dependencies = [dep for dep in dependencies if _ProtoGraphInfo in dep]
    sources = [] if ctx.rule.kind == "alias" else target[ProtoInfo].direct_sources
    node = {
        "label": str(target.label),
        "sources": sorted([proto_common.get_import_path(source) for source in sources]),
        "deps": sorted({str(dep.label): True for dep in dependencies}.keys()),
    }
    return [_ProtoGraphInfo(nodes = depset([json.encode(node)], transitive = [dep[_ProtoGraphInfo].nodes for dep in dependencies]))]

_graph_aspect = aspect(
    implementation = _graph_aspect_impl,
    attr_aspects = ["deps", "exports", "actual"],
    required_providers = [ProtoInfo],
)

def _graph_impl(ctx):
    nodes = depset(transitive = [dep[_ProtoGraphInfo].nodes for dep in ctx.attr.roots]).to_list()
    output = ctx.actions.declare_file(ctx.label.name + ".json")
    ctx.actions.write(output, json.encode({
        "nodes": [json.decode(node) for node in sorted(nodes)],
        "roots": {spelling: str(dep.label) for dep, spelling in ctx.attr.roots.items()},
    }))
    return [DefaultInfo(files = depset([output]))]

_graph = rule(
    implementation = _graph_impl,
    attrs = {
        "roots": attr.label_keyed_string_dict(aspects = [_graph_aspect], providers = [ProtoInfo], cfg = proto_source_info),
        "_allowlist_function_transition": attr.label(default = "@bazel_tools//tools/allowlists/function_transition_allowlist"),
    },
)

def proto_graph(name, deps, **kwargs):
    """Exports configured native graph facts without selecting generated schemas."""
    _graph(name = name, roots = {dep: str(dep) for dep in deps}, **kwargs)
