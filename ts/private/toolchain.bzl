"""Toolchain definitions for rules_typescript.

Defines the toolchain providers and rules for the oxc-bazel and tsgo
compilers, and the repository rule that fetches a tsgo binary.
"""

load("//platforms:platforms.bzl", "constraints", "npm_arch")

# ─── Toolchain type labels ────────────────────────────────────────────────────

# Label(), not a string: these resolve in this file's repository mapping, so
# they keep working when a consumer gives rules_typescript another repo name.
OXC_TOOLCHAIN_TYPE = Label("//ts/toolchain:oxc_toolchain_type")
TSGO_TOOLCHAIN_TYPE = Label("//ts/toolchain:tsgo_toolchain_type")

# ─── Providers ────────────────────────────────────────────────────────────────

OxcToolchainInfo = provider(
    doc = "Information about the oxc-bazel toolchain.",
    fields = {
        "oxc_binary": "File: The oxc-bazel CLI binary.",
    },
)

TsgoToolchainInfo = provider(
    doc = "Information about the tsgo toolchain.",
    fields = {
        "tsgo_binary": "File: The tsgo CLI binary.",
    },
)

# ─── Toolchain implementations ────────────────────────────────────────────────

def _oxc_toolchain_impl(ctx):
    binary = ctx.file.oxc_binary
    toolchain_info = platform_common.ToolchainInfo(
        oxc_info = OxcToolchainInfo(
            oxc_binary = binary,
        ),
        # Standard fields consumed by @toolchain_utils//toolchain:resolved.bzl.
        executable = binary,
        variable = "OXC",
        default = DefaultInfo(
            files = depset([binary]),
            runfiles = ctx.runfiles([binary]),
        ),
    )
    return [toolchain_info]

