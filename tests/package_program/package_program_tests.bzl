"""Which files of a dep a ts_test's check reads: the sources under the test's
tsconfig, the declarations under another."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

_PKG = "tests/package_program/"

def _rel(f):
    return f.path[f.path.find(_PKG) + len(_PKG):]

def _rels(files):
    return sorted([_rel(f) for f in files])

def _action(env, mnemonic):
    for action in analysistest.target_actions(env):
        if action.mnemonic == mnemonic:
            return action
    return None

def _manifest_lines(env, check):
    name = analysistest.target_under_test(env).label.name
    manifest = [
        f
        for f in check.inputs.to_list()
        if f.basename == name + ".ownership"
    ]
    asserts.equals(env, 1, len(manifest), "the ownership manifest is an input")
    if len(manifest) != 1:
        return []
    writers = [
        a
        for a in analysistest.target_actions(env)
        if manifest[0] in a.outputs.to_list()
    ]
    asserts.equals(env, 1, len(writers), "one action writes the manifest")
    if len(writers) != 1:
        return []
    return (writers[0].content or "").split("\n")

def _dep(ctx, env):
    info = ctx.attr.dep[TsInfo]
    asserts.equals(
        env,
        ["greet.mjs", "lib.ts"],
        _rels(info.sources.to_list()),
        "the dep's sources: its TypeScript and JavaScript srcs",
    )
    asserts.equals(
        env,
        ["greet.d.mts", "lib.d.ts"],
        _rels(info.declarations.to_list()),
        "the dep's declarations",
    )
    asserts.equals(
        env,
        [],
        info.deps_declarations.to_list(),
        "a leaf's program read no declarations",
    )
    return info

def _checks_the_sources_impl(ctx):
    env = analysistest.begin(ctx)
    dep = _dep(ctx, env)
    asserts.equals(
        env,
        ctx.file.tsconfig,
        dep.tsconfig,
        "the dep's tsconfig is the test's",
    )
    sources = dep.sources.to_list()
    declarations = dep.declarations.to_list()

    for mnemonic in ["TsConfig", "TsgoCheck"]:
        action = _action(env, mnemonic)
        asserts.true(env, action != None, "the test runs " + mnemonic)
        if action == None:
            continue
        inputs = action.inputs.to_list()
        for f in sources:
            asserts.true(
                env,
                f in inputs,
                "{} reads the dep's {}".format(mnemonic, _rel(f)),
            )
    for action in analysistest.target_actions(env):
        inputs = action.inputs.to_list()
        for f in declarations:
            asserts.false(
                env,
                f in inputs,
                "{} reads the dep's {}".format(action.mnemonic, _rel(f)),
            )
    target = analysistest.target_under_test(env)
    runfiles = target[DefaultInfo].default_runfiles.files.to_list()
    for f in declarations:
        asserts.false(env, f in runfiles, _rel(f) + " is in the runfiles")

    check = _action(env, "TsgoCheck")
    if check != None:
        lines = _manifest_lines(env, check)
        label = str(ctx.attr.dep.label)
        label = label[2:] if label.startswith("@@//") else label
        for f in sources:
            asserts.true(
                env,
                "file\t{}\t{}".format(label, f.path) in lines,
                "the manifest gives {} to {}:\n{}".format(
                    _rel(f),
                    label,
                    "\n".join(lines),
                ),
            )
            asserts.false(
                env,
                "own\t" + f.path in lines,
                "the test owns the dep's " + _rel(f),
            )
    return analysistest.end(env)

def _reads_the_declarations_impl(ctx):
    env = analysistest.begin(ctx)
    dep = _dep(ctx, env)
    asserts.false(
        env,
        ctx.file.tsconfig == dep.tsconfig,
        "the dep's tsconfig is another file",
    )
    check = _action(env, "TsgoCheck")
    asserts.true(env, check != None, "the test runs TsgoCheck")
    if check == None:
        return analysistest.end(env)
    inputs = check.inputs.to_list()
    for f in dep.declarations.to_list():
        asserts.true(env, f in inputs, "TsgoCheck reads the dep's " + _rel(f))
    for f in dep.sources.to_list():
        asserts.false(env, f in inputs, "TsgoCheck reads the dep's " + _rel(f))
    return analysistest.end(env)

_ATTRS = {
    "dep": attr.label(
        mandatory = True,
        doc = "The ts_compile the test under test depends on.",
    ),
    "tsconfig": attr.label(
        mandatory = True,
        allow_single_file = [".json"],
        doc = "The test's tsconfig.",
    ),
}

checks_the_sources_test = analysistest.make(
    _checks_the_sources_impl,
    attrs = _ATTRS,
)

reads_the_declarations_test = analysistest.make(
    _reads_the_declarations_impl,
    attrs = _ATTRS,
)
