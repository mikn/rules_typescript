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
    "darwin_amd64": "sha256-ajhdnYbo+e4yS7i1WLl2KKW2l6HhAE5709PAFBao7Eo=",
    "darwin_arm64": "sha256-jEVXPBHAdjPYkZcgl3+TnKIs/e8xxy/cFVOikpdLnoQ=",
    "linux_amd64": "sha256-2z4+ZhthTYOfTnZIbgZ7UeHt+8EbdrJv41acHy17y64=",
    "linux_arm64": "sha256-tngWOWkD80MEYANz0lOIbeJQ/QmDKTw3yuAOVEoXws8=",
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
