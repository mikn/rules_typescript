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
    "darwin_amd64": "sha256-VaOqjVqd6N94iGoeYdJEOZhY4ZSGZR2qlbYCUR2Khww=",
    "darwin_arm64": "sha256-CZS8wPZZWrG1wXguUMc0bvLxGeWiDkI1ASWYB93/snQ=",
    "linux_amd64": "sha256-Y2dOixFxp4wciv31oZnhwgHxeoqT7wPKqVSD2AGI5j4=",
    "linux_arm64": "sha256-rlz6CbsvABC4UaOdUPJ1No+8EwKhE4vfKQrDXdqHsGo=",
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
