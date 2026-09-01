"""The hub target for a pnpm `link:` dependency: a workspace member, named.

A `workspace:*` dependency resolves to a target in the consumer's own repository,
and the hub used to hold an `alias` to it. An alias cannot carry the name: Bazel
resolves it before any rule implementation runs, so `@npm//:shared` reaches
ts_compile as the aliased target itself and the only record that the member is
imported as `shared` is the alias label, which nothing can read. Every workspace
member therefore had to restate its own npm name as `module_name` -- a second
place for it to be wrong, in a file the lockfile does not reach.

This rule is that alias with the name attached: it forwards the member's
providers unchanged, re-declares TsModuleInfo under the name the lockfile says
the member is imported by, and describes the member as an npm package so that
the runtime tree can stage it.

That last part is what makes the link real at run time. Type-checking resolves
the member through TsModuleInfo, which reads declarations straight out of the
build graph; a node_modules tree is built from NpmPackageInfo instead, so a
member that carried only the first type-checks and then dies in Node's resolver.

A member is not an extracted tarball, and the two fields that assume one say so:

  package_dir  stays None. It is the tsconfig `paths` anchor for a published
               package, and a member's `paths` entry already comes from
               TsModuleInfo; a second anchor pointing at the directory holding
               only the manifest below would resolve the name to nothing.
  package_root is the compiling target's output directory rather than the
               directory package.json sits in, because the member's files hang
               off the former and the manifest is generated beside neither.

The manifest is generated because Node needs one: the compiled member is ESM,
and a directory holding no package.json is CommonJS to Node's loader whatever it
contains. It names no entry point, so Node falls back to `index.js` at the
package root -- which is where the `link:` target lands a member whose entry is
an `index`, and is not where it lands one whose entry is named anything else.

What the member's OWN package.json says it is entered through arrives as
`module_paths`, and it is what decides the `paths` entries the member gets: a
hardcoded `<root>/index.d.ts` is that answer only for the members whose entry
happens to be an index at the root. Read in the module extension and rebased
here, because this rule is where the two halves meet -- what the manifest says,
and the package of the target that actually compiles the member. See
npm/private/member_paths.bzl.
"""

load("//npm/private:member_paths.bzl", "declared_paths", "target_offset")
load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsDeclarationInfo")
load("//ts/private:ts_compile.bzl", "TsModuleInfo")

_FORWARDED = [JsInfo, TsDeclarationInfo, CssInfo, CssModuleInfo, AssetInfo]

# A `link:` records no version -- pnpm resolves a member by path. The tree needs
# one only to tell two resolutions of a name apart, and a name pnpm links has
# exactly one.
_WORKSPACE_VERSION = "0.0.0"

_WorkspaceNpmDeps = provider(
    doc = "The npm packages a workspace member's own dependency graph reaches.",
    fields = {
        "direct": "list of NpmPackageInfo: the npm packages this target depends on directly.",
        "closure": "depset of NpmPackageInfo: those, plus every npm package reachable through them.",
    },
)

def _workspace_npm_deps_impl(target, ctx):
    if NpmPackageInfo in target:
        return []

    direct = []
    transitive = []
    for dep in getattr(ctx.rule.attr, "deps", []):
        if NpmPackageInfo in dep:
            direct.append(dep[NpmPackageInfo])
            transitive.append(dep[NpmPackageInfo].transitive_deps)
        elif _WorkspaceNpmDeps in dep:
            transitive.append(dep[_WorkspaceNpmDeps].closure)
    return [_WorkspaceNpmDeps(direct = direct, closure = depset(direct, transitive = transitive))]

_workspace_npm_deps = aspect(
    implementation = _workspace_npm_deps_impl,
    attr_aspects = ["deps"],
    doc = "Collects the npm packages a workspace member imports, which its own " +
          "providers do not carry: ts_compile forwards declarations and files, " +
          "not the package identities a node_modules tree places directories by.",
)

def _label_text(label):
    text = str(label)
    return text[2:] if text.startswith("@@//") else text

