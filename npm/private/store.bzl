"""The virtual store: one tree of real files per lockfile snapshot, its
dependency links beside it. docs/rules/node-modules.md § The Store."""

load("//npm/private:member_manifest.bzl", "member_manifest_json")
load("//ts/private:providers.bzl", "TsConfigInfo", "TsInfo")

NpmStoreInfo = provider(
    doc = "One snapshot of the virtual store: its tree and the links beside " +
          "it.",
    fields = {
        "key": "string: the store directory's name, " +
               "`<name with / as +>@<version>[_<peer id>]`.",
        "tree": "File: the tree artifact of the snapshot's files, " +
                "`node_modules/.pnpm/<key>/node_modules/<name>`.",
        "links": "dict of string -> File: the dependency links beside the " +
                 "tree, by the name the snapshot imports each dependency by.",
        "transitive": "depset of File: the tree, its links and every " +
                      "dependency store's transitive set.",
        "manifest": "File or None: a member's package.json as built, the " +
                    "copy its tree holds; None for a published snapshot.",
    },
)

MEMBER_VERSION = "0.0.0"

STORE_DIR = "node_modules/.pnpm"

HOIST_TARGET = STORE_DIR + "/node_modules"

def store_key(name, version, peer_id):
    """The store directory of one resolution: name, version and peer set."""
    key = "{}@{}".format(name.replace("/", "+"), version)
    return key if not peer_id else "{}_{}".format(key, peer_id)

def store_target(key, name):
    """The name of a store target, which is its tree's path in the package."""
    return "{}/{}/node_modules/{}".format(STORE_DIR, key, name)

def _store_parts(name):
    parts = name.split("/")
    shaped = (
        len(parts) >= 5 and parts[0] == "node_modules" and
        parts[1] == ".pnpm" and parts[3] == "node_modules"
    )
    if not shaped:
        fail(
            "npm_store: a store target is named after its tree, " +
            "node_modules/.pnpm/<key>/node_modules/<package>; got '{}'".format(
                name,
            ),
        )
    return struct(
        key = parts[2],
        package = "/".join(parts[4:]),
        links_dir = "/".join(parts[:4]),
    )

def _relative(from_dir, to):
    a = from_dir.split("/")
    b = to.split("/")
    shared = 0
    for _ in range(min(len(a), len(b))):
        if a[shared] != b[shared]:
            break
        shared += 1
    return "/".join([".."] * (len(a) - shared) + b[shared:])

def store_link(ctx, dir, name, store):
    """The declared symlink `<dir>/<name>`, its target the relative path to
    `store`'s tree."""
    link = ctx.actions.declare_symlink("{}/{}".format(dir, name))
    ctx.actions.symlink(
        output = link,
        target_path = _relative(link.dirname, store.tree.path),
    )
    return link

def _link(ctx, dir, name, dep, links):
    if name in links:
        fail("{}: '{}' linked twice, to {} and {}".format(
            ctx.label,
            name,
            links[name].owner,
            dep.label,
        ))
    links[name] = store_link(ctx, dir, name, dep[NpmStoreInfo])

def _dep_links(ctx, parts):
    links = {}
    for dep, names in ctx.attr.deps.items():
        for name in names.split(" "):
            if name == parts.package:
                fail(
                    "{}: depends on {} under its own name '{}', ".format(
                        ctx.label,
                        dep.label,
                        name,
                    ) + "the path its tree holds; pnpm writes no such edge.",
                )
            _link(ctx, parts.links_dir, name, dep, links)
    return links

def _stage(ctx, tree, files, dest):
    args = ctx.actions.args()
    args.use_param_file("@%s", use_always = True)
    args.set_param_file_format("multiline")
    args.add("-out=" + tree.path)
    args.add_all(
        files,
        map_each = lambda f: [f.path, dest(f)],
        allow_closure = True,
    )
    ctx.actions.run(
        executable = ctx.executable._tsaction,
        arguments = ["stage", args],
        inputs = files,
        outputs = [tree],
        mnemonic = "NpmStore",
        progress_message = "Staging %{label}",
    )

def _store_info(ctx, parts, tree, links, manifest = None):
    return NpmStoreInfo(
        key = parts.key,
        tree = tree,
        links = links,
        transitive = depset(
            [tree] + links.values(),
            transitive = [d[NpmStoreInfo].transitive for d in ctx.attr.deps],
        ),
        manifest = manifest,
    )

def _npm_store_impl(ctx):
    parts = _store_parts(ctx.label.name)
    tree = ctx.actions.declare_directory(ctx.label.name)
    root = ctx.file.package_dir.dirname + "/"

    def dest(f):
        if not f.path.startswith(root):
            fail("{}: {} lies outside the package at {}".format(
                ctx.label,
                f.path,
                root,
            ))
        return f.path[len(root):]

    _stage(ctx, tree, ctx.files.files, dest)
    info = _store_info(ctx, parts, tree, _dep_links(ctx, parts))
    return [DefaultInfo(files = info.transitive), info]

