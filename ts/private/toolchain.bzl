"""Toolchain definitions for rules_typescript.

Defines the toolchain providers and rules for the oxc-bazel and tsgo
compilers, and the repository rule that fetches a TypeScript compiler binary.
"""

load("//npm/private:npmrc_auth.bzl", "npmrc_auth")
load("//platforms:platforms.bzl", "constraints")

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

# Keys of //platforms:platforms.bzl%PLATFORMS. Both compiler packages publish
# win32 builds too; Windows is unsupported here (COMPATIBILITY.md#windows).
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

def _auth_for_fetch(repository_ctx, url):
    npmrc = repository_ctx.attr.npmrc
    if not npmrc:
        return {}
    repository_ctx.watch(npmrc)
    return npmrc_auth(repository_ctx.read(npmrc), url, repository_ctx.getenv, str(npmrc))

def _tsgo_toolchain_repo_impl(repository_ctx):
    url = repository_ctx.attr.url
    integrity = repository_ctx.attr.integrity
    binary = repository_ctx.attr.binary

    download_kwargs = {
        "url": url,
        "stripPrefix": "package",
        "auth": _auth_for_fetch(repository_ctx, url),
    }
    if integrity:
        download_kwargs["integrity"] = integrity
    else:
        # buildifier: disable=print
        print("WARNING: tsgo from {} has no integrity; downloading unverified. ts.tsgo(pnpm_lock = ...) verifies every download.".format(url))
    repository_ctx.download_and_extract(**download_kwargs)

    if not repository_ctx.path("lib/" + binary).exists:
        fail("tsgo_toolchain_repo: the tarball at {} has no lib/{}. The platform packages of `typescript` ship lib/tsc and those of @typescript/native-preview lib/tsgo; ts.tsgo(package = ...) names which.".format(url, binary))

    # Platform-free label over the binary, so the toolchain macro does not have
    # to know the layout or which of the two compiler packages this is.
    repository_ctx.file("BUILD.bazel", """\
alias(
    name = "tsgo",
    actual = "lib/{binary}",
    visibility = ["//visibility:public"],
)

exports_files(["lib/{binary}"])
""".format(binary = binary))

tsgo_toolchain_repo = repository_rule(
    implementation = _tsgo_toolchain_repo_impl,
    attrs = {
        "url": attr.string(
            doc = "The platform package's tarball: `package/lib/<binary>` inside.",
            mandatory = True,
        ),
        "integrity": attr.string(
            doc = "The tarball's SRI integrity, as pnpm writes it. Empty downloads unverified.",
        ),
        "binary": attr.string(
            doc = "The compiler's file name under lib/: tsc or tsgo.",
            mandatory = True,
        ),
        "npmrc": attr.label(
            allow_single_file = True,
            doc = "The workspace .npmrc, read at fetch time for the credentials of the " +
                  "registry `url` points at, so no token is ever an attribute value.",
        ),
    },
    doc = """Downloads the TypeScript compiler binary for one platform.

One repository per platform, so that a build fetches only the compiler it runs.
""",
)
