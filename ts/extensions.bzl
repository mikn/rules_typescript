"""Module extension for the rules_typescript compiler toolchains."""

load("//npm/private:npm_translate_lock.bzl", "npmrc_registries")
load("//ts/private:toolchain.bzl", "TSGO_PLATFORMS", "tsgo_toolchain_repo")
load("//ts/private:tsgo_lock.bzl", "tsgo_from_pnpm_lock", "tsgo_from_version")

# Label(), not a string, so it resolves in this repository from any consumer.
_DEFAULT_TSGO_LOCK = Label("//ts/private/tsgo:pnpm-lock.yaml")

# One repository per platform, named "<prefix>_<platform>" to match the labels
# declare_tsgo_toolchains() generates.  rules_typescript itself is the only
# module that use_repo's them: a consumer reaches the toolchains through
# register_toolchains("@rules_typescript//ts/toolchain:all"), which resolves
# these names in rules_typescript's own repo mapping.
_TSGO_REPO_PREFIX = "tsgo"

def _registries(module_ctx, npmrc):
    return npmrc_registries(module_ctx.read(npmrc)) if npmrc else {}

def _from_lock(module_ctx, pnpm_lock, package, npmrc):
    return tsgo_from_pnpm_lock(
        module_ctx.read(pnpm_lock),
        package,
        TSGO_PLATFORMS,
        str(pnpm_lock),
        _registries(module_ctx, npmrc),
    )

def _spec_for(module_ctx, tag):
    if tag == None:
        return _from_lock(module_ctx, _DEFAULT_TSGO_LOCK, "typescript", None)
    if tag.pnpm_lock and tag.version:
        fail("ts.tsgo(): set pnpm_lock or version, not both. pnpm_lock reads the version and the integrity of every download from the lockfile; version names a release and downloads it unverified.")
    if tag.pnpm_lock:
        return _from_lock(module_ctx, tag.pnpm_lock, tag.package, tag.npmrc)
    if tag.version:
        return tsgo_from_version(tag.package, tag.version, TSGO_PLATFORMS, _registries(module_ctx, tag.npmrc))
    fail("ts.tsgo(): set pnpm_lock (the lockfile whose TypeScript the toolchain fetches, verified) or version (a release to download unverified).")

def _ts_impl(module_ctx):
    tag = None
    for mod in module_ctx.modules:
        if mod.is_root:
            for candidate in mod.tags.tsgo:
                tag = candidate

    spec = _spec_for(module_ctx, tag)
    if spec.error:
        fail(spec.error)

    for platform in TSGO_PLATFORMS:
        resolved = spec.platforms[platform]
        tsgo_toolchain_repo(
            name = "{}_{}".format(_TSGO_REPO_PREFIX, platform),
            url = resolved.url,
            integrity = resolved.integrity,
            binary = spec.binary,
            npmrc = tag.npmrc if tag != None else None,
        )

_tsgo_tag = tag_class(
    attrs = {
        "pnpm_lock": attr.label(
            allow_single_file = True,
            doc = """The pnpm-lock.yaml whose TypeScript the toolchain fetches.

The root importer's entry for `package` names the version, and the lockfile's
`packages:` entries for its platform packages carry the tarball and the
integrity of each compiler binary, which Bazel verifies. A `pnpm install` that
moves the version moves the toolchain.""",
        ),
        "package": attr.string(
            default = "typescript",
            doc = """The npm package the compiler comes from: `typescript` (a TypeScript 7
release, whose binary is `tsc`) or `@typescript/native-preview` (a nightly, whose
binary is `tsgo`).""",
        ),
        "version": attr.string(
            doc = """A release of `package` to download from the registry, unverified.

The alternative to `pnpm_lock` for a build no lockfile describes: a consumer
without pnpm, or a nightly being bisected. Nothing checks the bytes.""",
        ),
        "npmrc": attr.label(
            allow_single_file = True,
            doc = """The workspace .npmrc, when the platform packages come from anywhere but
registry.npmjs.org. `registry=` and `@typescript:registry=` decide the tarball
host and `//host/:_authToken=` its credentials, as they do for npm.translate_lock;
the token is read at fetch time and never becomes an attribute value.""",
        ),
    },
    doc = """Which TypeScript compiler the tsgo toolchain runs.

Only the root module's ts.tsgo() call takes effect.  Transitive dependencies
that also call ts.tsgo() are ignored.  With no call at all the toolchain is the
`typescript` release rules_typescript's own ts/private/tsgo/pnpm-lock.yaml pins.

Example:
    ts = use_extension("@rules_typescript//ts:extensions.bzl", "ts")
    ts.tsgo(pnpm_lock = "//:pnpm-lock.yaml")

The unverified alternative, for a version no lockfile states:
    ts.tsgo(version = "7.0.2")
    ts.tsgo(package = "@typescript/native-preview", version = "7.0.0-dev.20260311.1")
""",
)

# Deliberately neither os_dependent nor arch_dependent: it declares a repo for
# every supported platform and reads nothing about the host, so its result --
# and MODULE.bazel.lock -- is identical everywhere.  Only the repo Bazel
# actually fetches depends on where the build runs.
ts = module_extension(
    implementation = _ts_impl,
    tag_classes = {
        "tsgo": _tsgo_tag,
    },
)