_TSACTION = attr.label(
    default = Label("//ts/tools/tsaction"),
    executable = True,
    cfg = "exec",
)

_DEPS = attr.label_keyed_string_dict(
    providers = [NpmStoreInfo],
    doc = "Store target -> the space-separated names this snapshot imports " +
          "it by: one link beside the tree per name, an npm alias under the " +
          "alias name.",
)

npm_store = rule(
    implementation = _npm_store_impl,
    attrs = {
        "files": attr.label(
            mandatory = True,
            doc = "The snapshot repository's `:files` filegroup.",
        ),
        "package_dir": attr.label(
            mandatory = True,
            allow_single_file = True,
            doc = "The extracted package.json; its directory is the package " +
                  "root.",
        ),
        "deps": _DEPS,
        "_tsaction": _TSACTION,
    },
    doc = """One store tree, named after its path: the snapshot's files copied
by `tsaction stage` into `node_modules/.pnpm/<key>/node_modules/<name>`, and
one declared symlink beside it per dependency. `npm_virtual_store` declares
every one of a lockfile's; not for hand use.""",
)

_MemberJsx = provider(
    doc = "The jsx a member's compiling target declares, TsConfigInfo.jsx.",
    fields = {
        "jsx": "string: \"preserve\" when a .tsx emits .jsx; \"\" otherwise.",
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

def _member_roots(ctx, member):
    parts = [member.label.workspace_root, ctx.attr.member_dir]
    src = "/".join([p for p in parts if p])
    return [ctx.bin_dir.path + "/" + src + "/", src + "/"]

def _npm_store_member_impl(ctx):
    parts = _store_parts(ctx.label.name)
    member = ctx.attr.member
    tree = ctx.actions.declare_directory(ctx.label.name)
    key_dir = parts.links_dir.rsplit("/", 1)[0]
    manifest = ctx.actions.declare_file(key_dir + "/package.json")
    jsx = member[_MemberJsx].jsx if _MemberJsx in member else ""
    text = member_manifest_json(json.decode(ctx.attr.manifest_json), jsx)
    ctx.actions.write(output = manifest, content = text + "\n")

    info = member[TsInfo]
    own = ctx.attr.member_dir + "/package.json"
    files = [f for f in info.data.to_list() if f.short_path != own]
    for emitted in (info.declarations, info.js, info.js_maps):
        files.extend(emitted.to_list())
    roots = _member_roots(ctx, member)

    def dest(f):
        if f == manifest:
            return "package.json"
        for root in roots:
            if f.path.startswith(root):
                return f.path[len(root):]
        return f.basename

    _stage(ctx, tree, [manifest] + files, dest)
    store = _store_info(ctx, parts, tree, _dep_links(ctx, parts), manifest)
    return [DefaultInfo(files = store.transitive), store]

npm_store_member = rule(
    implementation = _npm_store_member_impl,
    attrs = {
        "member": attr.label(
            mandatory = True,
            aspects = [_member_jsx],
            providers = [TsInfo],
            doc = "The target that compiles the member, as npm_hub finds it.",
        ),
        "member_dir": attr.string(
            mandatory = True,
            doc = "The member's directory from the workspace root.",
        ),
        "manifest_json": attr.string(
            mandatory = True,
            doc = "The member's package.json as text; written as built " +
                  "(npm/private/member_manifest.bzl) beside the tree and " +
                  "copied into it.",
        ),
        "deps": _DEPS,
        "_tsaction": _TSACTION,
    },
    doc = """A workspace member's store tree, `node_modules/.pnpm/<name with /
as +>@0.0.0/node_modules/<name>`: its package.json as built, its `.js`,
`.js.map`, `.d.ts` and data at their package-relative paths, the source
package.json excepted; one declared symlink beside it per dependency the
member's importer declares. Declared by `npm_virtual_store`.""",
)

def _hoist_links(ctx, dir, entries, links):
    for dep, names in entries.items():
        for name in names.split(" "):
            _link(ctx, dir, name, dep, links)
    return links

def _npm_store_hoist_impl(ctx):
    links = _hoist_links(ctx, ctx.label.name, ctx.attr.private, {})
    links = _hoist_links(ctx, "node_modules", ctx.attr.public, links)
    stores = ctx.attr.private.keys() + ctx.attr.public.keys()
    return [DefaultInfo(files = depset(
        links.values(),
        transitive = [dep[NpmStoreInfo].transitive for dep in stores],
    ))]

_HOISTED = attr.label_keyed_string_dict(
    providers = [NpmStoreInfo],
    doc = "Store target -> the space-separated names linked at it.",
)

npm_store_hoist = rule(
    implementation = _npm_store_hoist_impl,
    attrs = {
        "private": _HOISTED,
        "public": _HOISTED,
    },
    doc = """The hidden hoist of one lockfile: a declared symlink into a store
tree per hoisted name, `private` ones under this target's name
(`node_modules/.pnpm/node_modules/<name>`), `public` ones at the root
importer's `node_modules/<name>`. Declared by `npm_virtual_store`.""",
)

def _join(names):
    return " ".join(sorted(names))

def _platform_dicts(common, by_platform, platforms):
    if not by_platform:
        return {label: _join(names) for label, names in common.items()}
    branches = {}
    for platform in platforms:
        merged = {label: list(names) for label, names in common.items()}
        for label, names in by_platform.get(platform, {}).items():
            merged.setdefault(label, []).extend(names)
        branches[Label("//platforms:is_" + platform)] = {
            label: _join(names)
            for label, names in merged.items()
        }
    return select(branches, no_match_error = (
        "the npm store here is platform-specific and this target platform " +
        "is none of " + ", ".join(platforms)
    ))

def _add(common, by_platform, platforms, label, name):
    if platforms == None:
        common.setdefault(label, []).append(name)
        return
    for platform in platforms:
        by_platform.setdefault(platform, {}).setdefault(label, []).append(name)

def hoisted_on(graph, platform):
    """{alias: (kind, target)} the graph hoists on `platform`; a target is
    ("snapshot", index) or ("member", index) into the graph's lists."""
    out = {}
    for entry in graph["hoist"]:
        ref = entry.get("target", entry.get("targets", {}).get(platform))
        if ref == None:
            continue
        which, index = ref.items()[0]
        out[entry["alias"]] = (entry["kind"], (which, index))
    return out

def virtual_store(name, graph, members, files, package_dir):
    """Declares a lockfile's store in the calling package: every npm_store,
    npm_store_member and the npm_store_hoist, `manual` and public.

    Args:
        name: The store directory the call is named after, `node_modules/.pnpm`.
        graph: The hub's store graph, decoded.
        members: {member path: struct(target = Label, manifest = str)} for
            every member whose BUILD file declares its target.
        files: function(repo) -> Label of the snapshot repository's `:files`,
            evaluated in the hub, which sees the repository by that name.
        package_dir: function(repo, name) -> Label of its package.json.
    """
    if name != STORE_DIR:
        fail(
            "npm_virtual_store: the store is pnpm's {}, ".format(STORE_DIR) +
            "which names the call; got '{}'".format(name),
        )
    here = (native.repo_name(), native.package_name())
    if here != (graph["repo"], graph["package"]):
        fail(
            "npm_virtual_store called from @@{}//{}: the store of {} ".format(
                here[0],
                here[1],
                graph["lockfile"],
            ) + "is declared in its own package, @@{}//{}".format(
                graph["repo"],
                graph["package"],
            ),
        )
    platforms = graph["platforms"]
    snapshots = graph["snapshots"]
    member_targets = {}
    for index, member in enumerate(graph["members"]):
        if member["path"] in members:
            member_targets[index] = store_target(member["key"], member["name"])

    def snapshot_target(index):
        snap = snapshots[index]
        return store_target(snap["key"], snap["name"])

    def edges(deps, links):
        common = {}
        by_platform = {}
        for index, alias in deps:
            dep = snapshots[index]
            label = ":" + snapshot_target(index)
            name = alias or dep["name"]
            _add(common, by_platform, dep.get("platforms"), label, name)
        for index, alias in links:
            if index in member_targets:
                label = ":" + member_targets[index]
                _add(common, by_platform, None, label, alias)
        return _platform_dicts(common, by_platform, platforms)

    for index, snap in enumerate(snapshots):
        npm_store(
            name = snapshot_target(index),
            files = files(snap["repo"]),
            package_dir = package_dir(snap["repo"], snap["name"]),
            deps = edges(snap["deps"], []),
            tags = ["manual"],
            visibility = ["//visibility:public"],
        )

    for index, member in enumerate(graph["members"]):
        if index not in member_targets:
            continue
        resolved = members[member["path"]]
        npm_store_member(
            name = member_targets[index],
            member = resolved.target,
            member_dir = member["path"],
            manifest_json = resolved.manifest,
            deps = edges(member["deps"], member["links"]),
            tags = ["manual"],
            visibility = ["//visibility:public"],
        )

    seen = {}
    for platform in platforms:
        for alias, (kind, ref) in hoisted_on(graph, platform).items():
            which, index = ref
            if which == "member" and index not in member_targets:
                continue
            if which == "member":
                target = member_targets[index]
            else:
                target = snapshot_target(index)
            seen.setdefault((kind, ":" + target, alias), []).append(platform)
    hoist = {"private": ({}, {}), "public": ({}, {})}
    for (kind, label, alias), on in seen.items():
        common, by_platform = hoist[kind]
        everywhere = len(on) == len(platforms)
        _add(common, by_platform, None if everywhere else on, label, alias)
    dicts = {
        kind: _platform_dicts(common, by_platform, platforms)
        for kind, (common, by_platform) in hoist.items()
    }
    npm_store_hoist(
        name = HOIST_TARGET,
        private = dicts["private"],
        public = dicts["public"],
        tags = ["manual"],
        visibility = ["//visibility:public"],
    )
