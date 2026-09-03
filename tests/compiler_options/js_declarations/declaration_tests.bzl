"""Analysis-time proof of what a .d.mts in srcs is: a declaration, and the one."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsDeclarationInfo")

_PKG = "tests/compiler_options/js_declarations/"

_CHECKED_IN = ["esm.d.mts", "legacy.d.cts"]

def _declarations_of(env):
    return analysistest.target_under_test(env)[TsDeclarationInfo].declaration_files.to_list()

def _passes_declarations_through_impl(ctx):
    env = analysistest.begin(ctx)
    declared = [f.short_path for f in _declarations_of(env)]
    for name in _CHECKED_IN:
        asserts.true(
            env,
            _PKG + name in declared,
            "{} is passed through as this target's own declaration: {}".format(name, declared),
        )
    return analysistest.end(env)

passes_declarations_through_test = analysistest.make(_passes_declarations_through_impl)

# TypeScript keeps the higher-priority extension of a .mjs / .d.mts pair listed
# together, so the .mjs leaves the program and tsgo writes nothing for it: an
# output declared at that path is never created, and a second declaration for
# one module would shadow the checked-in file for module_name consumers anyway.
def _one_declaration_per_module_impl(ctx):
    env = analysistest.begin(ctx)
    for name in _CHECKED_IN:
        found = [f for f in _declarations_of(env) if f.short_path == _PKG + name]
        asserts.equals(env, 1, len(found), "{}: one declaration for the module".format(name))
        asserts.true(
            env,
            len(found) == 1 and found[0].is_source,
            "{} is the checked-in file, not tsgo's emit for the JavaScript".format(name),
        )
    generated = [
        f.short_path
        for f in analysistest.target_under_test(env)[DefaultInfo].files.to_list()
        if not f.is_source and f.basename in _CHECKED_IN
    ]
    asserts.equals(env, [], generated, "no generated twin of a checked-in declaration in the default outputs")
    return analysistest.end(env)

one_declaration_per_module_test = analysistest.make(_one_declaration_per_module_impl)
