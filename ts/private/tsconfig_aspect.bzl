"""The workspace-root tsconfig.json an IDE reads, built from the build graph.

Everything the file needs is already in the graph: a ts_compile target's package
is its source root, its `path_aliases` attr is what a
`# gazelle:ts_path_alias` directive turns into, and NpmPackageInfo names the
.d.ts entry point of every npm package reachable from it. So the tsconfig is a
declared output of a rule over an aspect, and `bazel run //:refresh_tsconfig`
only copies that output into the source tree.

An `@types/*` dep is the exception to `paths`: nothing imports the globals it
declares, so, as in the tsconfig ts_compile generates, its entry point is named
in `files` instead.

npm declarations are the one part that lives outside the workspace, and no
workspace-relative path reaches them: a lazily-fetched spoke exists only under
`<output_base>/external/`, which nothing links into the execroot that the
`bazel-<workspace>` symlink points at. So the same `bazel run` that installs the
tsconfig copies those .d.ts files into `npm_dir` and points the paths entries
there -- resolvable in every editor, and keyed by package name rather than by a
canonical repository name that changes with every version bump.
"""

load("@bazel_skylib//rules:diff_test.bzl", "diff_test")
load("//ts/private:providers.bzl", "NpmPackageInfo", "TsDeclarationInfo")

TsconfigSourcesInfo = provider(
    doc = "What a workspace-root tsconfig.json needs from the ts_compile targets under it.",
    fields = {
        "packages": "depset of struct(path, has_index): package of every ts_compile target reached, and whether it has an index file to name as the package entry point.",
        "aliases": "depset of struct(prefix, dir): path_aliases entries, workspace-relative.",
        "npm_paths": "depset of struct(name, version, entry, is_file): npm entry points, relative to the package's own directory.",
        "npm_ambient": "depset of struct(name, version, entry): @types/* entry points to name in the tsconfig `files` array, relative to the package's own directory.",
        "npm_files": "depset of struct(name, version, dest, file): the files an npm entry point needs on disk, and where under the package each one goes.",
        "has_content": "Whether anything above is non-empty here or anywhere below, so that a fragment is written only where there is something to say.",
    },
)

TsconfigFragmentInfo = provider(
    doc = "The per-target tsconfig fragments an editor can merge without a rule ever naming the target.",
    fields = {
        "fragments": """depset of File: one fragment per target the aspect reached.

Each is complete for its own closure -- packages, aliases and npm entry points --
so any one of them is a usable answer on its own, which is what makes a
partially built bazel-out still readable. The `@types/*` entries are not in
there: they are a tsconfig `files` concern, and no module specifier resolves to
them.""",
    },
)

WorkspaceCopyInfo = provider(
    doc = "Files a rule wants written into the source tree beside its default output.",
    fields = {
        "entries": "depset of struct(file, dest): each file and its workspace-relative destination.",
    },
)

def _under(file, dir):
    """`file`'s path relative to `dir`, or None when it is not under `dir`."""
    if not file.path.startswith(dir + "/"):
        return None
    return file.path[len(dir) + 1:]

def _types_root(infos, dts):
    """The @types package `dts` came from: its root directory and its package.json."""
    for info in infos.values():
        if not info.package_name.startswith("@types/"):
            continue
        root = info.package_dir.dirname
        if dts.path.startswith(root + "/"):
            return root, info.package_dir
    return dts.dirname, None

def _npm_entries(rule_attr):
    """The npm paths entries one ts_compile target implies, and the files they need.

    Mirrors what ts_compile puts in the tsconfig it generates for tsgo: the
    package's own `exports["."].types` when it declares one, the paired @types
    directory when its declarations live there, the package directory otherwise.
    """
    infos = {}
    paired = {}

    for dep in getattr(rule_attr, "deps", []):
        if NpmPackageInfo not in dep:
            continue
        info = dep[NpmPackageInfo]
        if info.package_dir:
            infos.setdefault(info.package_name, info)

        # Unavoidable: the entries are provider structs, and a map keyed by
        # package name is what dedupes a package reached along two paths.
        for transitive in info.transitive_deps.to_list():
            if transitive.package_dir:
                infos.setdefault(transitive.package_name, transitive)

        if TsDeclarationInfo not in dep or info.package_name.startswith("@types/"):
            continue
        runtime_dir = info.package_dir.dirname
        outside = [
            dts
            for dts in dep[TsDeclarationInfo].declaration_files.to_list()
            if not dts.path.startswith(runtime_dir + "/")
        ]
        if outside:
            paired.setdefault(info.package_name, outside)

    paths = []
    files = []
    for name, info in infos.items():
        # A package with no declarations has nothing to tell an editor, and the
        # ones that ship none are the platform-specific binaries, whose repo
        # names would otherwise make the file differ per host.
        if name.startswith("@types/") or not info.declaration_files.to_list():
            continue

        if name in paired:
            sources = paired[name]
            root, types_package_json = _types_root(infos, sources[0])
            if types_package_json:
                sources = sources + [types_package_json]
            first = _under(sources[0], root) or ""
            entry = first.rsplit("/", 1)[0] if "/" in first else ""
            is_file = False
        else:
            root = info.package_dir.dirname
            sources = info.declaration_files.to_list() + [info.package_dir]
            entry = _under(info.exports_types_file, root) if info.exports_types_file else None
            is_file = entry != None
            entry = entry or ""

        paths.append(struct(
            name = name,
            version = info.package_version,
            entry = entry,
            is_file = is_file,
        ))
        for src in sources:
            dest = _under(src, root)
            if dest:
                files.append(struct(
                    name = name,
                    version = info.package_version,
                    dest = dest,
                    file = src,
                ))
    return paths, files

