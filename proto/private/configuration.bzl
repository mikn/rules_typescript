"""Shared native protobuf configuration for generation and graph discovery."""

_SOURCE_INFO = str(Label("@protobuf//bazel/flags:experimental_proto_descriptor_sets_include_source_info"))

def _source_info_impl(_settings, _attr):
    return {_SOURCE_INFO: True}

proto_source_info = transition(
    implementation = _source_info_impl,
    inputs = [],
    outputs = [_SOURCE_INFO],
)
