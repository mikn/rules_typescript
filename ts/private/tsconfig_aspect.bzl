"""The workspace-root tsconfig.json an IDE reads, built from the build graph.

Everything the file needs is already in the graph: a ts_compile target's package
is its source root, and NpmPackageInfo names the .d.ts entry point of every npm
package reachable from it. So the tsconfig is a declared output of a rule over an aspect,
and `bazel run //:refresh_tsconfig` only copies that output into the source
tree.

A `@types/*` package takes a `paths` key spelled with the name it types --
`estree`, not `@types/estree` -- because that is the only specifier anything
imports it by, and only when no package of that name publishes declarations of
its own. Its declarations install under `npm_dir` at its own name, and the key
points in there.

The `files` array is a second route, and a narrower one: it carries the globals
a `@types/*` package declares, and it is built from what each reached target
declares in its own `deps` plus what those entries name in
`/// <reference types=...>`. A `@types/*` package reached only through an
import -- which is most of them, since a dependency's .d.ts is where
`from "estree"` is written -- is in `files` nowhere, and the `paths` key is the
only route it has. Where both routes exist they name one installed copy.

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
load(
    "//ts/private:ts_compile.bzl",
    "referenced_type_files",
    "subpath_pattern_paths",
    "subpath_wildcards",
    "types_package_alias",
)

TsconfigSourcesInfo = provider(
    doc = "What a workspace-root tsconfig.json needs from the ts_compile targets under it.",
    fields = {
        "packages": "depset of struct(path, has_index): package of every ts_compile target reached, and whether it has an index file to name as the package entry point.",
        "npm_paths": "depset of struct(key, name, version, entry, is_file, wildcard, patterns): npm entry points, relative to the package's own directory. `key` is the specifier that resolves to one and `name` the package installed under `npm_dir`; they differ for a `@types/*` package, which answers the name it types and is installed under its own, and for an `exports` subpath, which answers a key under the package's name. `wildcard` is whether the key also takes a `<key>/*` companion -- false on a subpath entry, whose key names one file and whose own subpaths are not a thing. `patterns` is NpmPackageInfo.subpath_patterns as sorted (subpath, target) pairs, each written as a `<key><subpath>` key ahead of the wildcard's guesses; empty on a subpath entry and on a package a paired `@types/*` answers.",
        "npm_ambient": "depset of struct(name, version, entry): @types/* entry points to name in the tsconfig `files` array, relative to the package's own directory.",
        "npm_files": "depset of struct(name, version, dest, file): the files an npm entry point needs on disk, and where under the package each one goes.",
        "option_groups": "depset of struct(package, label, options_json, extends, include): the program one target checks under, which the root block cannot carry -- the tsconfig it names, and allowJs for its JavaScript srcs. A target whose tsconfig turns `strict` off or names a `lib` is checked correctly by the build and wrongly by the editor unless the editor gets its own program for those files.",
        "has_content": "Whether anything above is non-empty here or anywhere below, so that a fragment is written only where there is something to say.",
    },
)

TsconfigFragmentInfo = provider(
    doc = "The per-target tsconfig fragments an editor can merge without a rule ever naming the target.",
    fields = {
        "fragments": """depset of File: one fragment per target the aspect reached.

