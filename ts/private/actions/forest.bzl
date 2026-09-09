"""The node_modules forest tsgo resolves npm packages through.

One entry per resolution, the target's own deps flat: a dep's emitted .d.ts
imports the packages the dep declared. node_modules.bzl lays the tree out.
"""

load(
    "//ts/private:node_modules.bzl",
    "build_node_modules_action",
    "collect_npm_packages",
)

def forest_packages(direct_npm_infos, dep_npm_package_sets):
    """The packages the forest links: the direct npm deps, then the closures."""
    closure = depset(transitive = dep_npm_package_sets, order = "postorder")
    return collect_npm_packages(direct_npm_infos + closure.to_list())

def forest_action(ctx, packages):
    """Builds <name>/node_modules from `packages`; returns the tree."""
    return build_node_modules_action(
        ctx,
        packages,
        [npm_info.all_files for npm_info in packages],
        output_name = "{}/node_modules".format(ctx.label.name),
    )
