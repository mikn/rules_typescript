"""The tools release a build downloads its Go tools from.

`TOOLS_VERSION` names the `tools-v<N>` release whose four assets, one per
platform of `TSGO_PLATFORMS`, hold tsaction, ts_launcher, lcov_merger and
copy_to_workspace; `TOOLS_INTEGRITY` is the SRI of each asset, as
`tools/ci/check_tools_lock.sh` prints it from a build of the tagged sources.
Text in the tree, read with no fetch; //tests/toolchain:tools_lock_tests pins
the shape.
"""

TOOLS_VERSION = "1"

TOOLS_INTEGRITY = {
    "darwin_amd64": "sha256-lkhJ3Om1cC6mhQekscz8DH+gSMTPzqttV1AoD2f4U0k=",
    "darwin_arm64": "sha256-cUfe3rgBWIBjNTk/4BeSZhibcckjKwAer5qlYzPPzOw=",
    "linux_amd64": "sha256-UUjfmto3MzgatMiC1/irOdTVAwUBUyIoXucM7zzScCQ=",
    "linux_arm64": "sha256-kCaaSl7xkaChQJ+pj4HFYoJD9OPTocC00tj5EZ5BHnM=",
}

_ASSET = "rules_typescript-tools-{version}-{platform}"

def tools_asset_prefix(version, platform):
    """The one directory a tools asset holds, and the asset's basename."""
    return _ASSET.format(version = version, platform = platform)

def tools_asset_url(version, platform):
    """The release asset of one platform."""
    return (
        "https://github.com/mikn/rules_typescript/releases/download/" +
        "tools-v{version}/{asset}.tar.gz"
    ).format(version = version, asset = tools_asset_prefix(version, platform))
