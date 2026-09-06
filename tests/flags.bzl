"""A ts_compile target built under other values of the ruleset's build flags.

The emit knobs -- `//ts:declarations`, `//ts:source_map`, `//ts:declaration_map`
-- are build-wide, so a fixture that has to be built the other way is reached
through a transition rather than an attribute. The wrapped target's outputs and
output groups are this target's, so a go_test, a filegroup over `_validation` or
a build_test names the wrapper where it named the target.
"""

_FLAGS = {
    "declarations": str(Label("//ts:declarations")),
    "source_map": str(Label("//ts:source_map")),
    "declaration_map": str(Label("//ts:declaration_map")),
}

_BOOL = {"true": True, "false": False}

def _under_flags_impl(settings, attr):
    return {
        _FLAGS["declarations"]: attr.declarations or settings[_FLAGS["declarations"]],
        _FLAGS["source_map"]: _BOOL.get(attr.source_map, settings[_FLAGS["source_map"]]),
        _FLAGS["declaration_map"]: _BOOL.get(attr.declaration_map, settings[_FLAGS["declaration_map"]]),
    }

_under_flags = transition(
    implementation = _under_flags_impl,
    inputs = _FLAGS.values(),
    outputs = _FLAGS.values(),
)

def _ts_under_flags_impl(ctx):
    target = ctx.attr.target
    if type(target) == "list":
        target = target[0]
    providers = [DefaultInfo(files = target[DefaultInfo].files)]
    if OutputGroupInfo in target:
        providers.append(target[OutputGroupInfo])
    return providers

ts_under_flags = rule(
    implementation = _ts_under_flags_impl,
    attrs = {
        "target": attr.label(
            cfg = _under_flags,
            mandatory = True,
            doc = "The target to build under the flag values below.",
        ),
        "declarations": attr.string(
            values = ["", "tsgo", "oxc"],
            doc = "The value of //ts:declarations, or \"\" to keep the build's.",
        ),
        "source_map": attr.string(
            values = ["", "true", "false"],
            doc = "The value of //ts:source_map, or \"\" to keep the build's.",
        ),
        "declaration_map": attr.string(
            values = ["", "true", "false"],
            doc = "The value of //ts:declaration_map, or \"\" to keep the build's.",
        ),
        "_allowlist_function_transition": attr.label(
            default = "@bazel_tools//tools/allowlists/function_transition_allowlist",
        ),
    },
    doc = "Builds `target` under the given values of the ruleset's emit flags and forwards its files and output groups.",
)