def _ambient_entries(rule_attr):
    """The @types/* entry points one ts_compile target declares, and their files.

    Mirrors ts_compile: a direct @types/* dep is how a target asks for the
    globals, and the entry point is named in `files` because `typeRoots` wants a
    directory whose children are type packages, which one repo per package never
    produces. The whole declaration set comes along -- an entry point is a list
    of `/// <reference path=...>` to its siblings, resolved on disk.
    """
    entries = []
    files = []
    for dep in getattr(rule_attr, "deps", []):
        if NpmPackageInfo not in dep:
            continue
        info = dep[NpmPackageInfo]
        if not info.ambient_types_file:
            continue
        root = info.package_dir.dirname
        entry = _under(info.ambient_types_file, root)
        if not entry:
            continue
        entries.append(struct(
            name = info.package_name,
            version = info.package_version,
            entry = entry,
        ))
        for src in info.declaration_files.to_list() + [info.package_dir]:
            dest = _under(src, root)
            if dest:
                files.append(struct(
                    name = info.package_name,
                    version = info.package_version,
                    dest = dest,
                    file = src,
                ))
    return entries, files

def _aliases(rule_attr):
    entries = []
    for prefix, dir in getattr(rule_attr, "path_aliases", {}).items():
        prefix = prefix.rstrip("/")
        dir = dir.rstrip("/")
        if prefix and dir and prefix != dir:
            entries.append(struct(prefix = prefix, dir = dir))
    return entries

def _has_index(target, ctx):
    package = target.label.package
    for src in getattr(ctx.rule.files, "srcs", []):
        for ext in [".ts", ".tsx", ".mts", ".js", ".mjs"]:
            if src.short_path == package + "/index" + ext:
                return True
    return False

FRAGMENT_SUFFIX = ".tsconfig-fragment.json"

_FRAGMENT_FORMAT = "tsconfig-fragment-v1"

def _fragment_package(package):
    return json.encode({"package": package.path, "index": package.has_index})

def _fragment_alias(alias):
    return json.encode({"alias": alias.prefix, "dir": alias.dir})

def _fragment_npm(entry):
    return json.encode({
        "npm": entry.name,
        "version": entry.version,
        "entry": entry.entry,
        "file": entry.is_file,
    })

def _fragment(target, ctx, sources):
    """One JSON object per line, carrying `sources`, for the tsserver hook to merge.

    Deliberately not built from _packages/_npm_view: those materialise their
    depsets, which is affordable once for one ide_tsconfig target and not on
    every target of every build. An Args with map_each defers the same work to
    execution time, so the depsets stay depsets through analysis.
    """
    args = ctx.actions.args()
    args.set_param_file_format("multiline")
    args.add(json.encode({"format": _FRAGMENT_FORMAT, "label": str(target.label)}))
    args.add_all(sources.packages, map_each = _fragment_package, uniquify = True)
    args.add_all(sources.aliases, map_each = _fragment_alias, uniquify = True)
    args.add_all(sources.npm_paths, map_each = _fragment_npm, uniquify = True)

    out = ctx.actions.declare_file(target.label.name + FRAGMENT_SUFFIX)
    ctx.actions.write(out, args)
    return out

