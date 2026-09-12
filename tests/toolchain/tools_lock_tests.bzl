"""Unit tests for the tools release table the ts extension reads.

The table is text in the tree; //tests/integration/tools_release pins the
fetch it drives and what the downloaded binaries do.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//ts/private:toolchain.bzl", "TSGO_PLATFORMS")
load(
    "//ts/private:tools_lock.bzl",
    "TOOLS_INTEGRITY",
    "TOOLS_VERSION",
    "tools_asset_prefix",
    "tools_asset_url",
)

_SRI_LENGTH = len("sha256-") + 44

_RELEASES = "https://github.com/mikn/rules_typescript/releases/download/"

def _table_test(ctx):
    env = unittest.begin(ctx)

    asserts.true(
        env,
        TOOLS_VERSION.isdigit(),
        "TOOLS_VERSION is the N of tools-v<N>: " + TOOLS_VERSION,
    )
    asserts.equals(env, sorted(TSGO_PLATFORMS), sorted(TOOLS_INTEGRITY.keys()))
    for platform, integrity in TOOLS_INTEGRITY.items():
        asserts.true(
            env,
            integrity.startswith("sha256-") and len(integrity) == _SRI_LENGTH,
            "{}: an SRI as check_tools_lock.sh prints it, not {}".format(
                platform,
                repr(integrity),
            ),
        )

    return unittest.end(env)

table_test = unittest.make(_table_test)

def _asset_test(ctx):
    env = unittest.begin(ctx)

    asserts.equals(
        env,
        "rules_typescript-tools-1-linux_amd64",
        tools_asset_prefix("1", "linux_amd64"),
    )
    asserts.equals(
        env,
        _RELEASES + "tools-v1/rules_typescript-tools-1-linux_amd64.tar.gz",
        tools_asset_url("1", "linux_amd64"),
    )
    asserts.equals(
        env,
        _RELEASES + "tools-v12/rules_typescript-tools-12-darwin_arm64.tar.gz",
        tools_asset_url("12", "darwin_arm64"),
    )

    return unittest.end(env)

asset_test = unittest.make(_asset_test)

def tools_lock_test_suite(name):
    unittest.suite(
        name,
        table_test,
        asset_test,
    )
