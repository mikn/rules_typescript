"""Repository rules for rules_typescript toolchains."""

load("//ts/private:toolchain.bzl", "oxc_toolchain_repo", "tsgo_toolchain_repo")

def rules_typescript_dependencies():
    """Stub preserved for API compatibility.

    rules_typescript requires bzlmod (MODULE.bazel). WORKSPACE-based setup is
    not supported. If you are seeing this error, migrate to bzlmod:

        bazel_dep(name = "rules_typescript", version = "...")

    and remove any call to rules_typescript_dependencies() from your WORKSPACE.
    """
    fail(
        "rules_typescript_dependencies() is not supported. " +
        "rules_typescript requires bzlmod. " +
        "Add `bazel_dep(name = \"rules_typescript\", ...)` to your MODULE.bazel " +
        "and remove this call from your WORKSPACE.",
    )

# Re-export for use in extensions.bzl
oxc_toolchain_repository = oxc_toolchain_repo
tsgo_toolchain_repository = tsgo_toolchain_repo
