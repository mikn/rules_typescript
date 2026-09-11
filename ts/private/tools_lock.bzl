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
    "darwin_amd64": "sha256-A0OJr2fRmE80TARjbWC55cCG6KFp71NUfC7fOsJi/xc=",
    "darwin_arm64": "sha256-AxPV0D8RpOVzcJs28/83JxKtRYnNiJnicEI0TVXkPDs=",
    "linux_amd64": "sha256-FOdOemgnoQxwLtzp11o/LcwN1PO48F/7EGoiAOc1rfo=",
    "linux_arm64": "sha256-RQElQwxWCQgf9mKXeFNYzwaIQ1qtNhgznE9/ol/go4I=",
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
