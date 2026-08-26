"""The workspace-root tsconfig.json an IDE reads, built from the build graph.

Everything the file needs is already in the graph: a ts_compile target's package
is its source root, its `module_name` is the bare specifier that resolves to
that root, its `path_aliases` attr is what a `# gazelle:ts_path_alias` directive
turns into, and NpmPackageInfo names the .d.ts entry point of every npm package
reachable from it. So the tsconfig is a declared output of a rule over an aspect,
and `bazel run //:refresh_tsconfig` only copies that output into the source
tree.

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
load("//ts/private:providers.bzl", "NpmPackageInfo", "TsConfigInfo")

TsconfigSourcesInfo = provider(
    doc = "What a workspace-root tsconfig.json needs from the ts_compile targets under it.",
    fields = {
        "packages": "depset of struct(path, has_index, module_name): package of every ts_compile target reached, whether it has an index file to name as the package entry point, and the bare specifier the target declared with `module_name` (empty when it declared none).",
        "aliases": "depset of struct(prefix, dir): path_aliases entries, workspace-relative.",
        "npm_paths": "depset of struct(name, version, entry, is_file): npm entry points, relative to the package's own directory.",
        "npm_ambient": "depset of struct(name, version, entry): @types/* entry points to name in the tsconfig `files` array, relative to the package's own directory.",
        "npm_files": "depset of struct(name, version, dest, file): the files an npm entry point needs on disk, and where under the package each one goes.",
        "option_groups": "depset of struct(package, label, options_json, extends, include): the compilerOptions delta an editor needs for one target, which no single root block can carry -- a target that turns `strict` off, or names a `lib` its target does not imply, is checked correctly by the build and wrongly by the editor unless the editor gets its own program for those files.",
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

    # Over every package that gets a `paths` entry, not just the direct deps:
    # an untyped package reached transitively (vitest -> @vitest/expect -> chai)
    # is named in `paths` all the same, and pointing that entry at the runtime
    # package resolves to no declarations at all.
    paired = {}
    for name, info in infos.items():
        if name.startswith("@types/"):
            continue
        runtime_dir = info.package_dir.dirname
        outside = [
            dts
            for dts in info.declaration_files.to_list()
            if not dts.path.startswith(runtime_dir + "/")
        ]
        if outside:
            paired[name] = outside

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

def _npm_type_packages(rule_attr):
    """The compilerOptions.types entries an npm dep actually provides.

    Only these are dropped from an editor program's options: they are the ones
    that resolve through node_modules, which is what does not exist here.
    """
    requested = _requested_type_packages(rule_attr)
    if not requested:
        return []
    provided = []
    for dep in getattr(rule_attr, "deps", []):
        if NpmPackageInfo not in dep:
            continue
        info = dep[NpmPackageInfo]
        for entry in requested:
            if _requested_subpath_file(info, [entry]):
                provided.append(entry)
    return provided

def _requested_type_packages(rule_attr):
    """The compilerOptions.types entries that name a package rather than a path."""
    raw = getattr(rule_attr, "compiler_options_json", "")
    if not raw:
        return []
    decoded = json.decode(raw)
    if type(decoded) != "dict":
        return []
    return [
        t
        for t in decoded.get("types", [])
        if type(t) == "string" and not t.startswith(".")
    ]

def _requested_subpath_file(info, requested):
    """The declaration a `types` entry designates on this package, or None.

    The same resolution ts_compile does: a bare name means the root export, and
    `pkg/sub` means the `exports` subpath -- which is where a package puts an
    ambient module it must not force on every importer.
    """
    for entry in requested:
        if entry == info.package_name:
            if info.exports_types_file:
                return info.exports_types_file
        elif entry.startswith(info.package_name + "/"):
            designated = info.subpath_types.get("." + entry[len(info.package_name):])
            if designated:
                return designated
    return None

def _ambient_entries(rule_attr):
    """The @types/* entry points one ts_compile target declares, and their files.

    Mirrors ts_compile: a direct @types/* dep is how a target asks for the
    globals, and the entry point is named in `files` because `typeRoots` wants a
    directory whose children are type packages, which one repo per package never
    produces. The whole declaration set comes along -- an entry point is a list
    of `/// <reference path=...>` to its siblings, resolved on disk.
    """
    requested = _requested_type_packages(rule_attr)
    entries = []
    files = []
    for dep in getattr(rule_attr, "deps", []):
        if NpmPackageInfo not in dep:
            continue
        info = dep[NpmPackageInfo]
        ambient = info.ambient_types_file or _requested_subpath_file(info, requested)
        if not ambient:
            continue
        root = info.package_dir.dirname
        entry = _under(ambient, root)
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
    record = {"package": package.path, "index": package.has_index}
    if package.module_name:
        record["module"] = package.module_name
    return json.encode(record)

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

# Options a target sets that the root block cannot also be set to. Bazel-owned
# keys are dropped: they describe the sandbox's shape, not the semantics an
# editor has to agree with, and every one of them is rejected on the attr anyway.
_EDITOR_IRRELEVANT_OPTIONS = [
    "outDir",
    "rootDir",
    "rootDirs",
    "declarationDir",
    "declaration",
    "declarationMap",
    "emitDeclarationOnly",
    "sourceMap",
    "noEmit",
    "noEmitOnError",
    "isolatedDeclarations",
    "composite",
    "incremental",
    "tsBuildInfoFile",
    "baseUrl",
    "paths",
]

# The root block's own values, so a target matching them needs no nested config.
# ts_compile defaults `target` and `jsx_mode` to these.
_ROOT_TARGET = "ES2022"
_ROOT_JSX = "react-jsx"

# TypeScript compares these values case-insensitively, and treats `lib` as a set.
# Two targets spelling one value differently are not a conflict, so the delta is
# canonicalised before anything compares or emits it.
_CASE_INSENSITIVE_OPTIONS = [
    "target",
    "module",
    "moduleResolution",
    "jsx",
    "moduleDetection",
    "newLine",
]

def _canonical_options(options):
    canonical = dict(options)
    for key in _CASE_INSENSITIVE_OPTIONS:
        value = canonical.get(key)
        if type(value) == "string":
            canonical[key] = value.lower()
    lib = canonical.get("lib")
    if type(lib) == "list":
        canonical["lib"] = sorted([entry.lower() if type(entry) == "string" else entry for entry in lib])
    return canonical

def _option_group(target, ctx):
    """The compilerOptions delta one ts_compile target needs, or []."""

    # A `manual` target is one nothing builds -- //tests/compiler_options/analysis
    # has one whose option value is deliberately nonsense to tsgo, read by an
    # analysis test rather than run. There is no editor program to get right.
    if "manual" in getattr(ctx.rule.attr, "tags", []):
        return []

    raw = getattr(ctx.rule.attr, "compiler_options_json", "")
    options = {}
    if raw:
        decoded = json.decode(raw)
        if type(decoded) == "dict":
            options = {
                k: v
                for k, v in decoded.items()
                if k not in _EDITOR_IRRELEVANT_OPTIONS
            }

            # A `types` entry that names an npm package cannot resolve from a
            # nested config either -- there is still no node_modules to walk --
            # so those reach the editor through `files`, installed by
            # _ambient_entries. Every other entry stays: a relative path is a
            # workspace file, and a bare name under a declared `typeRoots` is a
            # local type package, which resolves from here exactly as it does for
            # the build.
            if "types" in options:
                npm_named = _npm_type_packages(ctx.rule.attr)
                kept = [t for t in options["types"] if t not in npm_named]
                if kept:
                    options["types"] = kept
                else:
                    options.pop("types")

    # A .js source is only in the program at all with allowJs, which ts_compile
    # infers from srcs rather than making the author say it.
    sources = [f for src in ctx.rule.files.srcs for f in [src]]
    if any([f.extension in ("js", "jsx", "mjs", "cjs") for f in sources]):
        options["allowJs"] = True

    # The baseline this target checks against. An editor program for these files
    # has to start from the same place, or it disagrees for a second reason.
    extends = ""
    tsconfig = getattr(ctx.rule.attr, "tsconfig", None)
    if tsconfig and TsConfigInfo in tsconfig:
        extends = tsconfig[TsConfigInfo].tsconfig.short_path
    elif getattr(ctx.rule.file, "tsconfig", None):
        extends = ctx.rule.file.tsconfig.short_path

    # target and jsx_mode are rule attrs the build injects, so they never appear
    # in compiler_options_json. Left out here the editor checks an es2017 or
    # preserve-JSX target against the root's ES2022/react-jsx and disagrees with
    # the build -- the divergence a nested config exists to end.
    if ctx.rule.attr.target and ctx.rule.attr.target.lower() != _ROOT_TARGET.lower():
        options["target"] = ctx.rule.attr.target
    if ctx.rule.attr.jsx_mode and ctx.rule.attr.jsx_mode.lower() != _ROOT_JSX.lower():
        options["jsx"] = ctx.rule.attr.jsx_mode

    options = _canonical_options(options)

    if not options and not extends:
        return []

    package = target.label.package
    include = [
        f.short_path[len(package) + 1:] if package else f.short_path
        for f in sources
        if not f.short_path.endswith(".d.ts") and f.short_path.startswith(package)
    ]
    if not include:
        return []

    # JSON and a tuple, not a dict and a list: a depset element has to be
    # immutable, and these travel to _ide_tsconfig_impl through one.
    return [struct(
        package = package,
        label = str(target.label),
        options_json = json.encode(options),
        extends = extends,
        include = tuple(sorted(include)),
    )]

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
    option_groups = []
    if ctx.rule.kind == "ts_compile":
        if target.label.package:
            packages = [struct(
                path = target.label.package,
                has_index = _has_index(target, ctx),
                module_name = getattr(ctx.rule.attr, "module_name", ""),
            )]
        aliases = _aliases(ctx.rule.attr)
        npm_paths, npm_files = _npm_entries(ctx.rule.attr)
        npm_ambient, ambient_files = _ambient_entries(ctx.rule.attr)
        npm_files = npm_files + ambient_files
        option_groups = _option_group(target, ctx)

    sources = TsconfigSourcesInfo(
        packages = depset(packages, transitive = [s.packages for s in inherited], order = "postorder"),
        aliases = depset(aliases, transitive = [s.aliases for s in inherited], order = "postorder"),
        npm_paths = depset(npm_paths, transitive = [s.npm_paths for s in inherited], order = "postorder"),
        npm_ambient = depset(npm_ambient, transitive = [s.npm_ambient for s in inherited], order = "postorder"),
        npm_files = depset(npm_files, transitive = [s.npm_files for s in inherited], order = "postorder"),
        option_groups = depset(option_groups, transitive = [s.option_groups for s in inherited], order = "postorder"),
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

def _modules(sources):
    """The package each `module_name` a reached target declared resolves to.

    A module_name is a bare specifier, so it needs its own paths key: the
    package-path key the target already has is not what the import says. Two
    targets declaring one name would fight over that key, so the lowest package
    path wins for the whole name.
    """
    modules = {}
    for package in _collect(sources, "packages"):
        if not package.module_name:
            continue
        chosen = modules.get(package.module_name)
        if chosen == None or package.path < chosen:
            modules[package.module_name] = package.path
    return modules

def _installed_entry(npm_dir, entry):
    base = "{}/{}".format(npm_dir, entry.name)
    return base + "/" + entry.entry if entry.entry else base

def _nested_configs(sources, root_options):
    """One editor program per package whose options the root block cannot carry.

    tsserver picks the nearest tsconfig.json walking up from a file, so a package
    whose targets disagree with the root block needs its own file there. Grouping
    is by package because that is the only granularity tsserver has: two targets
    in one directory setting the same key to different values have no
    representation at all, and that is an error rather than a silent pick.

    A key whose value already equals the root's is dropped, so a target that only
    restates the defaults produces no file.
    """

    # Both sides of the equality have to be canonical, or a root value spelled
    # "Preserve" stops matching a group's "preserve" and every package gets a
    # file it does not need.
    root_options = _canonical_options(root_options)

    groups = {}
    for entry in sources.option_groups.to_list():
        options = json.decode(entry.options_json)
        delta = {
            key: value
            for key, value in options.items()
            if key not in root_options or root_options[key] != value
        }
        group = groups.setdefault(entry.package, struct(
            options = {},
            owners = {},
            extends = {},
            include = {},
        ))
        for key, value in delta.items():
            if key in group.options and group.options[key] != value:
                fail(
                    "ts_refresh_tsconfig: {} and {} are in the same package and set\n".format(
                        group.owners[key],
                        entry.label,
                    ) +
                    "  compilerOptions.{} to {} and {}.\n".format(
                        key,
                        json.encode(group.options[key]),
                        json.encode(value),
                    ) +
                    "An editor resolves a file to a program by directory, so one\n" +
                    "directory cannot hold both answers -- whichever were written, the\n" +
                    "other target's sources would be checked against the wrong one.\n" +
                    "Move one of them into its own package.",
                )
            group.options[key] = value
            group.owners[key] = entry.label
        if entry.extends:
            group.extends[entry.extends] = True
        for path in entry.include:
            group.include[path] = True

    out = []
    for package in sorted(groups):
        group = groups[package]
        if not group.options and not group.extends:
            continue
        out.append(struct(
            package = package,
            options = group.options,
            extends = sorted(group.extends),
            include = sorted(group.include),
        ))
    return out

def _nested_config_json(package, group, tsconfig_path, ambient):
    """The nested tsconfig's own content.

    `extends` is an array with the root FIRST so that a package baseline the
    targets already check against wins over it. `include`/`exclude` are written
    rather than inherited: a relative path in an extended config is re-resolved
    against the extending file, so an inherited root `exclude` would name
    something else entirely here. Inherited `paths` are not re-resolved, which is
    why the root's aliases still work from down here.
    """
    depth = len(package.split("/"))
    to_root = "/".join([".."] * depth) + "/" + tsconfig_path
    extends = [to_root] + ["./" + _relative_to(package, path) for path in group.extends]

    # An editor program must not write anything, and it is the one place these can
    # be pinned: they sit in the nested file's OWN compilerOptions, which beat
    # every `extends`. A package baseline that sets outDir/incremental -- as
    # //tests/compiler_options/baseline's does -- otherwise wins over the root's
    # noEmit and leaves emitted files and a .tsbuildinfo in the source tree. The
    # whole group has to go together: composite implies incremental, so turning
    # only the latter off is TS6379.
    options = dict(group.options)
    options["noEmit"] = True
    options["composite"] = False
    options["incremental"] = False

    # rootDir too, and to this package. A baseline is a Bazel tsconfig, so its
    # rootDir is whatever directory oxc strips for the target that uses it, and a
    # program covering the whole package is not under that -- //vite's baseline
    # says vite/src, which puts vite/tsup.config.ts outside it (TS6059).
    options["rootDir"] = "."

    return {
        "_comment": _HEADER,
        "extends": extends,
        # `files` is restated rather than inherited, and it has to be both. A
        # baseline's is a Bazel one, naming what Bazel passes rather than what
        # exists (//tests/compiler_options/baseline lists a deliberately absent
        # file to prove the attr wins, TS6053) -- but the ROOT's is where every
        # ambient declaration is named, so an empty one here loses @types/node
        # and every `declare module` with it.
        "files": [
            "/".join([".."] * len(package.split("/"))) + "/" + entry.removeprefix("./")
            for entry in ambient
        ],
        "compilerOptions": options,
        "include": group.include,
        "exclude": [],
    }

def _relative_to(package, path):
    """`path`, which is workspace-relative, expressed from inside `package`."""
    prefix = package + "/"
    if path.startswith(prefix):
        return path[len(prefix):]
    return path

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

    # Last, so a first-party module_name wins over a same-named npm package --
    # the precedence the tsconfig ts_compile generates applies too.
    for module_name, package in sorted(_modules(sources).items()):
        if packages.get(package):
            paths[module_name] = ["./{}/index".format(package)]
        paths[module_name + "/*"] = ["./{}/*".format(package), "{}/{}/*".format(bin_dir, package)]

    config = {
        "_comment": _HEADER,
        "compilerOptions": {
            "strict": True,
            "target": _ROOT_TARGET,
            "module": "Preserve",
            "moduleResolution": "Bundler",
            "jsx": _ROOT_JSX,
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
        ] + ctx.attr.extra_exclude,
    }

    if ambient:
        # An @types/* package declares globals nothing imports, so it has to be
        # named directly. Spelling `include` out is the price: a `files` array
        # switches off the implicit one, and TypeScript's implicit one is what
        # puts the workspace's own sources in the program.
        config["files"] = ambient
        config["include"] = ["**/*"]

    # A package whose targets disagree with the block above gets its own program.
    # Its files leave the root one FILE BY FILE rather than by directory: a file
    # no ts_compile claims is still worth checking, and excluding the directory
    # would drop it from the editor entirely.
    nested = _nested_configs_for(ctx, config["compilerOptions"])
    nested_outs = []
    for group in nested:
        config["exclude"] = config["exclude"] + [
            "{}/{}".format(group.package, path)
            for path in group.include
        ]
        nested_out = ctx.actions.declare_file("{}.nested.{}.json".format(
            ctx.label.name,
            group.package.replace("/", "_"),
        ))
        ctx.actions.write(
            nested_out,
            json.indent(
                json.encode(_nested_config_json(
                    group.package,
                    group,
                    ctx.attr.tsconfig_path,
                    ambient,
                )),
                indent = "  ",
            ) + "\n",
        )
        nested_outs.append(struct(
            file = nested_out,
            dest = group.package + "/tsconfig.json",
        ))

    _check_declared_nested(ctx, [n.dest for n in nested_outs])

    out = ctx.actions.declare_file(ctx.label.name + ".json")
    ctx.actions.write(out, json.indent(json.encode(config), indent = "  ") + "\n")
    return [
        DefaultInfo(files = depset([out])),
        WorkspaceCopyInfo(entries = depset(copies + nested_outs)),
        OutputGroupInfo(nested_tsconfigs = depset([n.file for n in nested_outs])),
    ]

