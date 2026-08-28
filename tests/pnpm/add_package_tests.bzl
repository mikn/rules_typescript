"""Analysis-time coverage for ts_add_package: which hub a target edits.

The wrapper is handed the hub as an environment variable and the hub's lockfile
as a runfile; both are read straight off the target here, so the derivation from
the `pnpm_lock` label is pinned without running pnpm.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _hub_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    asserts.equals(
        env,
        ctx.attr.hub_dir,
        target[RunEnvironmentInfo].environment.get("PNPM_HUB_DIR"),
        "PNPM_HUB_DIR",
    )

    runfiles = [f.short_path for f in target[DefaultInfo].default_runfiles.files.to_list()]
    asserts.true(
        env,
        ctx.attr.lockfile in runfiles,
        "the hub's lockfile is an input of the target: " + str(runfiles),
    )
    return analysistest.end(env)

hub_test = analysistest.make(
    _hub_impl,
    attrs = {
        "hub_dir": attr.string(mandatory = True),
        "lockfile": attr.string(mandatory = True),
    },
)
