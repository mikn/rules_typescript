"""Toolchain definitions for rules_typescript.

Defines toolchain providers and rules for oxc-bazel and tsgo binaries,
plus repository rules that fetch platform-specific binaries.
"""

# ─── Toolchain type labels ────────────────────────────────────────────────────

# These labels must match the toolchain_type() targets declared in
# //ts/toolchain/BUILD.bazel.
OXC_TOOLCHAIN_TYPE = "@rules_typescript//ts/toolchain:oxc_toolchain_type"
TSGO_TOOLCHAIN_TYPE = "@rules_typescript//ts/toolchain:tsgo_toolchain_type"

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

# ─── Platform constraint helpers ──────────────────────────────────────────────

# Public alias so other .bzl files (e.g. runtime.bzl) can import this map
# without duplicating it.
#
# linux_arm64 is included here because:
#  - oxc-bazel is compiled from Rust source via rules_rust, which supports
#    aarch64-unknown-linux-gnu cross-compilation out of the box.
#  - @typescript/native-preview-linux-arm64 publishes tsgo binaries for
#    linux-arm64 (verified: package exists on npm registry with our version).
PLATFORM_CONSTRAINTS = {
    "linux_amd64": {
        "os": "@platforms//os:linux",
        "cpu": "@platforms//cpu:x86_64",
    },
    "linux_arm64": {
        "os": "@platforms//os:linux",
        "cpu": "@platforms//cpu:aarch64",
    },
    "darwin_amd64": {
        "os": "@platforms//os:macos",
        "cpu": "@platforms//cpu:x86_64",
    },
    "darwin_arm64": {
        "os": "@platforms//os:macos",
        "cpu": "@platforms//cpu:aarch64",
    },
}

# ─── Toolchain macros ─────────────────────────────────────────────────────────

def declare_oxc_toolchains(name, repo_name = None):
    """Declares oxc_toolchain targets for all supported platforms.

    Creates one oxc_toolchain + toolchain() pair per platform, suitable for
    registration with register_toolchains().

    Args:
        name: Base name prefix for the generated targets.  Also used as the
              external repository name when repo_name is not provided.
        repo_name: Name of the external repository containing per-platform
                   binaries.  Defaults to name.
    """
    if repo_name == None:
        repo_name = name
    for platform, constraints in PLATFORM_CONSTRAINTS.items():
        toolchain_name = "{}_{}".format(name, platform)
        oxc_toolchain(
            name = "{}_impl".format(toolchain_name),
            oxc_binary = "@{}//:oxc_bazel_{}".format(repo_name, platform),
        )
        native.toolchain(
            name = toolchain_name,
            toolchain = ":{}_impl".format(toolchain_name),
            toolchain_type = OXC_TOOLCHAIN_TYPE,
            target_compatible_with = [
                constraints["os"],
                constraints["cpu"],
            ],
        )

def declare_tsgo_toolchains(name, repo_name = None):
    """Declares tsgo_toolchain targets for all supported platforms.

    Creates one tsgo_toolchain + toolchain() pair per platform, suitable for
    registration with register_toolchains().

    Args:
        name: Base name prefix for the generated targets.  Also used as the
              external repository name when repo_name is not provided.
        repo_name: Name of the external repository containing per-platform
                   binaries.  Defaults to name.
    """
    if repo_name == None:
        repo_name = name
    for platform, constraints in PLATFORM_CONSTRAINTS.items():
        toolchain_name = "{}_{}".format(name, platform)
        tsgo_toolchain(
            name = "{}_impl".format(toolchain_name),
            # Reference the exported file directly (exports_files in the root
            # BUILD of the repo, not a filegroup).  ctx.file.tsgo_binary
            # returns None when the label points to a filegroup; it resolves
            # correctly to the single file when using an exports_files label.
            tsgo_binary = "@{}//:tsgo_{}/lib/tsgo".format(repo_name, platform),
        )
        native.toolchain(
            name = toolchain_name,
            toolchain = ":{}_impl".format(toolchain_name),
            toolchain_type = TSGO_TOOLCHAIN_TYPE,
            target_compatible_with = [
                constraints["os"],
                constraints["cpu"],
            ],
        )

# ─── Repository rules ─────────────────────────────────────────────────────────

def _oxc_toolchain_repo_impl(repository_ctx):
    """Repository rule implementation for the oxc toolchain.

    Creates per-platform alias targets that all point to the Bazel-built
    @rules_typescript//oxc_cli:oxc-bazel binary.  Cross-compilation support
    (platform-specific binaries) is a future concern.
    """
    build_content = """\
package(default_visibility = ["//visibility:public"])
"""
    for platform in repository_ctx.attr.platforms:
        build_content += """
alias(
    name = "oxc_bazel_{platform}",
    actual = "@rules_typescript//oxc_cli:oxc-bazel",
)
""".format(platform = platform)

    repository_ctx.file("BUILD.bazel", content = build_content)

