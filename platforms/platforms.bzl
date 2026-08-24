"""The platform vocabulary of rules_typescript.

One table, loaded by everything that needs to talk about a platform: the
toolchain declarations (which constraints a toolchain's binary satisfies), the
repository rules that download per-platform binaries (npm's naming for the same
platform), and this package's `platform()` / `config_setting()` targets.
"""

PLATFORMS = {
    "linux_amd64": struct(
        os = "@platforms//os:linux",
        cpu = "@platforms//cpu:x86_64",
        npm_os = "linux",
        npm_cpu = "x64",
    ),
    "linux_arm64": struct(
        os = "@platforms//os:linux",
        cpu = "@platforms//cpu:aarch64",
        npm_os = "linux",
        npm_cpu = "arm64",
    ),
    "darwin_amd64": struct(
        os = "@platforms//os:macos",
        cpu = "@platforms//cpu:x86_64",
        npm_os = "darwin",
        npm_cpu = "x64",
    ),
    "darwin_arm64": struct(
        os = "@platforms//os:macos",
        cpu = "@platforms//cpu:aarch64",
        npm_os = "darwin",
        npm_cpu = "arm64",
    ),
    "windows_amd64": struct(
        os = "@platforms//os:windows",
        cpu = "@platforms//cpu:x86_64",
        npm_os = "win32",
        npm_cpu = "x64",
    ),
}

def npm_arch(platform):
    """The `<os>-<cpu>` string npm uses to name a platform-specific package.

    Args:
        platform: A key of PLATFORMS.

    Returns:
        e.g. "linux-x64" for "linux_amd64".
    """
    entry = PLATFORMS[platform]
    return "{}-{}".format(entry.npm_os, entry.npm_cpu)

def constraints(platform):
    """The os and cpu constraint_value labels of a platform, as a list.

    Args:
        platform: A key of PLATFORMS.

    Returns:
        list of label strings, suitable for exec_compatible_with /
        target_compatible_with / constraint_values.
    """
    entry = PLATFORMS[platform]
    return [entry.os, entry.cpu]

# buildifier: disable=unnamed-macro
def declare_platforms():
    """Declares a platform() and a matching config_setting() per PLATFORMS key.

    `//platforms:<key>` names the platform for --platforms / --host_platform;
    `//platforms:is_<key>` is what a select() over platforms keys on, since
    select() cannot key on a constraint_value directly.
    """
    for platform in PLATFORMS:
        native.platform(
            name = platform,
            constraint_values = constraints(platform),
        )
        native.config_setting(
            name = "is_{}".format(platform),
            constraint_values = constraints(platform),
        )
