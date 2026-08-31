"""Reaches the classification a compile cannot show.

A module-scoped .d.ts wrongly called global would put a module in the consumer's
program and change nothing observable there, so the entry file itself is the
only place that half of the answer is visible.
"""

load("//ts/private:providers.bzl", "TsDeclarationInfo")

def _global_entry_files_impl(ctx):
    return [DefaultInfo(files = ctx.attr.target[TsDeclarationInfo].global_entry_files)]

global_entry_files = rule(
    implementation = _global_entry_files_impl,
    attrs = {
        "target": attr.label(mandatory = True, providers = [TsDeclarationInfo]),
    },
    doc = "The global-declaration entry `target` provides, as a file a test can read.",
)