Each is complete for its own closure -- packages and npm entry points --
so any one of them is a usable answer on its own, which is what makes a
partially built bazel-out still readable. The ambient entry points are not in
there: naming one is a tsconfig `files` concern, which a resolution map has no
key for.""",
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

    The entry point is the one ts_compile names in the tsconfig it generates for
    tsgo: the package's own module entry when its manifest designates one, the
    paired @types directory when its declarations live there, the package
    directory otherwise. //tests/npm_types_barename:test_config_agreement
    compares the two configs on that, by value.

    Which packages get an entry is not the same question, and the two configs
    still answer it differently: ts_compile names every package it resolves, and
    this names only the ones with declarations -- 165 npm keys against 105 over
    :mangled_scope's closure. So a declaration-free package is a key the build
    has and the editor does not, which resolves to nothing on the side that has
    it. The `exports` subpaths are not part of that gap: both configs read
    NpmPackageInfo.subpath_types and give each subpath its own key, because a
    `<name>/*` wildcard only guesses at one -- it substitutes the subpath as a
    path under the package and probes `.ts`, `.d.ts` and `/index.d.ts`, so it
    finds `postcss/lib/node.d.ts` and misses `rolldown/dist/config.d.mts`. The
    one-star patterns are NpmPackageInfo.subpath_patterns, read by both too.

    An entry's `key` is the specifier that resolves to it and its `name` is the
    package whose files are installed under `npm_dir`. The two differ for a
    `@types/*` package, which is imported under the name it types and installed
    under its own.
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

    # Which names a package answers with declarations of its own, which is what
    # decides whether a `@types/*` package may take the name it types --
    # ts_compile's rule, applied to the same closure.
    ships_declarations = {
        name: True
        for name, info in infos.items()
        if info.declaration_files.to_list()
    }

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
        # The name a `@types/*` package types, when no package of that name
        # publishes declarations to be shadowed. It is the only specifier
        # anything writes for it -- rollup's own .d.ts says `from "estree"` --
        # and reaching it through `files` puts the declarations in the program
        # under no name that resolves.
        alias = types_package_alias(name)
        if alias in ships_declarations:
            alias = None
        if name.startswith("@types/") and not alias:
            continue

        # A package with no declarations has nothing to tell an editor, and the
        # ones that ship none are the platform-specific binaries, whose repo
        # names would otherwise make the file differ per host.
        if not info.declaration_files.to_list():
            continue

        if name in paired:
            sources = paired[name]
            root, types_package_json = _types_root(infos, sources[0])
            if types_package_json:
                sources = sources + [types_package_json]
            first = _under(sources[0], root) or ""
            entry = first.rsplit("/", 1)[0] if "/" in first else ""
            is_file = False

            # A subpath or pattern the runtime manifest designates points into the
            # runtime package, and this branch stages the @types one instead.
            subpaths = {}
            patterns = ()
        else:
            root = info.package_dir.dirname

            # The editor half of ts_compile's npm_json_depset: the `<name>/*`
            # key already points into this directory.
            sources = (
                info.declaration_files.to_list() +
                info.json_files.to_list() +
                [info.package_dir]
            )
            entry = _under(info.module_entry_file, root) if info.module_entry_file else None
            is_file = entry != None
            entry = entry or ""
            subpaths = info.subpath_types
            patterns = tuple(sorted(info.subpath_patterns.items()))

        paths.append(struct(
            key = alias or name,
            name = name,
            version = info.package_version,
            entry = entry,
            is_file = is_file,
            wildcard = True,
            patterns = patterns,
        ))

        # What the manifest said a subpath designates, which the `<name>/*`
        # wildcard beside it only guesses: rolldown's `experimental` is
        # dist/experimental-index.d.mts, under neither the package root nor the
        # entry directory the wildcard names. ts_compile reads the same field.
        for subpath in sorted(subpaths):
            dest = _under(subpaths[subpath], root)
            if not dest:
                continue
            paths.append(struct(
                key = (alias or name) + subpath[1:],
                name = name,
                version = info.package_version,
                entry = dest,
                is_file = True,
                wildcard = False,
                patterns = (),
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

    A direct @types/* dep is how a target asks for the globals, and the entry
    point is named in `files` because `typeRoots` wants a directory whose
    children are type packages, which one repo per package never produces. The
    whole declaration set comes along -- an entry point is a list of
    `/// <reference path=...>` to its siblings, resolved on disk. What it names
    in `/// <reference types=...>` resolves on no disk the editor has either,
    so those packages' entries and files come along too, as they do in the
    tsconfig ts_compile generates.

    Only a target's own `deps` are read, plus what their entries reference,
    which is the narrow half of the module header: a @types/* package reached
    only through an import is in no `files` array and reaches the editor's
    program through its `paths` key alone.
    """
    entries = []
    files = []
    for dep in getattr(rule_attr, "deps", []):
        if NpmPackageInfo not in dep:
            continue
        info = dep[NpmPackageInfo]
        ambient = info.ambient_types_file
        if not ambient:
            continue
        for reached in referenced_type_files(ambient, info):
            package = reached.package
            if not package.package_dir:
                continue
            root = package.package_dir.dirname
            entry = _under(reached.file, root)
            if not entry:
                continue
            entries.append(struct(
                name = package.package_name,
                version = package.package_version,
                entry = entry,
            ))
            for src in package.declaration_files.to_list() + [package.package_dir]:
                dest = _under(src, root)
                if dest:
                    files.append(struct(
                        name = package.package_name,
                        version = package.package_version,
                        dest = dest,
                        file = src,
                    ))
    return entries, files

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