def _tsconfig_aspect_impl(target, ctx):
    inherited = [
        dep[TsconfigSourcesInfo]
        for dep in getattr(ctx.rule.attr, "deps", [])
        if TsconfigSourcesInfo in dep
    ]

    packages = []
    aliases = []
    npm_paths = []
    npm_ambient = []
    npm_files = []
    if ctx.rule.kind == "ts_compile":
        if target.label.package:
            packages = [struct(path = target.label.package, has_index = _has_index(target, ctx))]
        aliases = _aliases(ctx.rule.attr)
        npm_paths, npm_files = _npm_entries(ctx.rule.attr)
        npm_ambient, ambient_files = _ambient_entries(ctx.rule.attr)
        npm_files = npm_files + ambient_files

    sources = TsconfigSourcesInfo(
        packages = depset(packages, transitive = [s.packages for s in inherited], order = "postorder"),
        aliases = depset(aliases, transitive = [s.aliases for s in inherited], order = "postorder"),
        npm_paths = depset(npm_paths, transitive = [s.npm_paths for s in inherited], order = "postorder"),
        npm_ambient = depset(npm_ambient, transitive = [s.npm_ambient for s in inherited], order = "postorder"),
        npm_files = depset(npm_files, transitive = [s.npm_files for s in inherited], order = "postorder"),
        has_content = bool(packages or aliases or npm_paths) or any([s.has_content for s in inherited]),
    )

    fragments = [dep[TsconfigFragmentInfo].fragments for dep in getattr(ctx.rule.attr, "deps", []) if TsconfigFragmentInfo in dep]
    own = [_fragment(target, ctx, sources)] if sources.has_content else []
    fragments = depset(own, transitive = fragments, order = "postorder")

    return [
        sources,
        TsconfigFragmentInfo(fragments = fragments),
        OutputGroupInfo(ide_fragments = fragments),
    ]

tsconfig_aspect = aspect(
    implementation = _tsconfig_aspect_impl,
    attr_aspects = ["deps"],
    doc = """Collects the source roots, path aliases and npm entry points an IDE tsconfig needs.

Also writes one `<target>.tsconfig-fragment.json` per target reached, in the
`ide_fragments` output group. That group is how the tsserver hook gets the
targets no rule can name: an aspect propagates along dependency edges that
already exist and creates none, so it needs no visibility where
`ide_tsconfig(deps = [...])` needs a grant. Enable it in .bazelrc, where any
`bazel build` then refreshes the fragments as a side effect:

    build --aspects=@rules_typescript//ts/private:tsconfig_aspect.bzl%tsconfig_aspect
    build --output_groups=+ide_fragments
""",
)

_HEADER = "Generated by 'bazel run //:refresh_tsconfig'. Do not edit manually — re-run to update."

def _collect(sources, field):
    # The one place the graph has to become a file, so the one place a depset is
    # materialised.
    return depset(transitive = [getattr(s, field) for s in sources]).to_list()

def _npm_view(sources):
    """The npm entry points to write, the ambient ones, and the files each needs.

    Two versions of one package name would fight over the same paths key and the
    same directory, so the lowest version wins for the whole package.
    """
    chosen = {}
    for entry in _collect(sources, "npm_paths") + _collect(sources, "npm_ambient"):
        if entry.name not in chosen or entry.version < chosen[entry.name]:
            chosen[entry.name] = entry.version

    entries = sorted(
        [e for e in _collect(sources, "npm_paths") if chosen[e.name] == e.version],
        key = lambda e: e.name,
    )
    ambient = {}
    for entry in _collect(sources, "npm_ambient"):
        if chosen[entry.name] == entry.version:
            ambient[entry.name] = entry.entry
    files = sorted(
        [f for f in _collect(sources, "npm_files") if chosen.get(f.name) == f.version],
        key = lambda f: f.name + "/" + f.dest,
    )
    return entries, sorted(ambient.items()), files

def _packages(sources):
    """Every package the aspect reached, and whether any target in it has an index file."""
    indexed = {}
    for package in _collect(sources, "packages"):
        indexed[package.path] = indexed.get(package.path, False) or package.has_index
    return indexed

def _installed_entry(npm_dir, entry):
    base = "{}/{}".format(npm_dir, entry.name)
    return base + "/" + entry.entry if entry.entry else base

