"""Module extension for the rules_typescript compiler toolchains."""

load("//ts/private:toolchain.bzl", "TSGO_PLATFORMS", "tsgo_toolchain_repo")

# The tsgo release used unless the root module pins another with ts.tsgo().
_DEFAULT_TSGO_VERSION = "7.0.0-dev.20260311.1"

# One repository per platform, named "<prefix>_<platform>" to match the labels
# declare_tsgo_toolchains() generates.  rules_typescript itself is the only
# module that use_repo's them: a consumer reaches the toolchains through
# register_toolchains("@rules_typescript//ts/toolchain:all"), which resolves
# these names in rules_typescript's own repo mapping.
_TSGO_REPO_PREFIX = "tsgo"

def _ts_impl(module_ctx):
    tsgo_version = _DEFAULT_TSGO_VERSION
    for mod in module_ctx.modules:
        if mod.is_root:
            for tag in mod.tags.tsgo:
                tsgo_version = tag.version

    for platform in TSGO_PLATFORMS:
        tsgo_toolchain_repo(
            name = "{}_{}".format(_TSGO_REPO_PREFIX, platform),
            version = tsgo_version,
            platform = platform,
        )

_tsgo_version_tag = tag_class(
    attrs = {
        "version": attr.string(
            mandatory = True,
            doc = "Version of @typescript/native-preview (tsgo) to use.",
        ),
    },
    doc = """Pin the tsgo type-checker version used by this workspace.

Only the root module's ts.tsgo() call takes effect.  Transitive dependencies
that also call ts.tsgo() are ignored.

Example:
    ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
    ts.tsgo(version = "7.0.0-dev.20260311.1")
""",
)

# Deliberately neither os_dependent nor arch_dependent: it declares a repo for
# every supported platform and reads nothing about the host, so its result --
# and MODULE.bazel.lock -- is identical everywhere.  Only the repo Bazel
# actually fetches depends on where the build runs.
ts = module_extension(
    implementation = _ts_impl,
    tag_classes = {
        "tsgo": _tsgo_version_tag,
    },
)