def _fragment_npm(entry):
    record = {
        "npm": entry.key,
        "version": entry.version,
        "entry": entry.entry,
        "file": entry.is_file,
    }
    if entry.name != entry.key:
        record["dir"] = entry.name
    return json.encode(record)

def _fragment(target, ctx, sources):
    """One JSON object per line, carrying `sources`, for the tsserver hook to merge.

    Deliberately not built from _packages/npm_view: those materialise their
    depsets, which is affordable once for one ide_tsconfig target and not on
    every target of every build. An Args with map_each defers the same work to
    execution time, so the depsets stay depsets through analysis.
    """
    args = ctx.actions.args()
    args.set_param_file_format("multiline")
    args.add(json.encode({"format": _FRAGMENT_FORMAT, "label": str(target.label)}))
    args.add_all(sources.packages, map_each = _fragment_package, uniquify = True)
    args.add_all(sources.npm_paths, map_each = _fragment_npm, uniquify = True)

    out = ctx.actions.declare_file(target.label.name + FRAGMENT_SUFFIX)
    ctx.actions.write(out, args)
    return out

# The root block's own values.
_ROOT_TARGET = "ES2022"
_ROOT_JSX = "react-jsx"

# TypeScript compares these values case-insensitively, and treats `lib` as a set.
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
    """The editor program one ts_compile target needs beyond the root block, or []."""

    # A `manual` target is one nothing builds -- a fixture read by an analysis
    # test rather than run. There is no editor program to get right.
    if "manual" in getattr(ctx.rule.attr, "tags", []):
        return []

    # A .js source is only in the program at all with allowJs, which ts_compile
    # infers from srcs rather than making the author say it.
    sources = [f for src in ctx.rule.files.srcs for f in [src]]
    options = {}
    if any([f.extension in ("js", "jsx", "mjs", "cjs") for f in sources]):
        options["allowJs"] = True

    # The tsconfig this target checks against. An editor program for these files
    # has to start from the same place, or it disagrees for a second reason.
    extends = ""
    tsconfig = getattr(ctx.rule.attr, "tsconfig", None)
    if tsconfig and TsConfigInfo in tsconfig:
        extends = tsconfig[TsConfigInfo].tsconfig.short_path
    elif getattr(ctx.rule.file, "tsconfig", None):
        extends = ctx.rule.file.tsconfig.short_path

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
    # A workspace member's view reaches the member through `target`, the way a
    # ts_compile reaches its deps; ts_compile's own `target` is an ES version.
    reached = list(getattr(ctx.rule.attr, "deps", []))
    if type(getattr(ctx.rule.attr, "target", None)) == "Target":
        reached.append(ctx.rule.attr.target)
    inherited = [dep[TsconfigSourcesInfo] for dep in reached if TsconfigSourcesInfo in dep]

    packages = []
    npm_paths = []
    npm_ambient = []
    npm_files = []
    option_groups = []

    # A workspace member's view adds nothing of its own: the checkout's
    # node_modules holds pnpm's link to the member, and the editor resolves it there.
    if ctx.rule.kind == "ts_compile":
        if target.label.package:
            packages = [struct(
                path = target.label.package,
                has_index = _has_index(target, ctx),
            )]
        npm_paths, npm_files = _npm_entries(ctx.rule.attr)
        npm_ambient, ambient_files = _ambient_entries(ctx.rule.attr)
        npm_files = npm_files + ambient_files
        option_groups = _option_group(target, ctx)

    sources = TsconfigSourcesInfo(
        packages = depset(packages, transitive = [s.packages for s in inherited], order = "postorder"),
        npm_paths = depset(npm_paths, transitive = [s.npm_paths for s in inherited], order = "postorder"),
        npm_ambient = depset(npm_ambient, transitive = [s.npm_ambient for s in inherited], order = "postorder"),
        npm_files = depset(npm_files, transitive = [s.npm_files for s in inherited], order = "postorder"),
        option_groups = depset(option_groups, transitive = [s.option_groups for s in inherited], order = "postorder"),
        has_content = bool(packages or npm_paths) or any([s.has_content for s in inherited]),
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
    attr_aspects = ["deps", "target"],
    doc = """Collects the source roots and npm entry points an IDE tsconfig needs.

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

# tsserver resolves a plugin as <probe location>/node_modules/<plugin name>, so
# `.bazel` is the probe location an editor names and this is the package.
_PLUGIN_NAME = "@rules_typescript/tsserver-plugin"
_PLUGIN_DIR = ".bazel/node_modules/" + _PLUGIN_NAME

def _collect(sources, field):
    # The one place the graph has to become a file, so the one place a depset is
    # materialised.
    return depset(transitive = [getattr(s, field) for s in sources]).to_list()

def npm_key_beats(candidate, held):
    """Whether `candidate` should take the `paths` key both entries claim.

    Two packages claim one key when a `@types/x` package answers `x`: npm reads
    `node_modules/x` first and reaches `node_modules/@types/x` only when it
    finds no declarations there, so the entry installed under the key's own name
    wins. _npm_entries applies that per target from `ships_declarations`, and
    one target's closure is all it can see -- a closure holding `@types/x` and
    no `x` gives the alias the key while another target's closure has the real
    `x`, and the root config aggregates closures. Without this the winner is
    whichever package name sorted first, and `@types/x` sorts before `x`.
    Exported for the unit test.
    """
    if held == None:
        return True
    if (candidate.name == candidate.key) != (held.name == held.key):
        return candidate.name == candidate.key
    return candidate.name < held.name

def _one_entry_per_key(entries):
    chosen = {}
    for entry in entries:
        if npm_key_beats(entry, chosen.get(entry.key)):
            chosen[entry.key] = entry
    return [chosen[key] for key in sorted(chosen)]

def npm_view(sources, host_only = []):
    """The npm entry points to write, the ambient ones, and the files each needs.

    Two versions of one package name would fight over the same directory under
    npm_dir, so the lowest version wins for the whole package.

    host_only names packages left out entirely: a package pnpm resolves on some
    hosts and not others is one a checked-in file cannot name without differing
    per host, and the declaration-free platform binaries are already dropped
    upstream of here.

    Exported for the unit test.
    """
    skip = {name: True for name in host_only}
    chosen = {}
    for entry in _collect(sources, "npm_paths") + _collect(sources, "npm_ambient"):
        if entry.name in skip:
            continue
        if entry.name not in chosen or entry.version < chosen[entry.name]:
            chosen[entry.name] = entry.version

    claimed = [
        e
        for e in _collect(sources, "npm_paths")
        if chosen.get(e.name) == e.version
    ]
    entries = _one_entry_per_key(claimed)
    winner_of = {e.key: e for e in entries}

    # Derived from `claimed` rather than from "did this name win a key": an
    # `@types/x` shadowed by an `x` that ships declarations in the same closure
    # is dropped by _npm_entries before it claims anything, yet still reaches
    # `files` through _ambient_entries.
    #
    # Membership is per (name, key) because a collision is per key: an
    # `@types/x` that loses the bare `x` and wins an `exports` subpath key of
    # its own is still the loser of the bare one.
    won = {(e.name, e.key): True for e in entries}
    lost = {}
    for e in sorted(claimed, key = lambda e: e.key):
        if (e.name, e.key) not in won and e.name not in lost:
            lost[e.name] = winner_of[e.key]

    files = sorted(
        [f for f in _collect(sources, "npm_files") if chosen.get(f.name) == f.version],
        key = lambda f: f.name + "/" + f.dest,
    )
    installed = {f.name + "/" + f.dest: True for f in files}

    ambient = {}
    for entry in _collect(sources, "npm_ambient"):
        if chosen.get(entry.name) != entry.version:
            continue

        # A `files` entry under a name whose key went to another package names a
        # second copy of the declarations that key already reaches, so every
        # global in them is declared twice. Naming the winner's copy keeps the
        # entry: real tsc pulls `node_modules/@types/x` in for its globals
        # however `node_modules/x` shadows it for resolution, so dropping it
        # would lose them for a target that imports nothing.
        #
        # Only a winner with no `types` entry of its own is that copy, and the
        # reason is upstream: _npm_entries drops an `@types/x` shadowed by an
        # `x` that ships declarations, so a winner that reached here without an
        # entry is one whose directory the pairing filled from this very
        # package. A winner that names its own entry may ALSO ship a file where
        # the loser's sat -- a legacy root stub beside an `exports` entry under
        # dist/ -- and naming that would swap the globals for an unrelated
        # file, silently, which is the harm this repoint exists to avoid.
        winner = lost.get(entry.name)
        if winner and winner.entry:
            continue
        dir = winner.name if winner else entry.name
        if dir != entry.name and dir + "/" + entry.entry not in installed:
            continue
        ambient[entry.name] = struct(dir = dir, entry = entry.entry)
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

_ONE_ANSWER_PER_DIRECTORY = (
    "An editor resolves a file to a program by directory, so one\n" +
    "directory cannot hold both answers -- whichever were written, the\n" +
    "other target's sources would be checked against the wrong one.\n" +
    "Move one of them into its own package."
)

def _nested_configs(sources, root_options):
    """One editor program per package whose options the root block cannot carry.

    tsserver picks the nearest tsconfig.json walking up from a file, so a package
    whose targets check under a tsconfig of their own needs a file there that
    extends it. Grouping is by package because that is the only granularity
    tsserver has: two targets in one directory naming two tsconfigs have no
    representation at all, and that is an error rather than a silent pick --
    `extends` would take both, but TypeScript applies the array later-wins, so
    one file's keys would replace the other's for both targets' sources.

    allowJs is dropped where the root already sets it, so a target that only
    restates the defaults produces no file. That subtraction is sound only
    without a tsconfig: the root is the FIRST entry of the nested `extends`
    array, so a tsconfig after it wins every key the group leaves out.
    """
    root_options = _canonical_options(root_options)

    groups = {}
    for entry in sources.option_groups.to_list():
        group = groups.setdefault(entry.package, struct(
            options = {},
            extends = {},
            include = {},
        ))
        group.options.update(json.decode(entry.options_json))
        if entry.extends:
            for baseline, owner in group.extends.items():
                if baseline != entry.extends:
                    fail(
                        "ts_refresh_tsconfig: {} and {} are in the same package and\n".format(
                            owner,
                            entry.label,
                        ) +
                        "  extend the tsconfig baselines {} and {}.\n".format(
                            json.encode(baseline),
                            json.encode(entry.extends),
                        ) +
                        _ONE_ANSWER_PER_DIRECTORY,
                    )
            group.extends[entry.extends] = entry.label
        for path in entry.include:
            group.include[path] = True

    out = []
    for package in sorted(groups):
        group = groups[package]
        options = group.options
        if not group.extends:
            options = {
                key: value
                for key, value in options.items()
                if key not in root_options or root_options[key] != value
            }
        if not options and not group.extends:
            continue
        out.append(struct(
            package = package,
            options = options,
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
    why the root's package keys still work from down here.
    """
    depth = len(package.split("/"))
    to_workspace = "/".join([".."] * depth) + "/"
    to_root = to_workspace + tsconfig_path
    extends = [to_root] + [_relative_to(package, path, to_workspace) for path in group.extends]

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
            to_workspace + entry.removeprefix("./")
            for entry in ambient
        ],
        "compilerOptions": options,
        "include": group.include,
        "exclude": [],
    }