def _nested_configs_for(ctx, root_options):
    sources = [dep[TsconfigSourcesInfo] for dep in ctx.attr.deps]
    merged = struct(option_groups = depset(transitive = [s.option_groups for s in sources]))
    return _nested_configs(merged, root_options)

def _check_declared_nested(ctx, computed):
    """Fails when the checked-in set of nested configs is not the computed one.

    glob() cannot see across package boundaries, so the macro cannot discover
    these on its own -- and an orphan left behind after a package's options
    converge with the root would silently own that whole subtree in the editor.
    So the set is declared, and disagreement in either direction is an error.
    """
    declared = sorted(ctx.attr.nested_tsconfigs)
    if declared == sorted(computed):
        return
    missing = [p for p in computed if p not in declared]
    extra = [p for p in declared if p not in computed]
    fail(
        "ts_refresh_tsconfig: the nested_tsconfigs list does not match what the\n" +
        "graph needs.\n" +
        ("  add:    {}\n".format(", ".join(missing)) if missing else "") +
        ("  remove: {}\n".format(", ".join(extra)) if extra else "") +
        "Each entry is a package that needs its own editor program because its\n" +
        "targets' compilerOptions disagree with the root block. The list is\n" +
        "declared rather than discovered because glob() cannot cross a package\n" +
        "boundary, and an entry left behind would own its subtree in the editor.",
    )

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
        nested_tsconfigs = attr.string_list(
            doc = "Every package that gets its own generated tsconfig.json, as a " +
                  "workspace-relative path to that file. Declared rather than " +
                  "discovered, and checked against what the graph needs.",
        ),
        tsconfig_path = attr.string(
            default = "tsconfig.json",
            doc = "Where the root tsconfig is written, so a nested one can point " +
                  "`extends` back at it.",
        ),
        symlink_prefix = attr.string(
            default = "bazel-",
            doc = "Value of --symlink_prefix, which names the bazel-bin symlink the IDE reads .d.ts through.",
        ),
        extra_exclude = attr.string_list(
            doc = """Globs to add to the generated `exclude`, on top of the built-in ones.

`include` stays `**/*`, so a directory holding TypeScript that is not in this
module's build graph -- a nested Bazel module, listed in .bazelignore -- is in
the program until something excludes it. Nothing in the graph names such a
directory, so this is where a workspace says so. Anchor each glob with `**/`:
an unanchored one only matches at the workspace root.""",
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
        "modules": [
            {"name": name, "package": package}
            for name, package in sorted(_modules(sources).items())
        ],
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

def _nested_tsconfig_file_impl(ctx):
    """One nested tsconfig out of the generator's output group, by destination.

    diff_test compares two single files, and the generator produces a set of
    them; without this each nested file would need its own rule instance in the
    generator, which the generator cannot know the names of at load time.
    """
    wanted = ctx.attr.dest.replace("/", "_")
    for entry in ctx.attr.generator[WorkspaceCopyInfo].entries.to_list():
        if entry.dest == ctx.attr.dest:
            return [DefaultInfo(files = depset([entry.file]))]
    fail("no generated tsconfig for {} (looked for {})".format(ctx.attr.dest, wanted))

_nested_tsconfig_file = rule(
    implementation = _nested_tsconfig_file_impl,
    attrs = {
        "generator": attr.label(
            doc = "The ide_tsconfig target whose nested outputs to pick from.",
            providers = [WorkspaceCopyInfo],
        ),
        "dest": attr.string(
            doc = "The workspace-relative destination naming which nested file to take.",
        ),
    },
)

def ts_refresh_tsconfig(
        name = "refresh_tsconfig",
        deps = [],
        npm_dir = ".bazel/npm",
        tsconfig = "tsconfig.json",
        extra_exclude = [],
        nested_tsconfigs = [],
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
        extra_exclude:
                  Globs added to the generated `exclude`, for TypeScript trees
                  that are not in this module's build graph. Anchor each with
                  `**/`.
        nested_tsconfigs:
                  Packages that need their own editor program, as
                  workspace-relative paths to the tsconfig.json each one gets
                  (e.g. "tests/compiler_options/jsx/tsconfig.json"). A package
                  belongs here when its targets set compilerOptions the root
                  block cannot also be set to -- `strict` off, a `lib` its
                  target does not imply -- because an editor resolves a file to
                  a program by directory and the root program would check those
                  files against the wrong options. The list is declared rather
                  than discovered (glob() cannot cross a package boundary) and
                  the rule fails when it disagrees with the graph in either
                  direction.
        test:     Add a diff_test that fails when `tsconfig` is stale. Turn it
                  on once `tsconfig` is checked in.
    """
    ide_tsconfig(
        name = name + ".generated",
        deps = deps,
        npm_dir = npm_dir,
        extra_exclude = extra_exclude,
        nested_tsconfigs = nested_tsconfigs,
        tsconfig_path = tsconfig,
    )
    for nested in nested_tsconfigs:
        _nested_tsconfig_file(
            name = "{}.nested.{}".format(name, nested.replace("/", "_").replace(".", "_")),
            generator = ":" + name + ".generated",
            dest = nested,
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
        for nested in nested_tsconfigs:
            slug = nested.replace("/", "_").replace(".", "_")
            diff_test(
                name = "{}_nested_{}_test".format(name, slug),
                size = "small",
                failure_message = "{} is stale: run `bazel run //:{}`.".format(nested, name),
                file1 = ":{}.nested.{}".format(name, slug),
                file2 = "//{}:tsconfig.json".format(nested.rsplit("/", 1)[0]),
            )
