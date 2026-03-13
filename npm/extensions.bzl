"""Module extension for npm dependency management.

Usage in MODULE.bazel:
    npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
    npm.pnpm(version = "10.32.1")          # optional; defaults to _DEFAULT_PNPM_VERSION
    npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
    use_repo(npm, "npm", "pnpm")

Design note: when both the root workspace and rules_typescript register a repo
with the same name (e.g. "npm"), the root workspace wins.  This lets consumers
provide their own pnpm-lock.yaml while rules_typescript ships a default lockfile
for its own tests.  Non-root registrations for a name are silently skipped when
the root module has already claimed that name.
"""

load("//ts/private:npm_translate_lock.bzl", "npm_translate_lock")
load("//ts/private:pnpm.bzl", "DEFAULT_PNPM_VERSION", "pnpm_repo")

def _npm_impl(module_ctx):
    # Collect registrations in two passes:
    #   1. Root-module registrations take priority.
    #   2. Non-root registrations only fill in names not already claimed.
    claimed = {}  # name → pnpm_lock label

    # Pass 1: root module.
    for mod in module_ctx.modules:
        if not mod.is_root:
            continue
        for lock_tag in mod.tags.translate_lock:
            if lock_tag.name not in claimed:
                claimed[lock_tag.name] = lock_tag.pnpm_lock

    # Pass 2: non-root modules (fill in unclaimed names only).
    for mod in module_ctx.modules:
        if mod.is_root:
            continue
        for lock_tag in mod.tags.translate_lock:
            if lock_tag.name not in claimed:
                claimed[lock_tag.name] = lock_tag.pnpm_lock

    for name, pnpm_lock in claimed.items():
        npm_translate_lock(
            name = name,
            pnpm_lock = pnpm_lock,
        )

    # ── pnpm hermetic binary ──────────────────────────────────────────────────
    # The root module's npm.pnpm(version=...) tag sets the version; other
    # modules are ignored.  When no npm.pnpm() tag is present at all, we still
    # download the default version so that rules_typescript's own tests have a
    # hermetic pnpm available.
    pnpm_version = DEFAULT_PNPM_VERSION
    pnpm_repo_name = "pnpm"

    for mod in module_ctx.modules:
        if not mod.is_root:
            continue
        for tag in mod.tags.pnpm:
            pnpm_version = tag.version
            if tag.name:
                pnpm_repo_name = tag.name

    pnpm_repo(
        name = pnpm_repo_name,
        version = pnpm_version,
    )

_translate_lock_tag = tag_class(attrs = {
    "name": attr.string(default = "npm"),
    "pnpm_lock": attr.label(mandatory = True, allow_single_file = True),
})

_pnpm_tag = tag_class(
    attrs = {
        "name": attr.string(
            default = "pnpm",
            doc = "Name of the external repository that will contain the pnpm binary (default 'pnpm').",
        ),
        "version": attr.string(
            default = DEFAULT_PNPM_VERSION,
            doc = "pnpm version to download (e.g. '10.32.1'). Defaults to the bundled stable version.",
        ),
    },
    doc = """\
Pin the hermetic pnpm version used by this workspace.

Only the root module's npm.pnpm() call takes effect.  Transitive dependencies
that also call npm.pnpm() are ignored.

When this tag is absent, rules_typescript downloads the built-in default
pnpm version automatically, so no explicit call is needed unless you want to
override the version.

Example:
    npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
    npm.pnpm(version = "10.32.1")
    use_repo(npm, "pnpm")
""",
)

npm = module_extension(
    implementation = _npm_impl,
    tag_classes = {
        "translate_lock": _translate_lock_tag,
        "pnpm": _pnpm_tag,
    },
)
