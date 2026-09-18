"""The crates oxc-bazel is built from.

tools/vendor_crates.sh renders them into oxc_cli/crates at the rules_rust floor
(COMPATIBILITY.md § rules_rust). `crates_hub` is that package as a repository
and `oxc_crates` declares the crate archives the rendering names, so a
consumer's own rules_rust never renders or checks them.
"""

load("//oxc_cli/crates:defs.bzl", "crate_repositories")

def _crates_hub_impl(repository_ctx):
    for name in ("BUILD.bazel", "defs.bzl"):
        repository_ctx.file(
            name,
            repository_ctx.read(Label("//oxc_cli/crates:" + name)),
        )

crates_hub = repository_rule(
    doc = "The rendered hub package oxc_cli/crates as a repository.",
    implementation = _crates_hub_impl,
)

def _oxc_crates_impl(module_ctx):
    direct = crate_repositories()
    return module_ctx.extension_metadata(
        root_module_direct_deps = [dep.repo for dep in direct],
        root_module_direct_dev_deps = [],
        reproducible = True,
    )

oxc_crates = module_extension(
    doc = "The crate archives oxc_cli/crates names.",
    implementation = _oxc_crates_impl,
)