def _ide_tsconfig_impl(ctx):
    sources = [dep[TsconfigSourcesInfo] for dep in ctx.attr.deps]
    bin_dir = "./{}bin".format(ctx.attr.symlink_prefix)

    packages = _packages(sources)
    aliases = sorted([(a.prefix, a.dir) for a in _collect(sources, "aliases")])
    npm_entries, npm_ambient, npm_files = _npm_view(sources)

    paths = {}
    copies = []
    ambient = []

    for prefix, dir in aliases:
        paths[prefix] = ["./{}/index".format(dir)]
        paths[prefix + "/*"] = ["./{}/*".format(dir), "{}/{}/*".format(bin_dir, dir)]

    if ctx.attr.npm_dir:
        for entry in npm_entries:
            if entry.name in paths:
                continue
            installed = "./" + _installed_entry(ctx.attr.npm_dir, entry)
            paths[entry.name] = [installed]
            paths[entry.name + "/*"] = [
                (installed.rsplit("/", 1)[0] if entry.is_file else installed) + "/*",
            ]

        for npm_file in npm_files:
            copies.append(struct(
                file = npm_file.file,
                dest = "{}/{}/{}".format(ctx.attr.npm_dir, npm_file.name, npm_file.dest),
            ))

        ambient = [
            "./{}/{}/{}".format(ctx.attr.npm_dir, name, entry)
            for name, entry in npm_ambient
        ]

        # Nothing under npm_dir is authored, and a consumer's `git status` should
        # not have to learn that.
        ignore = ctx.actions.declare_file(ctx.label.name + ".gitignore")
        ctx.actions.write(ignore, "*\n")
        copies.append(struct(file = ignore, dest = ctx.attr.npm_dir + "/.gitignore"))

    for package in sorted(packages):
        key = ("@/" + package.removeprefix("src/")) if package.startswith("src/") else package
        if key in paths:
            continue

        # Only a package that has one can name an entry point; the rest are
        # reachable through the wildcard alone.
        if packages[package]:
            paths[key] = ["./{}/index".format(package)]
        paths[key + "/*"] = ["./{}/*".format(package), "{}/{}/*".format(bin_dir, package)]

    config = {
        "_comment": _HEADER,
        "compilerOptions": {
            "strict": True,
            "target": "ES2022",
            "module": "Preserve",
            "moduleResolution": "Bundler",
            "jsx": "react-jsx",
            "declaration": True,
            "sourceMap": True,
            "skipLibCheck": True,
            "esModuleInterop": True,
            "allowArbitraryExtensions": True,
            "rootDirs": [".", bin_dir],
            "paths": paths,
            "noEmit": True,
        },
        "exclude": [
            "**/bazel-*",
            "**/node_modules",
            "**/dist",
            "**/build",
            "**/.next",
            "**/.nuxt",
            ".bazel",
        ],
    }

    if ambient:
        # An @types/* package declares globals nothing imports, so it has to be
        # named directly. Spelling `include` out is the price: a `files` array
        # switches off the implicit one, and TypeScript's implicit one is what
        # puts the workspace's own sources in the program.
        config["files"] = ambient
        config["include"] = ["**/*"]

    out = ctx.actions.declare_file(ctx.label.name + ".json")
    ctx.actions.write(out, json.indent(json.encode(config), indent = "  ") + "\n")
    return [
        DefaultInfo(files = depset([out])),
        WorkspaceCopyInfo(entries = depset(copies)),
    ]

_IDE_ATTRS = {
    "deps": attr.label_list(
        aspects = [tsconfig_aspect],
        doc = """The ts_compile targets the IDE should see.

The aspect walks `deps` from here, so a target whose sources another listed
target already depends on does not need its own entry.""",
    ),
    "npm_dir": attr.string(
        default = ".bazel/npm",
        doc = """Workspace-relative directory the npm declarations are installed in.

They live in an external repository, so this copy is the only path to them a
tsconfig can name. The empty string omits the npm entries and their files.""",
    ),
}

ide_tsconfig = rule(
    implementation = _ide_tsconfig_impl,
    attrs = dict(
        _IDE_ATTRS,
        symlink_prefix = attr.string(
            default = "bazel-",
            doc = "Value of --symlink_prefix, which names the bazel-bin symlink the IDE reads .d.ts through.",
        ),
    ),
    doc = """Writes a workspace-root tsconfig.json for IDE consumption.

The file is a declared output; it is Bazel's copy, not the workspace's. Pair it
with refresh_workspace_files to put it in the source tree along with the npm
declarations its paths entries name, and with diff_test to fail when the
checked-in copy has gone stale.""",
)

def _ide_hook_data_impl(ctx):
    sources = [dep[TsconfigSourcesInfo] for dep in ctx.attr.deps]
    npm_entries, _, _ = _npm_view(sources)

    data = {
        "_comment": _HEADER,
        "npmDir": ctx.attr.npm_dir,
        "npmPackages": [
            {"name": e.name, "entry": e.entry, "isFile": e.is_file}
            for e in (npm_entries if ctx.attr.npm_dir else [])
        ],
        "packages": sorted(_packages(sources)),
        "aliases": [
            {"prefix": prefix, "dir": dir}
            for prefix, dir in sorted([(a.prefix, a.dir) for a in _collect(sources, "aliases")])
        ],
    }

    out = ctx.actions.declare_file(ctx.label.name + ".json")
    ctx.actions.write(out, json.indent(json.encode(data), indent = "  ") + "\n")
    return [DefaultInfo(files = depset([out]))]