oxc_toolchain_repo = repository_rule(
    implementation = _oxc_toolchain_repo_impl,
    attrs = {
        "platforms": attr.string_list(
            doc = "List of platform keys to generate toolchain targets for.",
            default = ["linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"],
        ),
    },
    doc = """Generates per-platform alias targets pointing to the Bazel-built oxc-bazel binary.

oxc-bazel is a Rust binary built from source by rules_rust, which means it
builds natively for whatever exec platform Bazel is running on.  The alias
targets simply point at @rules_typescript//oxc_cli:oxc-bazel, so no
pre-built binaries are needed.

Future: downloads platform-specific release archives from GitHub Releases.
""",
)

# ─── tsgo repository rule ─────────────────────────────────────────────────────

# npm registry base URL
_NPM_REGISTRY = "https://registry.npmjs.org"

# Package that contains tsgo platform-specific binaries
_TSGO_NPM_PACKAGE = "@typescript/native-preview"

# Map from our platform key to the npm optional-dependency package suffix
_TSGO_NPM_ARCH = {
    "linux_amd64": "linux-x64",
    "linux_arm64": "linux-arm64",
    "darwin_amd64": "darwin-x64",
    "darwin_arm64": "darwin-arm64",
}

# sha256 checksums for @typescript/native-preview optional deps.
# These cover version 7.0.0-dev.20260311.1.
# Format: { platform_key: sha256_of_tarball }
_TSGO_CHECKSUMS = {
    "linux_amd64": "e0379b70c1631d2193dc871610adceb6552c43407ea43ff637b642cace956958",
    "linux_arm64": "7806d9089b7367de7098598feee39bab046fceb8991ac46bd33af79a00c56410",
    "darwin_amd64": "7f5a64672732144761025bc41fd9685e0e3004d591ec53055cf7f4de69b0e1d5",
    "darwin_arm64": "c8378be9b3c35560e7c446abaa2665e6b4b75b604ba8deea8042ee6d83391152",
}

def _tsgo_toolchain_repo_impl(repository_ctx):
    """Repository rule implementation for the tsgo toolchain.

    Downloads the @typescript/native-preview npm package for each platform and
    extracts the `tsgo` binary from it.
    """
    version = repository_ctx.attr.version
    platforms = repository_ctx.attr.platforms

    build_content = """# Auto-generated by tsgo_toolchain_repo
package(default_visibility = ["//visibility:public"])
"""

    for platform in platforms:
        npm_arch = _TSGO_NPM_ARCH.get(platform)
        if not npm_arch:
            fail("tsgo_toolchain_repo: unknown platform key '{}'. Supported platforms are: {}. Remove the unsupported platform from the `platforms` attr.".format(
                platform,
                ", ".join(_TSGO_NPM_ARCH.keys()),
            ))

        # The optional-dependency package name for this platform.
        # e.g. @typescript/native-preview-linux-x64
        scoped_pkg = _TSGO_NPM_PACKAGE + "-" + npm_arch
        # npm encodes scopes as %40 in the tarball URL path segment, but the
        # registry path uses the unescaped @-prefixed form under the scope dir.
        # URL shape: https://registry.npmjs.org/@typescript/native-preview-linux-x64/-/native-preview-linux-x64-VERSION.tgz
        pkg_base = scoped_pkg.split("/")[1]  # "native-preview-linux-x64"
        tarball_url = "{registry}/{scoped}/-/{base}-{version}.tgz".format(
            registry = _NPM_REGISTRY,
            scoped = scoped_pkg,
            base = pkg_base,
            version = version,
        )

        output_dir = "tsgo_{}".format(platform)
        checksum = _TSGO_CHECKSUMS.get(platform, "")

        # Download and extract the npm tarball into a subdirectory per platform.
        if not checksum:
            # buildifier: disable=print
            print("WARNING: No sha256 checksum for tsgo platform '{}'. Set _TSGO_CHECKSUMS for production use.".format(platform))

        download_kwargs = {
            "url": tarball_url,
            "output": output_dir,
            "stripPrefix": "package",
        }
        if checksum:
            download_kwargs["sha256"] = checksum
        repository_ctx.download_and_extract(**download_kwargs)

        # The binary inside the npm package lives at lib/tsgo (or tsgo on
        # Windows, but we don't support Windows yet).
        binary_path = "{}/lib/tsgo".format(output_dir)

        # Ensure the binary is executable (download_and_extract preserves bits
        # from the archive, but be explicit).
        repository_ctx.execute(["chmod", "+x", binary_path])

        build_content += """
exports_files(["{binary_path}"])
""".format(binary_path = binary_path)

    repository_ctx.file("BUILD.bazel", build_content)

tsgo_toolchain_repo = repository_rule(
    implementation = _tsgo_toolchain_repo_impl,
    attrs = {
        "version": attr.string(
            doc = "Version of @typescript/native-preview to download.",
            mandatory = True,
        ),
        "platforms": attr.string_list(
            doc = "List of platform keys to download tsgo for.",
            default = ["linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"],
        ),
    },
    doc = """Downloads tsgo binaries from the @typescript/native-preview npm package.

The npm package ships platform-specific optional dependencies; this rule
downloads the appropriate tarball per platform and extracts the tsgo binary.
""",
)
