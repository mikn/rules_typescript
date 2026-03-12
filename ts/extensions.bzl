"""Module extensions for rules_typescript."""

load("//ts:repositories.bzl", "oxc_toolchain_repository", "tsgo_toolchain_repository")

_OXC_PLATFORMS = {
    "linux_amd64": struct(
        os = "linux",
        cpu = "x86_64",
        triple = "x86_64-unknown-linux-gnu",
    ),
    "linux_arm64": struct(
        os = "linux",
        cpu = "aarch64",
        triple = "aarch64-unknown-linux-gnu",
    ),
    "darwin_amd64": struct(
        os = "macos",
        cpu = "x86_64",
        triple = "x86_64-apple-darwin",
    ),
    "darwin_arm64": struct(
        os = "macos",
        cpu = "aarch64",
        triple = "aarch64-apple-darwin",
    ),
}

_TSGO_PLATFORMS = {
    "linux_amd64": struct(
        os = "linux",
        cpu = "x86_64",
        npm_arch = "linux-x64",
    ),
    "linux_arm64": struct(
        os = "linux",
        cpu = "aarch64",
        npm_arch = "linux-arm64",
    ),
    "darwin_amd64": struct(
        os = "macos",
        cpu = "x86_64",
        npm_arch = "darwin-x64",
    ),
    "darwin_arm64": struct(
        os = "macos",
        cpu = "aarch64",
        npm_arch = "darwin-arm64",
    ),
}

# Default versions used when the consumer does not specify overrides.
_DEFAULT_TSGO_VERSION = "7.0.0-dev.20260311.1"
_DEFAULT_NODE_VERSION = "22.14.0"

def _ts_impl(module_ctx):
    # Collect version overrides from the root module.  The root module's
    # ts.tsgo(version=...) and ts.node(version=...) tags take priority over
    # rules_typescript's own defaults.  Non-root module overrides are ignored
    # to preserve consistent behaviour across the dependency graph.
    tsgo_version = _DEFAULT_TSGO_VERSION

    for mod in module_ctx.modules:
        if mod.is_root:
            for tag in mod.tags.tsgo:
                tsgo_version = tag.version

    for mod in module_ctx.modules:
        for toolchain in mod.tags.oxc_toolchain:
            oxc_toolchain_repository(
                name = toolchain.name,
                # platforms attr is attr.string_list — pass keys only.
                platforms = list(_OXC_PLATFORMS.keys()),
            )

        for toolchain in mod.tags.tsgo_toolchain:
            # The tsgo_toolchain tag carries an explicit version so that
            # rules_typescript's own MODULE.bazel can declare the default.
            # When the root module has overridden via ts.tsgo(version=...),
            # use that version instead.
            effective_version = tsgo_version if toolchain.name == "tsgo" else toolchain.tsgo_version
            tsgo_toolchain_repository(
                name = toolchain.name,
                version = effective_version,
                # platforms attr is attr.string_list — pass keys only.
                platforms = list(_TSGO_PLATFORMS.keys()),
            )

_oxc_toolchain_tag = tag_class(attrs = {
    "name": attr.string(mandatory = True),
})

_tsgo_toolchain_tag = tag_class(attrs = {
    "name": attr.string(mandatory = True),
    "tsgo_version": attr.string(mandatory = True),
})

# Consumer-facing version-pin tag classes.
#
# ts.tsgo(version = "...") lets consumers pin a specific tsgo release without
# having to re-declare the full tsgo_toolchain tag.  The root module's value
# wins; non-root modules are silently ignored.
#
# ts.node(version = "...") is a no-op in the extension itself — Node.js is
# configured through rules_nodejs's own extension.  The tag exists so that
# consumers have a single, discoverable place to express intent.  The README
# explains the corresponding node.toolchain(...) call that actually applies the
# version.
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

_node_version_tag = tag_class(
    attrs = {
        "version": attr.string(
            mandatory = True,
            doc = "Informational Node.js version.  See README for the matching node.toolchain() call.",
        ),
    },
    doc = """Informational tag for documenting the intended Node.js version.

Node.js is managed by rules_nodejs, not by this extension.  To pin a specific
version, add the following to your MODULE.bazel:

    node = use_extension("@rules_nodejs//nodejs:extensions.bzl", "node")
    node.toolchain(name = "nodejs", node_version = "22.14.0")
    use_repo(node, "nodejs_linux_amd64", "nodejs_linux_arm64",
             "nodejs_darwin_amd64", "nodejs_darwin_arm64", "nodejs_windows_amd64")

The ts.node() tag itself is a no-op.  It exists only as a discoverable marker
so that consumers can express version intent in a consistent place.
""",
)

ts = module_extension(
    implementation = _ts_impl,
    tag_classes = {
        "oxc_toolchain": _oxc_toolchain_tag,
        "tsgo_toolchain": _tsgo_toolchain_tag,
        "tsgo": _tsgo_version_tag,
        "node": _node_version_tag,
    },
)