ide_hook_data = rule(
    implementation = _ide_hook_data_impl,
    attrs = _IDE_ATTRS,
    doc = """Writes what the tsserver hook needs from the build graph.

The hook runs inside a long-lived editor process, so it reads this file rather
than asking Bazel: the package list, the npm entry points and the path aliases
are all analysis-time facts, and querying for them would fight the server lock.""",
)

def _rlocation(ctx, file):
    if file.short_path.startswith("../"):
        return file.short_path[3:]
    return ctx.workspace_name + "/" + file.short_path

def _refresh_workspace_files_impl(ctx):
    entries = []
    inputs = []
    for src, dest in ctx.attr.files.items():
        files = src[DefaultInfo].files.to_list()
        if len(files) != 1:
            fail("refresh_workspace_files: {} produces {} files, want exactly one.".format(src.label, len(files)))
        inputs.append(files[0])
        entries.append({"rlocation": _rlocation(ctx, files[0]), "dest": dest})

        if WorkspaceCopyInfo not in src:
            continue
        for copy in src[WorkspaceCopyInfo].entries.to_list():
            inputs.append(copy.file)
            entries.append({"rlocation": _rlocation(ctx, copy.file), "dest": copy.dest})

    manifest = ctx.actions.declare_file(ctx.label.name + ".manifest.json")
    ctx.actions.write(manifest, json.encode(entries))

    launcher = ctx.actions.declare_file(ctx.label.name)
    ctx.actions.symlink(output = launcher, target_file = ctx.executable._copier, is_executable = True)

    return [
        DefaultInfo(
            executable = launcher,
            runfiles = ctx.runfiles(files = inputs + [manifest]).merge(
                ctx.attr._copier[DefaultInfo].default_runfiles,
            ),
        ),
        RunEnvironmentInfo(environment = {"COPY_TO_WORKSPACE_MANIFEST": _rlocation(ctx, manifest)}),
    ]

refresh_workspace_files = rule(
    implementation = _refresh_workspace_files_impl,
    executable = True,
    attrs = {
        "files": attr.label_keyed_string_dict(
            doc = """Maps each single-file target to its destination, relative to the workspace root.

A target that also returns WorkspaceCopyInfo contributes the files named there,
each to the destination that provider carries.""",
            allow_empty = False,
            allow_files = True,
        ),
        "_copier": attr.label(
            default = "//tools/copy_to_workspace",
            executable = True,
            cfg = "exec",
        ),
    },
    doc = """Copies build outputs into the source tree under `bazel run`.

The copy is all it does: what to write is decided at analysis time, by the rules
that declared the outputs.""",
)

def ts_refresh_tsconfig(
        name = "refresh_tsconfig",
        deps = [],
        npm_dir = ".bazel/npm",
        tsconfig = "tsconfig.json",
        test = False):
    """Declares the IDE tsconfig, the run target that installs it, and its staleness test.

    Args:
        name:     Name of the `bazel run` target. The generated files are
                  `<name>.generated` and `<name>.hook_data`, the diff test
                  `<name>_test`.
        deps:     ts_compile targets the IDE should see. The aspect follows
                  `deps` from each one.
        npm_dir:  Workspace-relative directory the npm declarations the paths
                  entries point at are installed in. Empty omits them.
        tsconfig: Where in the workspace the file is written.
        test:     Add a diff_test that fails when `tsconfig` is stale. Turn it
                  on once `tsconfig` is checked in.
    """
    ide_tsconfig(
        name = name + ".generated",
        deps = deps,
        npm_dir = npm_dir,
    )
    ide_hook_data(
        name = name + ".hook_data",
        deps = deps,
        npm_dir = npm_dir,
    )
    refresh_workspace_files(
        name = name,
        files = {
            ":" + name + ".generated": tsconfig,
            ":" + name + ".hook_data": ".bazel/tsserver-hook-data.json",
            "@rules_typescript//tools:tsserver-hook.js": ".bazel/tsserver-hook.js",
            "@rules_typescript//tools:tsserver-hook-worker.js": ".bazel/tsserver-hook-worker.js",
        },
        visibility = ["//visibility:public"],
    )
    if test:
        diff_test(
            name = name + "_test",
            size = "small",
            failure_message = "{} is stale: run `bazel run //:{}`.".format(tsconfig, name),
            file1 = ":" + name + ".generated",
            file2 = tsconfig,
        )
