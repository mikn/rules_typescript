"""Module extension for npm dependency management.

Usage in MODULE.bazel:
    npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
    npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
    use_repo(npm, "npm")

Design note: when both the root workspace and rules_typescript register a repo
with the same name (e.g. "npm"), the root workspace wins.  This lets consumers
provide their own pnpm-lock.yaml while rules_typescript ships a default lockfile
for its own tests.  Non-root registrations for a name are silently skipped when
the root module has already claimed that name.
"""

load("//ts/private:npm_translate_lock.bzl", "npm_translate_lock")

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

_translate_lock_tag = tag_class(attrs = {
    "name": attr.string(default = "npm"),
    "pnpm_lock": attr.label(mandatory = True, allow_single_file = True),
})

npm = module_extension(
    implementation = _npm_impl,
    tag_classes = {
        "translate_lock": _translate_lock_tag,
    },
)