def _output_root(ctx, member):
    return "/".join([
        p
        for p in [ctx.bin_dir.path, member.label.workspace_root, member.label.package]
        if p
    ])

def _npm_package_info(ctx, member):
    manifest = ctx.actions.declare_file("{}/package.json".format(ctx.label.name))
    ctx.actions.write(
        output = manifest,
        content = json.encode({"name": ctx.attr.package_name, "type": "module"}) + "\n",
    )

    js = member[JsInfo] if JsInfo in member else None
    npm = member[_WorkspaceNpmDeps] if _WorkspaceNpmDeps in member else None
    direct_deps = npm.direct if npm else []
    file_sets = [js.js_files, js.js_map_files] if js else []

    return NpmPackageInfo(
        package_name = ctx.attr.package_name,
        package_version = _WORKSPACE_VERSION,
        peer_id = "",
        package_dir = None,
        package_root = _output_root(ctx, member),
        all_files = depset([manifest], transitive = file_sets),
        js_files = js.js_files if js else depset(),
        json_files = depset(),
        declaration_files = member[TsDeclarationInfo].declaration_files if TsDeclarationInfo in member else depset(),
        direct_deps = direct_deps,
        transitive_deps = npm.closure if npm else depset(),
        transitive_package_dirs = depset(
            transitive = [dep.transitive_package_dirs for dep in direct_deps],
        ),
        exports_types_file = None,
        subpath_types = {},
        ambient_types_file = None,
        types_package_dir = None,
    )

def _npm_workspace_package_impl(ctx):
    member = ctx.attr.target

    providers = [
        DefaultInfo(
            files = member[DefaultInfo].files,
            runfiles = member[DefaultInfo].default_runfiles,
        ),
        _npm_package_info(ctx, member),
    ]

    # A member with no TsModuleInfo has no name to attach -- a css_module or an
    # asset_library linked by `workspace:*` is a legitimate dependency, and the
    # alias this rule replaced forwarded it, so failing here would be a
    # regression rather than a diagnosis.
    if TsModuleInfo in member:
        module = member[TsModuleInfo]
        declared = declared_paths(
            json.decode(ctx.attr.module_paths) if ctx.attr.module_paths else {},
            target_offset(ctx.attr.member_dir, member.label.package),
        )
        providers.append(TsModuleInfo(
            module_name = ctx.attr.package_name,
            label = _label_text(ctx.label),
            declaration_root = module.declaration_root,
            source_root = module.source_root,
            declared_paths = declared,
            transitive_modules = depset(
                [struct(
                    module_name = ctx.attr.package_name,
                    label = _label_text(ctx.label),
                    declaration_root = module.declaration_root,
                    source_root = module.source_root,
                    declared_paths = declared,
                )],
                transitive = [module.transitive_modules],
            ),
        ))

    for provider in _FORWARDED:
        if provider in member:
            providers.append(member[provider])
    return providers

npm_workspace_package = rule(
    implementation = _npm_workspace_package_impl,
    attrs = {
        "package_name": attr.string(
            mandatory = True,
            doc = "The npm package name the lockfile imports the member under.",
        ),
        "target": attr.label(
            mandatory = True,
            aspects = [_workspace_npm_deps],
            doc = "The workspace member's own target, as the `link:` path names it.",
        ),
        "module_paths": attr.string(
            doc = "What the member's own package.json says each of its specifiers " +
                  "resolves to, as the JSON of a specifier -> declarations map -- " +
                  "npm/private/member_paths.bzl works it out in the module extension, " +
                  "where the manifest is a file that can still be read. Text rather " +
                  "than a label because `paths` is written during analysis, and " +
                  "nothing reads a file there.",
        ),
        "member_dir": attr.string(
            doc = "The member's directory from the workspace root, which the paths in " +
                  "`module_paths` are relative to. It is not always `target`'s " +
                  "package: a member whose entry sits in a subdirectory is compiled " +
                  "by a target in that subdirectory.",
        ),
    },
    doc = "Generated by npm_hub for each pnpm `link:` dependency; not for hand use.",
)
