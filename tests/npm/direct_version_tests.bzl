"""Analysis-time coverage for which version of a name a target's `paths` names.

A tsconfig has one `paths` key per package name, and a target reaching two
versions of one name -- its own, and an older copy inside a dependent's closure
-- can have only one of them there. The one its sources mean is the one it
declared: pnpm installed that version for this importer and the bundler resolves
it, so a `paths` entry naming the other type-checks the code against a package
that is not the one it runs on.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _tsconfig_paths(env):
    for action in analysistest.target_actions(env):
        outputs = action.outputs.to_list()
        if len(outputs) == 1 and outputs[0].basename.endswith(".tsconfig.json"):
            return json.decode(action.content)["compilerOptions"]["paths"]
    return None

def _direct_version_wins_impl(ctx):
    env = analysistest.begin(ctx)
    paths = _tsconfig_paths(env)
    asserts.true(env, paths != None, "ts_compile generated no tsconfig")
    if paths == None:
        return analysistest.end(env)

    for key in (ctx.attr.package, ctx.attr.package + "/*"):
        entry = paths.get(key, [])
        asserts.equals(env, 1, len(entry), "one value for '{}': {}".format(key, entry))
        if len(entry) != 1:
            continue
        asserts.true(
            env,
            ctx.attr.want_dir in entry[0],
            "'{}' resolves to {}, want the directory of {}".format(key, entry[0], ctx.attr.want_dir),
        )
    return analysistest.end(env)

direct_version_wins_test = analysistest.make(
    _direct_version_wins_impl,
    attrs = {
        "package": attr.string(mandatory = True),
        "want_dir": attr.string(mandatory = True),
    },
)
