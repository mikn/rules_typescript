"""The hub's view of a pnpm workspace member: an npm package, named.

A `workspace:*` dependency resolves to a target in the consumer's own repository,
and the hub used to hold an `alias` to it. An alias cannot carry the name: Bazel
resolves it before any rule implementation runs, so `@npm//:shared` would reach a
consumer as the aliased target itself and the only record that the member is
imported as `shared` would be the alias label, which nothing can read.

This rule is that alias with the name attached. It forwards the member's TsInfo
unchanged and describes the member as an npm package, so that the
type-check forest and the runtime tree link it at `node_modules/<name>`: the
member's package.json as built beside the member's `.js` and `.d.ts`, at the
paths the manifest names, and its data srcs at their package-relative paths,
where the `.js` reaches them, its own package.json excepted. tsc maps a `.js`
target to the `.d.ts` beside it and node runs the `.js`, so one manifest serves
both.

Two fields of NpmPackageInfo that assume an extracted tarball say otherwise:

  package_dir  is None. A member has no package.json in an external repository;
               the view writes the one it links.
  package_root is the member's directory under bazel-bin, where the compiling
               target's outputs hang off, and not that target's package: a
               member whose entry sits in a subdirectory is compiled by a target
               in that subdirectory, and its manifest names `src/index.js`.
"""

load("//npm/private:member_manifest.bzl", "member_manifest_json")
load(
    "//ts/private:providers.bzl",
    "NpmPackageInfo",
    "TsConfigInfo",
    "TsInfo",
)

# A `link:` records no version -- pnpm resolves a member by path. The tree needs
# one only to tell two resolutions of a name apart, and a member has exactly one.
_WORKSPACE_VERSION = "0.0.0"

_WorkspaceNpmDeps = provider(
    doc = "The npm packages a workspace member depends on directly.",
    fields = {
        "direct": "list of NpmPackageInfo: the npm packages this target " +
                  "depends on directly.",
    },
)

def _workspace_npm_deps_impl(target, ctx):
    return [_WorkspaceNpmDeps(direct = [
        dep[NpmPackageInfo]
        for dep in getattr(ctx.rule.attr, "deps", [])
        if NpmPackageInfo in dep
    ])]

_workspace_npm_deps = aspect(
    implementation = _workspace_npm_deps_impl,
    doc = "Reads the member's direct npm deps: TsInfo carries the closure, " +
          "and the direct set, which names the top-level directories of a " +
          "node_modules tree, travels nowhere else.",
)

_MemberJsx = provider(
    doc = "The jsx a member's compiling target declares, TsConfigInfo.jsx.",
    fields = {
        "jsx": "string: \"preserve\" when a .tsx of the member emits .jsx; " +
               "\"\" otherwise.",
    },
)

def _member_jsx_impl(target, ctx):
    tsconfig = getattr(ctx.rule.attr, "tsconfig", None)
    jsx = ""
    if tsconfig and TsConfigInfo in tsconfig:
        jsx = tsconfig[TsConfigInfo].jsx
    return [_MemberJsx(jsx = jsx)]

_member_jsx = aspect(
    implementation = _member_jsx_impl,
    doc = "Reads the declared jsx off the compiling target's tsconfig: it " +
          "names a .tsx's emit and travels in no provider of the target's own.",
)

def _package_root(ctx, member):
    return "/".join([
        p
        for p in [ctx.bin_dir.path, member.label.workspace_root, ctx.attr.member_dir]
        if p
    ])

def _npm_package_info(ctx, member):
    manifest = ctx.actions.declare_file("{}/package.json".format(ctx.label.name))
    jsx = member[_MemberJsx].jsx if _MemberJsx in member else ""
    text = member_manifest_json(json.decode(ctx.attr.manifest_json), jsx)
    ctx.actions.write(output = manifest, content = text + "\n")

    info = member[TsInfo]
    direct_deps = member[_WorkspaceNpmDeps].direct

    # The member's own package.json names source targets; the manifest as
    # built takes its place in the link.
    own = ctx.attr.member_dir + "/package.json"
    data = [f for f in info.data.to_list() if f.short_path != own]

    return NpmPackageInfo(
        package_name = ctx.attr.package_name,
        package_version = _WORKSPACE_VERSION,
        peer_id = "",
        package_dir = None,
        package_root = _package_root(ctx, member),
        all_files = depset(
            [manifest] + data,
            transitive = [info.declarations, info.js, info.js_maps],
        ),
        js_files = info.js,
        direct_deps = direct_deps,
        transitive_deps = info.npm_packages,
        transitive_package_dirs = depset(
            transitive = [dep.transitive_package_dirs for dep in direct_deps],
        ),
    )

def _npm_workspace_package_impl(ctx):
    member = ctx.attr.target

    return [
        DefaultInfo(
            files = member[DefaultInfo].files,
            runfiles = member[DefaultInfo].default_runfiles,
        ),
        _npm_package_info(ctx, member),
        member[TsInfo],
    ]

npm_workspace_package = rule(
    implementation = _npm_workspace_package_impl,
    attrs = {
        "package_name": attr.string(
            mandatory = True,
            doc = "The npm package name the member is imported by.",
        ),
        "member_dir": attr.string(
            mandatory = True,
            doc = "The member's directory from the workspace root: the directory its " +
                  "package.json sits in and its manifest's targets are relative to.",
        ),
        "target": attr.label(
            mandatory = True,
            aspects = [_workspace_npm_deps, _member_jsx],
            providers = [TsInfo],
            doc = "The target that compiles the member, as npm_hub finds it.",
        ),
        "manifest_json": attr.string(
            mandatory = True,
            doc = "The member's package.json as text; the view rewrites " +
                  "every source-file target to the emitted file " +
                  "(npm/private/member_manifest.bzl) and writes the result. " +
                  "Text rather than a label: the module extension reads the " +
                  "file, and an analysis-time write needs its content.",
        ),
    },
    doc = "Generated by npm_hub for each workspace member; not for hand use.",
)