def _relative_to(package, path, to_workspace):
    """`path`, which is workspace-relative, expressed from inside `package`."""
    prefix = package + "/"
    if path.startswith(prefix):
        return "./" + path[len(prefix):]
    return to_workspace + path

def _ide_tsconfig_impl(ctx):
    sources = [dep[TsconfigSourcesInfo] for dep in ctx.attr.deps]
    bin_dir = "./{}bin".format(ctx.attr.symlink_prefix)

    packages = _packages(sources)
    npm_entries, npm_ambient, npm_files = npm_view(sources, ctx.attr.host_only_packages)

    paths = {}
    copies = []
    ambient = []

    if ctx.attr.npm_dir:
        for entry in npm_entries:
            if entry.key in paths:
                continue
            installed = "./" + _installed_entry(ctx.attr.npm_dir, entry)
            paths[entry.key] = [installed]

            # Both substitution roots, in ts_compile's own order and from its
            # own helper. The entry's directory alone left the package root
            # unreachable: `vite/dist/node/index` is a plain path under the
            # package, which is how a package with no `exports` map spells its
            # subpaths, and the build has always resolved it.
            if entry.wildcard:
                root = "./{}/{}".format(ctx.attr.npm_dir, entry.name)
                entry_dir = installed.rsplit("/", 1)[0] if entry.is_file else installed
                paths[entry.key + "/*"] = subpath_wildcards(root, entry_dir)
                for subpath, target in entry.patterns:
                    paths[entry.key + subpath[1:]] = subpath_pattern_paths(root, entry_dir, subpath, target)

        for npm_file in npm_files:
            copies.append(struct(
                file = npm_file.file,
                dest = "{}/{}/{}".format(ctx.attr.npm_dir, npm_file.name, npm_file.dest),
            ))

        ambient = [
            "./{}/{}/{}".format(ctx.attr.npm_dir, value.dir, value.entry)
            for _, value in npm_ambient
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
            # Not sufficient on its own: tsserver resolves a plugin named here
            # against its probe locations, so a client that passes no
            # --pluginProbeLocations still loads nothing. It is here so the
            # entry survives the next refresh, which rewrites this file whole.
            "plugins": [{"name": _PLUGIN_NAME}],
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
    "host_only_packages": attr.string_list(
        doc = """npm packages to leave out of the generated `paths` map.

pnpm resolves an `optionalDependencies` entry only on the hosts its `os`/`cpu`
fields match, so a package that exists on one developer's machine and not
another's makes a checked-in tsconfig differ per host -- and the staleness test
then fails for everyone on the other platform. Packages shipping no
declarations are already dropped, which covers the platform binaries; this is
for one that does ship them, such as `fsevents`.""",
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
    npm_entries, _, _ = npm_view(sources, ctx.attr.host_only_packages)

    data = {
        "_comment": _HEADER,
        "npmDir": ctx.attr.npm_dir,
        "npmPackages": [
            {"name": e.key, "dir": e.name, "entry": e.entry, "isFile": e.is_file}
            for e in (npm_entries if ctx.attr.npm_dir else [])
        ],
        "packages": sorted(_packages(sources)),
    }

    out = ctx.actions.declare_file(ctx.label.name + ".json")
    ctx.actions.write(out, json.indent(json.encode(data), indent = "  ") + "\n")
    return [DefaultInfo(files = depset([out]))]

ide_hook_data = rule(
    implementation = _ide_hook_data_impl,
    attrs = _IDE_ATTRS,
    doc = """Writes what the tsserver hook needs from the build graph.

The hook runs inside a long-lived editor process, so it reads this file rather
than asking Bazel: the package list and the npm entry points are analysis-time
facts, and querying for them would fight the server lock.""",
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
        host_only_packages = [],
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
        host_only_packages:
                  npm packages left out of the generated `paths`, for ones pnpm
                  resolves on only some hosts (an `optionalDependencies` entry
                  whose `os`/`cpu` matches) and that ship declarations, so the
                  checked-in file does not differ per host.
        nested_tsconfigs:
                  Packages that need their own editor program, as
                  workspace-relative paths to the tsconfig.json each one gets
                  (e.g. "tests/compiler_options/jsx/tsconfig.json"). A package
                  belongs here when its targets name a tsconfig of their own,
                  because an editor resolves a file to a program by directory
                  and the root program would check those files against the
                  wrong options. The list is declared rather
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
        host_only_packages = host_only_packages,
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
        host_only_packages = host_only_packages,
        npm_dir = npm_dir,
    )
    refresh_workspace_files(
        name = name,
        files = {
            ":" + name + ".generated": tsconfig,
            ":" + name + ".hook_data": ".bazel/tsserver-hook-data.json",
            "@rules_typescript//tools:tsserver-hook.js": ".bazel/tsserver-hook.js",
            "@rules_typescript//tools:tsserver-hook-resolver.js": ".bazel/tsserver-hook-resolver.js",
            "@rules_typescript//tools:tsserver-hook-worker.js": ".bazel/tsserver-hook-worker.js",
            "@rules_typescript//tools:tsserver-plugin.js": _PLUGIN_DIR + "/index.js",
            "@rules_typescript//tools:tsserver-plugin-package.json": _PLUGIN_DIR + "/package.json",
            "@rules_typescript//tools:tsserver-plugin-resolver": _PLUGIN_DIR + "/tsserver-hook-resolver.js",
            "@rules_typescript//tools:tsserver-plugin-worker": _PLUGIN_DIR + "/tsserver-hook-worker.js",
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
