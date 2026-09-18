"""Pinned archives used only by the explicit prebuilt_tools configuration."""

TOOLS_VERSION = "1"

TOOLS_INTEGRITY = {
    "darwin_amd64": "sha256-y24L43nLTdr5n4hSocBGmPxaxkq2yLWbmhQzQ0GJHy8=",
    "darwin_arm64": "sha256-Ee4ywBSKF+NRT0FxOT/a2kU7iOnSlRiaTuZV81FO0eU=",
    "linux_amd64": "sha256-DDDwyUvvYpxH+fPdYANsZhLCzi6OFYZBK9HXLfXPlEY=",
    "linux_arm64": "sha256-edQ5K0MWVzkD5kR2QhGVb7UgwSEFtU0kSCFIpmlLaY4=",
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
