"""Analysis test: which platform each toolchain's binary comes from."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load(":probe.bzl", "ToolchainProbeInfo")

def _foreign_target_platform_test_impl(ctx):
    env = analysistest.begin(ctx)
    resolved = analysistest.target_under_test(env)[ToolchainProbeInfo]

    asserts.false(
        env,
        "windows" in resolved.tsgo,
        "tsgo runs on the exec platform, so it must not be the target platform's binary: " + resolved.tsgo,
    )
    asserts.false(
        env,
        "windows" in resolved.oxc,
        "oxc runs on the exec platform, so it must not be the target platform's binary: " + resolved.oxc,
    )
    asserts.true(
        env,
        "nodejs_windows_amd64" in resolved.js_runtime,
        "the staged JS runtime must come from the target platform: " + resolved.js_runtime,
    )
    asserts.false(
        env,
        "windows" in resolved.js_tool,
        "the JS runtime used as a build tool must come from the exec platform: " + resolved.js_tool,
    )

    return analysistest.end(env)

foreign_target_platform_test = analysistest.make(
    _foreign_target_platform_test_impl,
    config_settings = {
        "//command_line_option:platforms": [Label("//platforms:windows_amd64")],
    },
)