oxc_toolchain = rule(
    implementation = _oxc_toolchain_impl,
    attrs = {
        "oxc_binary": attr.label(
            doc = "The oxc-bazel CLI binary.",
            mandatory = True,
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)

def _tsgo_toolchain_impl(ctx):
    binary = ctx.file.tsgo_binary
    toolchain_info = platform_common.ToolchainInfo(
        tsgo_info = TsgoToolchainInfo(
            tsgo_binary = binary,
        ),
        # Standard fields consumed by @toolchain_utils//toolchain:resolved.bzl.
        executable = binary,
        variable = "TSGO",
        default = DefaultInfo(
            files = depset([binary]),
            runfiles = ctx.runfiles([binary]),
        ),
    )
    return [toolchain_info]

tsgo_toolchain = rule(
    implementation = _tsgo_toolchain_impl,
    attrs = {
        "tsgo_binary": attr.label(
            doc = "The tsgo CLI binary.",
            mandatory = True,
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
)

# ─── Toolchain resolution helpers ─────────────────────────────────────────────

def get_oxc_toolchain(ctx):
    """Resolves the oxc toolchain from the rule context.

    Args:
        ctx: The rule context.

    Returns:
        OxcToolchainInfo: The resolved oxc toolchain info.
    """
    return ctx.toolchains[OXC_TOOLCHAIN_TYPE].oxc_info

def get_tsgo_toolchain(ctx):
    """Resolves the tsgo toolchain from the rule context.

    Args:
        ctx: The rule context.

    Returns:
        TsgoToolchainInfo: The resolved tsgo toolchain info.
    """
    return ctx.toolchains[TSGO_TOOLCHAIN_TYPE].tsgo_info

# ─── Supported platforms ──────────────────────────────────────────────────────

# The platforms tsgo has a published binary for; keys of
# //platforms:platforms.bzl%PLATFORMS.  Windows is absent from
# @typescript/native-preview.
TSGO_PLATFORMS = ["linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"]

# oxc-bazel needs no such list: it is built from source by rules_rust for
# whichever exec platform the build runs on.

# ─── Toolchain macros ─────────────────────────────────────────────────────────

def declare_oxc_toolchain(name):
    """Declares the oxc toolchain.

    oxc-bazel is built from source by rules_rust, so there is one toolchain
    rather than one per platform: `cfg = "exec"` on the binary already builds
    it for the exec platform, and it constrains neither exec nor target.

    Args:
        name: Name of the generated toolchain target.
    """
    oxc_toolchain(
        name = "{}_impl".format(name),
        oxc_binary = Label("//oxc_cli:oxc-bazel"),
    )
    native.toolchain(
        name = name,
        toolchain = ":{}_impl".format(name),
        toolchain_type = OXC_TOOLCHAIN_TYPE,
    )

def declare_tsgo_toolchains(name, repo_prefix = None):
    """Declares one tsgo toolchain per platform tsgo publishes a binary for.

    tsgo is a compiler: it runs on the exec platform and places no constraint
    on the target platform.

    The per-platform `_impl` targets are tagged manual so that a `//...` build
    does not fetch every platform's tarball; toolchain resolution reaches the
    selected one directly.

    Args:
        name: Base name prefix for the generated targets.
        repo_prefix: Prefix of the per-platform external repositories.
                     Defaults to name.
    """
    if repo_prefix == None:
        repo_prefix = name
    for platform in TSGO_PLATFORMS:
        toolchain_name = "{}_{}".format(name, platform)
        tsgo_toolchain(
            name = "{}_impl".format(toolchain_name),
            tsgo_binary = "@{}_{}//:tsgo".format(repo_prefix, platform),
            tags = ["manual"],
        )
        native.toolchain(
            name = toolchain_name,
            toolchain = ":{}_impl".format(toolchain_name),
            toolchain_type = TSGO_TOOLCHAIN_TYPE,
            exec_compatible_with = constraints(platform),
        )

# ─── tsgo repository rule ─────────────────────────────────────────────────────

_NPM_REGISTRY = "https://registry.npmjs.org"

_TSGO_NPM_PACKAGE = "@typescript/native-preview"

# sha256 of each @typescript/native-preview-<arch> tarball, per version.  A
# version this table does not cover downloads unverified, with a warning.
_TSGO_CHECKSUMS = {
    "7.0.0-dev.20260311.1": {
        "linux_amd64": "e0379b70c1631d2193dc871610adceb6552c43407ea43ff637b642cace956958",
        "linux_arm64": "7806d9089b7367de7098598feee39bab046fceb8991ac46bd33af79a00c56410",
        "darwin_amd64": "7f5a64672732144761025bc41fd9685e0e3004d591ec53055cf7f4de69b0e1d5",
        "darwin_arm64": "c8378be9b3c35560e7c446abaa2665e6b4b75b604ba8deea8042ee6d83391152",
    },
}

def _tsgo_toolchain_repo_impl(repository_ctx):
    version = repository_ctx.attr.version
    platform = repository_ctx.attr.platform

    if platform not in TSGO_PLATFORMS:
        fail("tsgo_toolchain_repo: no tsgo binary is published for platform '{}'. Supported platforms are: {}.".format(
            platform,
            ", ".join(TSGO_PLATFORMS),
        ))

    # e.g. @typescript/native-preview-linux-x64, whose tarball the registry
    # serves from the scope directory under its unscoped basename.
    scoped_pkg = "{}-{}".format(_TSGO_NPM_PACKAGE, npm_arch(platform))
    pkg_base = scoped_pkg.split("/")[1]
    tarball_url = "{registry}/{scoped}/-/{base}-{version}.tgz".format(
        registry = _NPM_REGISTRY,
        scoped = scoped_pkg,
        base = pkg_base,
        version = version,
    )

    checksum = _TSGO_CHECKSUMS.get(version, {}).get(platform, "")
    if not checksum:
        # buildifier: disable=print
        print("WARNING: no sha256 for tsgo {} on {}; downloading unverified.".format(version, platform))

    download_kwargs = {
        "url": tarball_url,
        "stripPrefix": "package",
    }
    if checksum:
        download_kwargs["sha256"] = checksum
    repository_ctx.download_and_extract(**download_kwargs)

    # The npm package ships the binary at lib/tsgo; give it a platform-free
    # label so the toolchain macro does not have to know the layout.
    repository_ctx.file("BUILD.bazel", """\
alias(
    name = "tsgo",
    actual = "lib/tsgo",
    visibility = ["//visibility:public"],
)

exports_files(["lib/tsgo"])
""")

tsgo_toolchain_repo = repository_rule(
    implementation = _tsgo_toolchain_repo_impl,
    attrs = {
        "version": attr.string(
            doc = "Version of @typescript/native-preview to download.",
            mandatory = True,
        ),
        "platform": attr.string(
            doc = "Platform key (see //platforms:platforms.bzl) to download tsgo for.",
            mandatory = True,
        ),
    },
    doc = """Downloads the tsgo binary for one platform.

One repository per platform, so that a build fetches only the tsgo it runs.
""",
)
