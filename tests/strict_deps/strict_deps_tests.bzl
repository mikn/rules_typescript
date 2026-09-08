"""Analysis tests pinning the split between what the compiler READS and what an import may RESOLVE to.

A dep's own deps stay action inputs: the compiler has to read them to check the
declarations of the dep that WAS declared. They must not reach the check that
decides whether an import is declared, or a transitively-arriving .d.ts would
keep satisfying an import -- the drift this whole check exists to catch.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts:defs.bzl", "TsInfo")

_MNEMONIC = "TsStrictDeps"

def _resolution_surface_impl(ctx):
    env = analysistest.begin(ctx)

    transitive_only = [
        f.path
        for f in ctx.attr.transitive_dep[TsInfo].declarations.to_list()
    ]
    asserts.true(
        env,
        len(transitive_only) > 0,
        "the transitive dep produces no declarations -- the test would pass vacuously",
    )

    checks = []
    readers = []
    for action in analysistest.target_actions(env):
        inputs = [f.path for f in action.inputs.to_list()]
        if action.mnemonic == _MNEMONIC:
            checks.append(action)
            for path in transitive_only:
                asserts.false(
                    env,
                    path in inputs,
                    "{} reads {}, so a transitively-provided declaration could satisfy an import".format(
                        _MNEMONIC,
                        path,
                    ),
                )
        elif [p for p in transitive_only if p in inputs]:
            readers.append(action.mnemonic)

    asserts.equals(
        env,
        1,
        len(checks),
        "expected exactly one {} action on the target under test".format(_MNEMONIC),
    )
    asserts.true(
        env,
        len(readers) > 0,
        "no action reads the transitive declarations -- narrowing the check also narrowed the inputs",
    )

    stamps = [f.path for f in checks[0].outputs.to_list()] if checks else []
    gated = []
    for action in analysistest.target_actions(env):
        if action.mnemonic == _MNEMONIC:
            continue
        inputs = [f.path for f in action.inputs.to_list()]
        if [p for p in stamps if p in inputs]:
            gated.append(action.mnemonic)
    asserts.true(
        env,
        len(gated) > 0,
        "nothing takes the check's stamp as an input, so a finding would not stop the compile",
    )

    return analysistest.end(env)

resolution_surface_test = analysistest.make(
    _resolution_surface_impl,
    attrs = {
        "transitive_dep": attr.label(
            mandatory = True,
            doc = "A target reachable from the target under test only through another dep.",
        ),
    },
)

def _tsgo_check_impl(ctx):
    env = analysistest.begin(ctx)
    name = analysistest.target_under_test(env).label.name
    tsgo = [
        a
        for a in analysistest.target_actions(env)
        if a.mnemonic in ("TsgoDeclare", "TsgoCheck")
    ]
    asserts.equals(env, 1, len(tsgo), "one tsgo action")
    if len(tsgo) != 1:
        return analysistest.end(env)
    argv = tsgo[0].argv
    asserts.true(env, "--explainFiles" in argv, "tsgo lists the edges")
    asserts.true(
        env,
        "--pretty" in argv and argv[argv.index("--pretty") + 1] == "false",
        "the listing is plain text",
    )
    manifest = [
        f
        for f in tsgo[0].inputs.to_list()
        if f.basename == name + ".ownership"
    ]
    asserts.equals(env, 1, len(manifest), "the ownership manifest is an input")
    if manifest:
        asserts.true(
            env,
            "-check=" + manifest[0].path in argv,
            "tsaction reads the manifest: " + str(argv),
        )
        writers = [
            a
            for a in analysistest.target_actions(env)
            if manifest[0] in a.outputs.to_list()
        ]
        asserts.equals(env, 1, len(writers), "one action writes the manifest")
        if writers and ctx.attr.lines:
            content = writers[0].content
            for line in ctx.attr.lines:
                asserts.true(
                    env,
                    content != None and line in content.split("\n"),
                    "the manifest carries %r:\n%s" % (line, content),
                )
    return analysistest.end(env)

tsgo_check_test = analysistest.make(
    _tsgo_check_impl,
    attrs = {
        "lines": attr.string_list(
            doc = "Lines the ownership manifest holds, tab-separated fields.",
        ),
    },
)
