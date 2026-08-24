"""Analysis tests pinning the split between what the compiler READS and what an import may RESOLVE to.

A dep's own deps stay action inputs: the compiler has to read them to check the
declarations of the dep that WAS declared. They must not reach the check that
decides whether an import is declared, or a transitively-arriving .d.ts would
keep satisfying an import -- the drift this whole check exists to catch.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("//ts/private:providers.bzl", "TsDeclarationInfo")

_MNEMONIC = "TsStrictDeps"

def _resolution_surface_impl(ctx):
    env = analysistest.begin(ctx)

    transitive_only = [
        f.path
        for f in ctx.attr.transitive_dep[TsDeclarationInfo].declaration_files.to_list()
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
