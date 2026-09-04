"""Core TypeScript compilation rule using oxc-bazel.

ts_compile transforms .ts/.tsx source files into .js + .js.map + .d.ts outputs
using the oxc-bazel CLI as a Bazel action.

JavaScript sources (.js/.mjs/.cjs) are accepted too. They need no transform, so
they are materialised in the output tree unchanged and joined into the type
program: `import "./util.js"` resolves, JSDoc types cross the package boundary,
and `checkJs` in compiler_options type-checks them.

srcs may span a whole subtree. Every output keeps its package-relative path, so
one target can hold `index.ts` and `nested/helper.ts` together.

The .d.ts output is the compilation boundary artifact: downstream targets
depend only on .d.ts files, so Bazel's content-based caching means that if a
dep's .d.ts doesn't change (e.g. because an internal implementation detail
changed but the public API did not), dependents are not recompiled.

When a tsgo toolchain is available, ts_compile also runs type-checking as a
Bazel validation action in the _validation output group. Validation actions
run unconditionally during `bazel build` but do NOT block downstream
compilation.
"""

load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")
load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsConfigInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")
load("//ts/private:toolchain.bzl", "OXC_TOOLCHAIN_TYPE", "TSGO_TOOLCHAIN_TYPE", "get_oxc_toolchain")

TsModuleInfo = provider(
    doc = """The bare specifier a ts_compile target is importable as.

A first-party package imported as `@scope/pkg` rather than by relative path
needs a paths entry pointing at the .d.ts files Bazel produced for it. Only the
producing target knows where those land under the current configuration, so the
name travels with the target instead of being written into a consumer's
tsconfig by hand.
""",
    fields = {
        "module_name": "string: the bare specifier for this target, or '' if it declared none.",
        "label": "string: the label of this target, as a dep list writes it.",
        "declaration_root": "string: exec-root-relative directory the target's generated .d.ts files land in.",
        "source_root": "string: exec-root-relative package directory, where .d.ts files passed straight through stay.",
        "declared_paths": "tuple of struct(specifier, declarations): what the module's own package.json says its specifiers resolve to. `specifier` is the part after the module name -- \"\" for the bare name, \"/button\" for a subpath, \"/tokens/*\" for a wildcard one -- and `declarations` are the module-root-relative declaration paths it designates, in resolution order. Empty on a target that declared its name with `module_name`: nothing there reads a manifest.",
        "transitive_modules": "depset of struct(module_name, label, declaration_root, source_root, declared_paths): this target's modules and its deps'.",
    },
)

# ─── Helpers ──────────────────────────────────────────────────────────────────

_TS_EXTENSIONS = ["ts", "tsx"]

_JS_EXTENSIONS = ["js", "mjs", "cjs"]

# tsc's own naming for the declaration it emits from a JavaScript source.
_JS_DECLARATION_EXTENSION = {
    "js": ".d.ts",
    "mjs": ".d.mts",
    "cjs": ".d.cts",
}

_DECLARATION_SUFFIXES = (".d.ts", ".d.mts", ".d.cts")

_SRC_SUFFIXES = tuple(["." + ext for ext in _TS_EXTENSIONS + _JS_EXTENSIONS])

def _hangs_off_another_root(package, src):
    """Whether a src, as a BUILD file wrote it, names a file outside this package's tree.

    Only a label that names a source file in an explicit package locates
    anything: a bare filename, a `:name` and every glob result belong to the
    package that wrote them, and a label naming a rule stands for files this
    phase cannot place. A repository part that is empty once its `@`s are
    stripped -- `@//pkg:f` and the canonical `@@//pkg:f` -- is this repository.
    """
    if not src.endswith(_SRC_SUFFIXES):
        return False
    if src.startswith("@"):
        marker = src.find("//")
        if marker == -1:
            return False
        if src[:marker].lstrip("@"):
            return True
        src = src[marker:]
    if not src.startswith("//"):
        return False
    named = src[2:].split(":", 1)[0]
    return named != package and not named.startswith(package + "/")

def mixed_src_packages(package, srcs):
    """The srcs that hang off a root this package's own srcs do not.

    A src is compiled into the package of the target that LISTS it -- its
    outputs are declared under that package -- but the root its
    package-relative path hangs off is where the file actually lives. A file
    outside this package's directory therefore hangs off a root of its own
    while this package's files hang off the package, and one tsgo declaration
    emit has one rootDir.

    A DESCENDANT package's file is already inside this package's directory and
    shares that root: ts_compile holds whole subtrees, and a subtree may grow a
    BUILD file. The TOP-LEVEL package is the exec root, which is the root a src
    from anywhere else hangs off. A DECLARATION is passed through rather than
    compiled, so it declares no output and joins no rootDir -- which is what
    makes `vite_types = True` legal from any package. A `select` decides its
    srcs after loading is over, and a label naming a rule or a filegroup does
    not say where its files live; the analysis-time root check covers both.

    Args:
        package: The listing target's own package, from native.package_name().
        srcs: The `srcs` list as the BUILD file wrote it.
    """
    if not package or type(srcs) != "list":
        return []
    other = []
    own = False
    for src in srcs:
        if type(src) != "string" or src.endswith(_DECLARATION_SUFFIXES):
            continue
        if _hangs_off_another_root(package, src):
            other.append(src)
        else:
            own = True
    return other if own else []

def fail_on_mixed_src_packages(kind, name, srcs, declarations, enable_check):
    """The one-rootDir rule's srcs-list half, checked while the BUILD file loads.

    Gated on the tsgo declaration emit, which is the emit that has one rootDir:
    `declarations = "oxc"` groups the sources by root and runs oxc once per
    group, and `enable_check = False` emits nothing from tsgo at all. Those two
    are the escape hatch the analysis-time error already offers.
    """
    if declarations == "oxc" or not enable_check:
        return
    package = native.package_name()
    other = mixed_src_packages(package, srcs)
    if not other:
        return
    fail(
        "{}: srcs on //{}:{} mix this package's own files with files that live ".format(kind, package, name) +
        "outside it:\n" +
        "".join(["  {}\n".format(src) for src in other]) +
        "A src keeps the package-relative path of where it actually lives, so " +
        "these hang off a root of their own while this package's srcs hang off " +
        "'{}' -- and one tsgo declaration emit has one rootDir. Each would ".format(package) +
        "also be emitted a second time under '{}', once per package that ".format(package) +
        "lists it.\n" +
        "Give them a target in their own package and depend on that (set " +
        "module_name on it when the import is by bare specifier), or set " +
        "declarations = \"oxc\" or enable_check = False, neither of which emits " +
        "from tsgo.",
    )

def _is_dts_source(f):
    """Returns True if the file is a declaration file."""
    return f.basename.endswith(_DECLARATION_SUFFIXES)

def _package_relative_path(f, pkg):
    """Returns the path of a src relative to the target's package, extension intact."""
    p = f.short_path
    if p.startswith("../"):
        # An external-repo file: ../<repo name>/<rest>.
        parts = p.split("/", 2)
        if len(parts) == 3:
            p = parts[2]
    if pkg and p.startswith(pkg + "/"):
        p = p[len(pkg) + 1:]
    return p

def _strip_ts_extension(p):
    for ext in (".tsx", ".ts"):
        if p.endswith(ext):
            return p[:-len(ext)]
    return p

def _package_relative_stem(f, pkg):
    """Returns the package-relative path with the TypeScript extension stripped."""
    return _strip_ts_extension(_package_relative_path(f, pkg))

def _source_root(f, pkg):
    """Returns the exec-root-relative directory the package-relative path hangs off.

    A checked-in source gives the package directory; a generated one gives the
    bin directory plus the package. oxc's --strip-dir-prefix takes a single
    value, so two srcs with different roots cannot share one invocation.
    """
    rel = _package_relative_path(f, pkg)
    p = f.path
    if p == rel:
        return ""
    if p.endswith("/" + rel):
        return p[:len(p) - len(rel) - 1]
    return f.dirname

def _rebased_declarations(roots, declarations):
    """Each declaration under each of the module's roots, in the order given."""
    out = []
    for root in roots:
        for declaration in declarations:
            candidate = root + "/" + declaration
            if candidate not in out:
                out.append(candidate)
    return out

def _relative_path(from_dir, to_dir):
    """Computes a relative path from from_dir to to_dir.

    Both arguments are /-separated directory paths. Returns a string like
    "../../other/pkg" or "." when from_dir == to_dir.
    """
    from_parts = [p for p in from_dir.split("/") if p]
    to_parts = [p for p in to_dir.split("/") if p]
    common_len = 0
    for i in range(min(len(from_parts), len(to_parts))):
        if from_parts[i] == to_parts[i]:
            common_len += 1
        else:
            break
    up_parts = [".."] * (len(from_parts) - common_len)
    down_parts = to_parts[common_len:]
    result = up_parts + down_parts
    return "/".join(result) if result else "."

def explicitly_relative(path):
    """A `paths` value spelled so TypeScript reads it as a path, not a package.

    `_relative_path` answers with a bare segment whenever the target sits under
    the tsconfig's own directory, and tsgo removed `baseUrl`, so TypeScript reads
    that as a module specifier and rejects it with TS5090. TypeScript's own test
    for an already-relative path is `^\\.\\.?($|/)` -- which is why a leading
    dot alone does not qualify: `.bazel/npm/x` is a directory named `.bazel`, not
    a relative path. Exported for the unit test.
    """
    if path in (".", "..") or path.startswith("./") or path.startswith("../") or path.startswith("/"):
        return path
    return "./" + path

def subpath_roots(tsconfig_dir, pkg_root, entry_rel_dir):
    """Where `pkg/sub` may live, in the order npm would look.

    With no `exports` map -- which is most of the registry -- `pkg/sub` is a
    plain path under the package root, so `recharts/types/shape/Curve` is
    `<recharts>/types/shape/Curve`. Hanging the wildcard off the entry's own
    directory instead spells that `<recharts>/types/types/shape/Curve`. The
    entry directory stays as a second substitution: a package whose subpaths do
    sit beside its entry keeps resolving, and TypeScript tries each in turn.
    Exported for the unit test.
    """
    return subpath_wildcards(
        explicitly_relative(_relative_path(tsconfig_dir, pkg_root)),
        entry_rel_dir,
    )

def subpath_wildcards(pkg_root_rel, entry_rel_dir):
    """`subpath_roots` over two directories already relative to the tsconfig.

    tsconfig_aspect reaches the same two by its own route -- the installed tree
    under `npm_dir` rather than an external repository -- and which order they
    go in is the rule above, not a second opinion about it. Exported for that
    caller and for the unit test.
    """
    roots = [pkg_root_rel] if pkg_root_rel == entry_rel_dir else [pkg_root_rel, entry_rel_dir]
    return [r + "/*" for r in roots]

def types_package_alias(package_name):
    """The name `@types/x` supplies declarations for, or None for any other package.

    DefinitelyTyped publishes `x`'s declarations as `@types/x`, and a scoped
    `@a/b`'s as `@types/a__b`. TypeScript pairs the two by walking
    `node_modules/@types`, which this ruleset does not have: npm packages reach
    the compiler through `paths`, and a key spelled `@types/x` answers no import
    anyone writes. Exported for the unit test.
    """
    if not package_name.startswith("@types/"):
        return None
    unmangled = package_name[len("@types/"):]
    scope, separator, name = unmangled.partition("__")
    return "@" + scope + "/" + name if separator else unmangled

def types_package_name(package_name):
    """The `@types/*` package DefinitelyTyped publishes `package_name`'s declarations as.

    The inverse of `types_package_alias`: `@a/b` is `@types/a__b`. Exported for
    the unit test.
    """
    if package_name.startswith("@"):
        return "@types/" + package_name[1:].replace("/", "__", 1)
    return "@types/" + package_name

def include_entry(tsconfig_dir, src_dir, basename):
    """Returns a src's `include` entry, relative to the generated tsconfig.

    Exported for the unit test. A src in the workspace root has an empty
    dirname, which names the exec root -- not the tsconfig's own directory.
    """
    return _relative_path(tsconfig_dir, src_dir) + "/" + basename

# ─── Tsconfig generation ─────────────────────────────────────────────────────

# Options whose values encode the sandbox layout or the action's declared
# outputs: a user value here breaks the build rather than configuring it, so
# setting one fails with the mapped text as the pointer to the real knob.
_BAZEL_OWNED_OPTIONS = {
    "baseUrl": "tsgo removed baseUrl (TS5102); use path_aliases, whose values Bazel rewrites",
    "rootDirs": "rootDirs bridges the source tree and the output tree",
    "preserveSymlinks": "the sandbox stages every input as a symlink, and following one reaches files the target never declared",
    "paths": "use path_aliases for source aliases, or module_name on the target that produces the declarations",
    "outDir": "outDir must be the directory Bazel declared the outputs in",
    "rootDir": "rootDir must be the source directory oxc strips",
    "declarationDir": "declarations must land next to the .js Bazel declared",
    "declaration": "declaration emit follows from the `declarations` attr",
    "emitDeclarationOnly": "declaration emit follows from the `declarations` attr",
    "declarationMap": "declaration map emit follows from the `declaration_map` attr",
    "sourceMap": "source map emit follows from the `source_map` attr",
    "noEmit": "declaration emit follows from the `declarations` attr",
    "noEmitOnError": "a target that fails to check must not leave a declaration on disk",
    "isolatedDeclarations": "isolated declarations follow from declarations = \"oxc\"",
    "composite": "cross-target wiring is Bazel's job, not tsc's",
    "incremental": "Bazel declares no .tsbuildinfo output",
    "tsBuildInfoFile": "Bazel declares no .tsbuildinfo output",
}

# tsgo flags that report on the program without changing what it emits or how
# it resolves. Everything else is either a compilerOption -- which belongs in
# `compiler_options`, where the guard above can see it -- or a flag that would
# move outputs Bazel already declared.
_ALLOWED_TSGO_ARGS = [
    "--diagnostics",
    "--explainFiles",
    "--extendedDiagnostics",
    "--listEmittedFiles",
    "--listFiles",
    "--noErrorTruncation",
    "--traceResolution",
]

# Required by the .d.ts this ruleset generates for css_module, css_library,
# asset_library and json_library deps, whose extensions TypeScript otherwise
# refuses. Beneath the user's options, so an explicit value still wins.
_RULESET_OPTIONS = {
    "allowArbitraryExtensions": True,
}

# The options a TypeScript target gets from this ruleset whether or not it names
# a `tsconfig`. Without one they go straight into the generated config; with one
# they go into a file that config extends FIRST, so every key the user's tsconfig
# (or its own extends chain) mentions wins and only the keys it says nothing
# about fall back here. Naming a tsconfig therefore adds what the file says
# instead of subtracting what the ruleset already guaranteed.
_BASELINE_OPTIONS = {
    "strict": True,
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

# `module` and `moduleResolution` are one setting spelled as two keys, and
# TypeScript rejects a pair it did not derive itself: Bundler under module
# NodeNext is TS5109, NodeNext under any other module is TS5110. Layers merge
# per key, so a layer whose `module` a later one replaces is left asserting the
# partner of a module that is no longer there. Defaulting `module` alone stays
# safe -- tsgo derives a legal resolver from every value it takes -- so this one
# key is asserted only by the layer that also owns `module`, and withdrawn
# everywhere else. That withdrawal changes nothing that resolves today: tsgo
# derives Bundler from every module but Node16/NodeNext, which derive their own.
_DERIVED_FROM_MODULE = "moduleResolution"

def _drop_derived_resolution(opts, overriding_layer):
    """Withdraws `moduleResolution` unless `opts` also owns the `module` it fits.

    `overriding_layer` is the options of whoever can still replace this layer's
    `module`: the user's compiler_options over the generated config, or -- with
    no way to read a tsconfig at analysis time -- None for their tsconfig over
    the baseline file, which can never be shown to have left `module` alone.
    """
    if overriding_layer == None or "module" in overriding_layer or _DERIVED_FROM_MODULE in overriding_layer:
        opts.pop(_DERIVED_FROM_MODULE, None)
    return opts

# Options whose values are paths a user copies out of a package's own
# tsconfig.json, where they are written relative to that package.
_PACKAGE_RELATIVE_OPTIONS = ["types", "typeRoots"]

def _rebase_package_relative(entry, package_rel):
    """Rewrites a package-relative path so it resolves from the generated config.

    Entries that are not relative paths (npm package names such as "node" or
    "@cloudflare/workers-types") are returned untouched.
    """
    if entry == ".":
        return package_rel
    if entry.startswith("./"):
        return package_rel + "/" + entry[2:]
    if entry.startswith("../"):
        return package_rel + "/" + entry
    return entry

# An entry that resolved to a staged file is written as the path to that file:
# the source tree for a checked-in declaration, bazel-out for a generated one.
def _rebase_types_entry(entry, package_rel, tsconfig_dir, types_files):
    found = types_files.get(types_entry_declaration(entry))
    if found:
        return explicitly_relative(include_entry(tsconfig_dir, found.dirname, found.basename))
    return _rebase_package_relative(entry, package_rel)

# A tsconfig `types` entry names a package, and TypeScript resolves it by walking
# node_modules for that package and reading its manifest. There is no
# node_modules here -- npm packages reach the compiler through `paths`, which
# `types` does not consult -- so an entry that would have resolved natively
# resolves to nothing and its declarations never join the program.
#
# So the entry is resolved here instead, against what the package's own manifest
# designated. The file goes in `files`, which is how every other ambient
# declaration in this ruleset reaches tsgo.
#
# Gazelle reads the same shapes out of a `types` entry, in `ambientTypeLabel`
# (gazelle/config.go), for the other half of the job: it reads the entries out of
# a tsconfig file and writes the npm deps, while the rule reads the attribute and
# resolves it against those deps. Different inputs, one vocabulary -- an entry
# the two classify differently is either one the rule ignores while Gazelle
# writes a dep for it, silently back to the bug the guard below exists for, or a
# package the rule demands a dep for that Gazelle never writes: a fail() nothing
# can clear. So one table of shapes is asserted on both sides:
# `types_entry_package_ref_test` in //tests/compiler_options/analysis and
# `TestTsConfigTypes_EntryShapesAreClassifiedLikeTheRule` in //gazelle.
def types_entry_package_ref(entry):
    """The package one `compilerOptions.types` entry names, or "".

    The recogniser of this attribute for the whole ruleset: anything that has
    to know what a `types` entry names calls this rather than spelling the
    shapes again, and `types_entry_file` below is the one resolution built on
    it. A second spelling is a second answer for some entry, and the guard
    below turns a disagreement into a fail() no dep clears.

    "" for an entry that names a path instead: one starting with `.` or `/`, or
    ending in a declaration extension, which no dep resolves.
    `types_entry_declaration` below takes the two of those shapes the rule
    resolves itself. Whitespace is trimmed first, which is what Gazelle's
    `ambientTypeLabel` does before it reads the same shapes -- so a padded entry
    it writes a dep for is one this spends that dep on, and a blank entry, which
    it writes no dep for, trims away to no package at all.
    """
    entry = entry.strip()
    if entry.startswith(".") or entry.startswith("/") or entry.endswith(_DECLARATION_SUFFIXES):
        return ""
    return entry

# `^\.\.?($|/)` is TypeScript's own test for a path here; this takes the `./` and
# `../` of it that end in a declaration extension and leaves the rest --
# `vendor/x.d.ts`, `.`, `./typings` -- to the compiler, whatever
# `_rebase_package_relative` rewrote.
def types_entry_declaration(entry):
    """The declaration file one `types` entry names, package-relative, or "".

    Paired with `types_entry_package_ref` above: between them they classify
    every entry, one to a dep and one to a label of this target's, and an entry
    both answer "" for is the compiler's own to resolve.

    A relative entry that does not end in a declaration extension is one of
    those: `./typings` is a directory whose declarations TypeScript picks by
    reading it, which Starlark cannot do.
    """
    entry = entry.strip()
    if not entry.startswith("./") and not entry.startswith("../"):
        return ""
    return entry if entry.endswith(_DECLARATION_SUFFIXES) else ""

def workspace_relative(package, entry):
    """`entry`, written relative to `package`, as a path from the workspace root.

    "" when its `..` segments climb out above the root, which no input answers.
    """
    parts = []
    for part in (package.split("/") if package else []) + entry.split("/"):
        if part in ("", "."):
            continue
        if part != "..":
            parts.append(part)
        elif parts:
            parts.pop()
        else:
            return ""
    return "/".join(parts)

def types_entry_file(entry, npm_info):
    """The declaration `entry` designates in `npm_info`, or None.

    The resolution for the whole ruleset, the way `types_entry_package_ref` is
    the classification: calling these two is how a second reader of the
    attribute stays the same reader, rather than a copy that comes to disagree
    about one entry.

    Four package spellings resolve, each one TypeScript would have walked
    node_modules for: the package itself; one of its `exports` subpaths; a
    subpath its manifest says nothing about, answered by the declaration the
    package ships there, so `@cloudflare/workers-types/2023-07-01` is that
    package's `2023-07-01/index.d.ts`; and the bare name a paired @types/*
    package supplies -- `types = ["node"]` is @types/node, which is the only
    place DefinitelyTyped puts it.

    For the third, TypeScript consults the manifest three ways before it reads
    a file, and NpmPackageInfo carries none of the three: a `typesVersions`
    mapping, a package.json inside the subpath's directory, and whether an
    `exports` map exists at all, since one that omits the subpath stops tsc.
    This reads the shipped files alone, in tsc's order
    (`_shipped_subpath_candidates`). Where a manifest maps the subpath in
    `typesVersions` the two part: web-streams-polyfill rewrites `dist/types/*`
    to `dist/types/ts3.6/*`, so `web-streams-polyfill/dist/types/polyfill` is
    `dist/types/ts3.6/polyfill.d.ts` to tsc and `dist/types/polyfill.d.ts`
    here. The unit test pins that answer.
    """
    ref = types_entry_package_ref(entry)
    if not ref:
        return None
    name = npm_info.package_name
    if ref == name:
        return npm_info.exports_types_file or npm_info.ambient_types_file
    if ref.startswith(name + "/"):
        subpath = ref[len(name):]
        return npm_info.subpath_types.get("." + subpath) or _shipped_subpath_file(subpath[1:], npm_info)
    if types_package_alias(name) == ref:
        return npm_info.ambient_types_file
    return None

def _shipped_subpath_candidates(sub, npm_info):
    """Every file `pkg/<sub>` may resolve to among the package's own, in TypeScript's order.

    typeRoots are read before node_modules is walked, so `<sub>/index.d.ts` under
    the paired @types package outranks everything the package itself ships, and
    that package's `<sub>.d.ts` comes last. `shown` is the path as the package
    publishes it, for the message an unresolved entry fails with.
    """
    name, own = npm_info.package_name, npm_info.package_root
    file, index = "/" + sub + ".d.ts", "/" + sub + "/index.d.ts"
    shipped = [(own + file, name + file), (own + index, name + index)]
    if npm_info.types_package_dir:
        types, types_name = npm_info.types_package_dir.dirname, types_package_name(name)
        shipped = [(types + index, types_name + index)] + shipped + [(types + file, types_name + file)]
    return [struct(path = path, shown = shown) for path, shown in shipped]

def _shipped_subpath_file(sub, npm_info):
    if not sub:
        return None
    ranked = {c.path: rank for rank, c in enumerate(_shipped_subpath_candidates(sub, npm_info))}
    best = None

    # One package's own declarations, and only for a subpath its manifest left
    # unnamed: a depset answers "which file sits at this path" no other way.
    for f in npm_info.declaration_files.to_list():
        rank = ranked.get(f.path)
        if rank != None and (best == None or rank < best[0]):
            best = (rank, f)
    return best[1] if best else None

def _requested_type_files(ctx, npm_info):
    out = []
    for entry in _requested_types(ctx):
        designated = types_entry_file(entry, npm_info)
        if designated:
            out.append(designated)
    return out

def _directive_answer(name, deps, untyped):
    """The dep of a package that answers its `/// <reference types="name" />`, and the file.

    TypeScript's order: `@types/<name>` under typeRoots first, a package called
    `name` beside it second -- both against the referencing package's own
    dependencies, which is where npm installs them.
    """
    typed = [dep for dep in deps if dep.package_name.startswith("@types/")]
    for dep in typed + [dep for dep in deps if not dep.package_name.startswith("@types/")]:
        if dep.package_name in untyped:
            continue
        designated = types_entry_file(name, dep)
        if designated:
            return dep, designated
    return None, None

# A worklist bound Starlark's for-loop needs, not a size any chain approaches.
_MAX_REFERENCED_DECLARATIONS = 1024

def referenced_type_files(entry, npm_info, untyped = {}):
    """`entry` and every declaration its `/// <reference types=...>` directives reach.

    A `@types/*` entry in `files` brings its own declarations; what it
    references arrives only if something resolves the directive, and tsgo
    cannot -- the resolver walks typeRoots and node_modules, never `paths`. So
    each name the package recorded for the file is answered from that package's
    own deps, and the answer's directives are followed in turn: @types/bun is
    one line forwarding to bun-types, whose entry references `node`. Items are
    structs of `file` and the `package` (NpmPackageInfo) it belongs to, `entry`
    first. A package named in `untyped` answers nothing.
    """
    out = [struct(file = entry, package = npm_info)]
    seen = {entry.path: True}
    for i in range(_MAX_REFERENCED_DECLARATIONS):
        if i >= len(out):
            return out
        item = out[i]
        for name in item.package.type_references.get(item.file.path, []):
            dep, designated = _directive_answer(name, item.package.direct_deps, untyped)
            if designated and designated.path not in seen:
                seen[designated.path] = True
                out.append(struct(file = designated, package = dep))
    fail("ts_compile: more than {} declarations reached through /// <reference types> directives from {}".format(
        _MAX_REFERENCED_DECLARATIONS,
        entry.path,
    ))

def _bare_package_name(entry):
    """The package `entry` names, without its subpath. Mirrors Gazelle's barePackageName."""
    if entry.startswith("@"):
        segments = entry[1:].split("/")
        if len(segments) >= 2:
            return "@" + segments[0] + "/" + segments[1]
        return entry
    return entry.split("/")[0]

# tsc and tsgo 7.0.2 report TS2688 for such an entry at action time, naming no
# dep; a fail() here names it, and unlike a print() survives a cache hit.
#
# A `typeRoots` of the target's own is the one case this cannot judge. It names
# a directory tsgo scans at action time, holding declarations the rule never
# sees, so an entry there may well resolve and unresolvability cannot be shown.
# Setting it is therefore also the escape hatch for an entry that resolves by
# some route only the compiler can see.
def _fail_on_unresolved_types(ctx, npm_infos):
    requested = _requested_types(ctx)
    if not requested or _requested_option_list(ctx, "typeRoots"):
        return
    for entry in requested:
        ref = types_entry_package_ref(entry)
        if not ref or _types_entry_resolves(ref, npm_infos):
            continue
        fail(
            "ts_compile: compilerOptions.types entry \"{entry}\" on {label} resolves to nothing.\n".format(
                entry = ref,
                label = ctx.label,
            ) +
            _unresolved_type_reason(ref, npm_infos) +
            "The declarations the entry names would never join this target's program.\n" +
            _unresolved_type_fix(ref, npm_infos),
        )

def _dep_answered_types(ctx, npm_infos):
    """The `types` entries a dep answers, which the generated config must not repeat.

    Their declarations reach the compiler through `files`; TypeScript resolves
    the entry itself through typeRoots and node_modules, neither of which the
    sandbox has, and reports TS2688 for it -- tsgo 7.0.2 included.
    """
    answered = {}
    for entry in _requested_types(ctx):
        ref = types_entry_package_ref(entry)
        if ref and _types_entry_resolves(ref, npm_infos):
            answered[entry] = True
    return answered

# Keyed by short_path, so a generated file answers by the same workspace-relative
# path a checked-in one does; _generate_tsconfig writes the entry to where it is.
def _declaration_type_files(ctx, dep_declarations):
    """The file each relative `types` entry names, keyed by the entry.

    An entry nothing stages fails here, naming the label to add; the compiler
    would report TS2688 for it from the action, naming none.
    """
    written = [types_entry_declaration(entry) for entry in _requested_types(ctx)]
    written = [entry for entry in written if entry]
    if not written and not ctx.files.types_srcs:
        return {}

    # Flattened because the answer is which staged file sits at one path, and
    # only for a target that names a declaration file at all.
    staged = {}
    for f in ctx.files.srcs + ctx.files.types_srcs + ctx.files.path_alias_srcs + dep_declarations.to_list():
        staged.setdefault(f.short_path, f)

    out = {}
    for entry in written:
        path = workspace_relative(ctx.label.package, entry)
        found = staged.get(path)
        if found:
            out[entry] = found
            continue
        basename = entry.split("/")[-1]
        near = sorted([p for p in staged if p.endswith("/" + basename)])
        fail(
            "ts_compile: compilerOptions.types entry \"{entry}\" on {label} names \"{path}\",\n".format(
                entry = entry,
                label = ctx.label,
                path = path if path else entry,
            ) +
            "  which no file this target stages sits at.\n" +
            "The type-check action stages srcs, types_srcs, path_alias_srcs and its " +
            "deps' declarations, so the entry would resolve to nothing there.\n" +
            "List the file in types_srcs, which stages it without " +
            "publishing it as this target's own declaration the way a .d.ts in srcs " +
            "is.\n" +
            ("Did you mean \"{}\"?\n".format(near[0]) if near else ""),
        )

    named = {f.path: True for f in out.values()}
    for f in ctx.files.types_srcs:
        if f.path not in named:
            fail(
                "ts_compile: types_srcs on {} names '{}', which no ".format(ctx.label, f.short_path) +
                "compilerOptions.types entry names.\nA file here is staged for an entry to " +
                "resolve and reaches the program no other way, so this one reaches it not at " +
                "all.\nName it -- types = [\"{}\"] from this package -- or drop it.\n".format(
                    explicitly_relative(_relative_path(ctx.label.package, f.dirname) + "/" + f.basename),
                ),
            )
    return out

def _fail_on_untyped_conflict(ctx):
    """A `types` entry naming a package this target also keeps out of the program."""
    untyped = {name: True for name in ctx.attr.untyped_packages}
    if not untyped:
        return
    for entry in _requested_types(ctx):
        ref = types_entry_package_ref(entry)
        package = _bare_package_name(ref) if ref else None
        if not package or package not in untyped:
            continue
        fail(
            "ts_compile: {label} names \"{package}\" in both compilerOptions.types and\n".format(
                label = ctx.label,
                package = package,
            ) +
            "  untyped_packages, which ask for opposite things: `types` puts a package's\n" +
            "  declarations in the program unasked, and untyped_packages keeps them out.\n" +
            "Drop whichever one is wrong. A target that needs those globals is not one\n" +
            "that can leave the package out.\n",
        )

def _untyped_names(ctx, resolved):
    """The `untyped_packages` names, checked against the closure they name.

    An entry matching no package in the closure keeps nothing out, and these are
    npm package names -- which a target does not otherwise spell anywhere -- so
    a typo has nothing else to fail against.
    """
    for name in ctx.attr.untyped_packages:
        if name in resolved:
            continue
        near = sorted([p for p in resolved if name in p or p in name])
        fail(
            "ts_compile: untyped_packages on {} names \"{}\", which no dep of\n".format(
                ctx.label,
                name,
            ) +
            "  this target resolves, so it keeps nothing out of the type program.\n" +
            ("Did you mean one of these: {}?\n".format(", ".join(near)) if near else "Entries are npm package names, spelled as the lockfile spells them.\n"),
        )
    return {name: True for name in ctx.attr.untyped_packages}

def _types_entry_resolves(entry, npm_infos):
    for npm_info in npm_infos:
        if types_entry_file(entry, npm_info):
            return True
    return False

def _unresolved_type_reason(entry, npm_infos):
    package = _bare_package_name(entry)
    for npm_info in npm_infos:
        if npm_info.package_name != package:
            continue
        if package == entry:
            return "\"{}\" is a dep, but its package.json designates no declarations for the package root.\n".format(package)
        sub = entry[len(package) + 1:]
        if not sub:
            return "\"{}\" is a dep, but the entry ends in \"/\" and names no subpath of it.\n".format(package)
        return "\"{package}\" is a dep, but its package.json designates no declarations for the subpath \"./{sub}\", and nothing is shipped at any path the entry is otherwise answered from: {searched}.\n".format(
            package = package,
            sub = sub,
            searched = ", ".join([c.shown for c in _shipped_subpath_candidates(sub, npm_info)]),
        )
    return "No dep of this target publishes \"{}\", and a `types` entry is resolved from this target's own deps -- there is no node_modules for TypeScript to walk.\n".format(package)

def _unresolved_type_fix(entry, npm_infos):
    package = _bare_package_name(entry)
    for npm_info in npm_infos:
        if npm_info.package_name == package:
            designated = sorted(npm_info.subpath_types.keys())
            if designated:
                return "Did you mean one of the subpaths it does designate: {}?\n".format(", ".join(designated))
            return "It designates none, so the declarations have to be named as a file, by a label that stages it: types = [\"./path/to/name.d.ts\"] with that file in types_srcs.\n"
    fix = ("Add the package to deps (e.g. \"@npm//:{}\"), or name a declaration file in this package instead: types = [\"./path/to/name.d.ts\"], with that file in types_srcs.\n".format(package) +
           "If it resolves from a typeRoots directory, set typeRoots in compiler_options: this check cannot read the one in a tsconfig file, and skips a target that states its own.\n")
    near = sorted([
        npm_info.package_name
        for npm_info in npm_infos
        if package in npm_info.package_name or npm_info.package_name in package
    ])
    if near:
        fix += "Did you mean one of these deps: {}?\n".format(", ".join(near))
    return fix

def _requested_types(ctx):
    """The compilerOptions.types this target asked for, or []."""
    return _requested_option_list(ctx, "types")

def _requested_option_list(ctx, key):
    """The list of strings this target set for `key`, or []."""
    if not ctx.attr.compiler_options_json:
        return []
    decoded = json.decode(ctx.attr.compiler_options_json)
    if type(decoded) != "dict":
        return []
    value = decoded.get(key)
    if type(value) != "list":
        return []
    return [v for v in value if type(v) == "string"]

def _write_baseline_tsconfig(ctx):
    """Writes _BASELINE_OPTIONS as a tsconfig for the generated one to extend.

    A file rather than a dict merge because Starlark cannot read the user's
    tsconfig to see which keys it already sets; TypeScript resolves that itself,
    and an `extends` list is the only place a layer can sit *under* the file.

    That same unreadability is why `moduleResolution` is withdrawn here: the
    user's `module` overrides this layer's without the layer ever learning it
    did, and the partner left behind is what tsgo rejects.
    """
    out = ctx.actions.declare_file("{}.tsconfig_baseline.json".format(ctx.label.name))
    opts = _drop_derived_resolution(dict(_BASELINE_OPTIONS), None)
    ctx.actions.write(
        output = out,
        content = json.encode_indent({"compilerOptions": opts}, indent = "  "),
    )
    return out

def _generate_tsconfig(
        ctx,
        srcs,
        npm_pkg_dirs = None,
        npm_subpath_dts = None,
        npm_types_aliases = None,
        ambient_dts = None,
        module_paths = None,
        extends_file = None,
        baseline_file = None,
        allow_js = False,
        emit_declarations = False,
        isolated_declarations = False,
        emit_root_dir = None,
        emit_out_dir = None,
        types_files = None,
        answered_types = None):
    """Generates the tsconfig.json tsgo is invoked with.

    Layered lowest precedence first:

      1. `baseline_file` -- _BASELINE_OPTIONS as a file, extended before the
         user's, so it reaches only the keys the user's chain never mentions,
         minus the one key that depends on which of the two `module` won.
      2. `extends_file` -- the user's own tsconfig.json, referenced (not
         copied), so every relative path inside it still resolves against the
         directory it was written for.
      3. _RULESET_OPTIONS and allowJs, plus _BASELINE_OPTIONS when there is no
         `extends_file` to put them under.
      4. compiler_options_json -- what the user asked for, via the ts_compile
         macro's lib / types / target / jsx / compiler_options arguments.
      5. Bazel-owned options: _BAZEL_OWNED_OPTIONS plus paths and include.

    Layers 3-5 all land in this generated file, so they override the user's
    tsconfig wholesale, per key, exactly as TypeScript's own `extends` does.

    Args:
        ctx:          Rule context.
        srcs:         Source files to type-check (.ts/.tsx/.js/.mjs/.cjs plus
                      ambient .d.ts).
        npm_pkg_dirs: (package_name, path, is_file, package_root) tuples for npm deps.
        npm_subpath_dts: (specifier, declaration path) pairs, one per `exports`
                      subpath an npm dep designates a declaration for.
        npm_types_aliases: struct(key, path, is_file, wildcard) per `paths` key a
                      @types/* dep needs under the bare name it types.
        ambient_dts:  The .d.ts whose declarations are global rather than
                      imported: each @types/* dep's entry point, and each dep
                      target's global-declaration entry. Listed in `files`.
        module_paths: struct(module_name, declaration_root, source_root,
                      declared_paths) from every dep that declared a
                      module_name.
        extends_file: The user's tsconfig.json, or None for zero-config.
        baseline_file: _BASELINE_OPTIONS written out, or None when there is no
                      `extends_file` to layer them under.
        allow_js:     True when a JavaScript src is in `include`.
        emit_declarations: True when tsgo emits the .d.ts (declarations =
                      "tsgo"), False when it only reports diagnostics.
        isolated_declarations: True when oxc emits the .d.ts, whose syntactic
                      emit requires every export to be annotated.
        emit_root_dir: Exec-root-relative common source directory.
        emit_out_dir: Exec-root-relative directory the .d.ts must land in.
        types_files:  The staged file each relative `types` entry resolved to,
                      keyed by the entry as written.
    """
    tsconfig = ctx.actions.declare_file("{}.tsconfig.json".format(ctx.label.name))
    tsconfig_dir = tsconfig.dirname
    package_rel = _relative_path(tsconfig_dir, ctx.label.package)

    opts = {}
    opts.update(_RULESET_OPTIONS)

    # A JavaScript src is listed in `include`; without this tsgo reports TS6504
    # on it rather than reading it.
    if allow_js:
        opts["allowJs"] = True

    user_opts = {}
    if ctx.attr.compiler_options_json:
        decoded = json.decode(ctx.attr.compiler_options_json)
        if type(decoded) != "dict":
            fail("ts_compile: compiler_options must be a dict, got {}.".format(type(decoded)))
        user_opts = decoded

    # With an extends_file the same options arrive through baseline_file, under
    # it rather than over it. Decoded first because here -- unlike there -- the
    # layer that can override the baseline's `module` is readable, so the
    # baseline keeps `moduleResolution` for as long as it still owns the module
    # the value belongs to.
    if not extends_file:
        opts.update(_drop_derived_resolution(dict(_BASELINE_OPTIONS), user_opts))

    # target and jsx come from the attrs in every mode, including over a
    # tsconfig baseline: oxc transforms with them, and the two compilers
    # disagreeing is worse than deferring to the file.
    opts["target"] = ctx.attr.target
    if ctx.attr.jsx_mode:
        opts["jsx"] = ctx.attr.jsx_mode

    # The resolvers with a `module` of their own: withdrawing the baseline's
    # `module` would not help, because the value tsgo derives for an unset
    # `module` is not one of them either. So this pair is named here instead of
    # at line 2 of a generated file. Only without a `tsconfig`, whose `module`
    # could be the matching one.
    resolution = user_opts.get(_DERIVED_FROM_MODULE)
    if not extends_file and "module" not in user_opts and type(resolution) == "string" and resolution.lower() in ("node16", "nodenext"):
        fail(
            "ts_compile: compilerOptions.moduleResolution is \"{}\" and no module is set, so it inherits\n".format(resolution) +
            "the ruleset's module \"{}\" -- a pair TypeScript rejects (TS5110).\n".format(_BASELINE_OPTIONS["module"]) +
            "Set compilerOptions.module to \"{}\" on {}, or name a tsconfig that does.".format(resolution, ctx.label),
        )

    for key, reason in _BAZEL_OWNED_OPTIONS.items():
        if key in user_opts:
            fail(
                "ts_compile: compilerOptions.{key} is set by the rule and cannot be overridden -- {reason}.\n".format(
                    key = key,
                    reason = reason,
                ) +
                "Remove \"{}\" from compiler_options on {}.".format(key, ctx.label),
            )

    for key in _PACKAGE_RELATIVE_OPTIONS:
        if key in user_opts:
            if type(user_opts[key]) != "list":
                fail("ts_compile: compilerOptions.{} must be a list of strings on {}.".format(key, ctx.label))
            user_opts[key] = [
                _rebase_types_entry(entry, package_rel, tsconfig_dir, types_files or {})
                for entry in user_opts[key]
                if key != "types" or entry not in (answered_types or {})
            ]

    opts.update(user_opts)

    # --//ts:lib_check. Over the user's options rather than under them: the
    # question it asks -- does anything in this program's .d.ts closure fail to
    # resolve -- is one no layer of configuration should be able to answer no to.
    if ctx.attr._lib_check[BuildSettingInfo].value:
        opts["skipLibCheck"] = False

    # ── Bazel-owned: module resolution ────────────────────────────────────
    #
    # paths is one key, so it cannot be half-inherited: everything importable
    # has to be represented here.
    paths = {}

    # Source tree first, then the same directory in bazel-bin: the declaration a
    # css_module, asset_library or json_library generates for a file under an
    # alias is written to bazel-bin and never appears beside its source, and
    # `rootDirs` bridges the two trees for a relative specifier and nowhere else.
    for alias_key, alias_dir in ctx.attr.path_aliases.items():
        dir_no_slash = alias_dir[:-1] if alias_dir.endswith("/") else alias_dir
        rel_dirs = [
            explicitly_relative(_relative_path(tsconfig_dir, dir_no_slash)),
            explicitly_relative(_relative_path(
                tsconfig_dir,
                ctx.bin_dir.path + "/" + dir_no_slash,
            )),
        ]
        if alias_key.endswith("/"):
            paths[alias_key + "*"] = [d + "/*" for d in rel_dirs]
            paths[alias_key[:-1]] = rel_dirs
        else:
            paths[alias_key] = rel_dirs
            paths[alias_key + "/*"] = [d + "/*" for d in rel_dirs]

    # A path_alias is the consumer naming a module outright. The @types keys
    # below are the ruleset inferring one, and an inference does not displace it.
    aliased = {key: True for key in paths}

    for entry in npm_pkg_dirs or []:
        pkg_name, path, is_file, pkg_root = entry[0], entry[1], entry[2], entry[3]
        pkg_dir = path[:path.rfind("/")] if "/" in path else ""
        rel_dir = explicitly_relative(_relative_path(tsconfig_dir, pkg_dir if is_file else path))
        if is_file:
            paths[pkg_name] = [rel_dir + "/" + path.split("/")[-1]]
        else:
            paths[pkg_name] = [rel_dir]
        paths[pkg_name + "/*"] = subpath_roots(tsconfig_dir, pkg_root, rel_dir)

    # After the wildcard, which is a guess wherever a manifest gates subpaths.
    # Where the manifest said which declaration a subpath designates, that answer
    # replaces the guess.
    for specifier, path in npm_subpath_dts or []:
        pkg_dir = path[:path.rfind("/")] if "/" in path else ""
        rel_dir = explicitly_relative(_relative_path(tsconfig_dir, pkg_dir))
        paths[specifier] = [rel_dir + "/" + path.split("/")[-1]]

    # The same entries again under the name a @types/* package types, which is
    # the only name anything imports it by -- rollup's own declarations say
    # `from "estree"`, never `from "@types/estree"`. Written over the runtime
    # package's own entry, which the caller only offers this key for once it has
    # established that package publishes no declarations to be shadowed.
    for alias in npm_types_aliases or []:
        if alias.key in aliased:
            continue
        pkg_dir = alias.path[:alias.path.rfind("/")] if "/" in alias.path else ""
        rel_dir = explicitly_relative(_relative_path(tsconfig_dir, pkg_dir if alias.is_file else alias.path))
        if alias.is_file:
            paths[alias.key] = [rel_dir + "/" + alias.path.split("/")[-1]]
        else:
            paths[alias.key] = [rel_dir]

        # The wildcard is written over the runtime package's too, and yields to
        # the same thing the bare key yields to. Leaving that entry standing
        # pointed `@babel/core/*` at +npm+npm__babel_core__7_29_0, whose whole
        # tree holds no .d.ts, while `@babel/core` resolved through
        # @types/babel__core beside it -- and tsconfig_aspect, which drops a
        # declaration-free package instead of naming it, disagreed with both.
        if alias.wildcard and alias.key + "/*" not in aliased:
            paths[alias.key + "/*"] = subpath_roots(tsconfig_dir, alias.root, rel_dir)

    # Last, so a first-party module_name wins over a same-named npm package.
    # Both roots are listed because a module's declarations are either generated
    # or passed through from srcs; TypeScript tries each entry in turn, and the
    # generated root goes first because it is what this build produced.
    #
    # What the module's own package.json designates goes ahead of the guesses
    # that used to be the whole answer, and the guesses stay: a manifest naming a
    # file this build does not produce is then no worse than a manifest nobody
    # read. That is also why a declared subpath repeats the wildcard expansion --
    # an exact `paths` key beats a pattern one, so `<name>/*` stops being
    # consulted for a subpath the moment it is named.
    for module in module_paths or []:
        roots = [
            explicitly_relative(_relative_path(tsconfig_dir, module.declaration_root)),
            explicitly_relative(_relative_path(tsconfig_dir, module.source_root)),
        ]
        declared = {d.specifier: d.declarations for d in module.declared_paths}
        paths[module.module_name] = (
            _rebased_declarations(roots, declared.get("", ())) +
            [r + "/index.d.ts" for r in roots] + roots
        )
        paths[module.module_name + "/*"] = (
            _rebased_declarations(roots, declared.get("/*", ())) +
            [r + "/*" for r in roots]
        )
        for specifier in sorted(declared):
            if specifier == "" or specifier == "/*":
                continue
            paths[module.module_name + specifier] = (
                _rebased_declarations(roots, declared[specifier]) +
                [r + specifier for r in roots]
            )

    if paths:
        opts["paths"] = {
            key: [explicitly_relative(value) for value in values]
            for key, values in paths.items()
        }

    opts["rootDirs"] = [
        _relative_path(tsconfig_dir, ""),
        _relative_path(tsconfig_dir, ctx.bin_dir.path),
    ]

    # tsgo resolves a `types` entry to its realpath and that file's imports from
    # there: the source tree, where undeclared files sit.
    opts["preserveSymlinks"] = True

    # ── Bazel-owned: emit shape ───────────────────────────────────────────
    opts["declaration"] = True
    opts["emitDeclarationOnly"] = True
    opts["declarationMap"] = ctx.attr.declaration_map
    opts["composite"] = False
    opts["incremental"] = False

    if emit_declarations:
        # noEmit off because a tsconfig that sets it -- the usual shape for a
        # bundler-built package -- would starve the action of its declared
        # outputs; noEmitOnError so a target that fails to check leaves no
        # declaration behind for a consumer to read.
        opts["noEmit"] = False
        opts["noEmitOnError"] = True

        # Compared against None, not tested for truth: "" is the exec root, the
        # source root of a target in the top-level package, and not "unset".
        opts["outDir"] = _relative_path(tsconfig_dir, emit_out_dir) if emit_out_dir != None else "."
        opts["declarationDir"] = opts["outDir"]
        opts["rootDir"] = _relative_path(tsconfig_dir, emit_root_dir) if emit_root_dir != None else "."
    else:
        # Every input is under the exec root. tsgo 7.0.2 checks the program against
        # rootDir under --noEmit too (TS6059), inferring the config's own directory.
        opts["rootDir"] = _relative_path(tsconfig_dir, "")
    if isolated_declarations:
        opts["isolatedDeclarations"] = True

    # ── Bazel-owned: the file set ─────────────────────────────────────────

    include = []
    for src in srcs:
        include.append(include_entry(tsconfig_dir, src.dirname, src.basename))

    # Globals are declared, never imported, so `include` -- which only ever names
    # this target's own srcs -- would leave every one of them out of the program.
    files = [
        include_entry(tsconfig_dir, f.dirname, f.basename)
        for f in ambient_dts or []
    ]

    config = {}
    if extends_file:
        extends_dir = explicitly_relative(_relative_path(tsconfig_dir, extends_file.dirname))
        chain = [extends_dir + "/" + extends_file.basename]

        # A list, and the ruleset's baseline first: a later entry overrides an
        # earlier one, so the user's file and its own extends chain win every key
        # they mention and the baseline reaches only the rest.
        if baseline_file:
            chain.insert(0, "./" + baseline_file.basename)
        config["extends"] = chain if len(chain) > 1 else chain[0]
    config["compilerOptions"] = opts
    config["include"] = include

    if extends_file:
        # srcs and the ambient packages are the only file list. Emptying the
        # inherited ones is safe only alongside `extends`: TS18002 rejects an
        # empty `files` otherwise.
        config["files"] = files
        config["exclude"] = []
        config["references"] = []
    elif files:
        config["files"] = files

    ctx.actions.write(output = tsconfig, content = json.encode_indent(config, indent = "  "))
    return tsconfig

# ─── Undeclared imports ──────────────────────────────────────────────────────
#
# An import has to be satisfied by a DIRECT dep. Inputs stay transitive and
# resolution becomes direct, and the split has to happen here rather than in the
# tsconfig: one `paths` map serves the whole program, so dropping the transitive
# entries would also stop a declared dep's own .d.ts from resolving ITS imports,
# which widens those types to `any` instead of reporting anything.
#
# So the check reads the target's own sources and asks, per specifier, whether a
# direct dep provides it. Only Bazel can answer that, and only Bazel knows the
# label to name in the answer, which is what the compiler's own "cannot find
# module" cannot tell anyone.
#
# The action's inputs are the target's own srcs plus a manifest of what the deps
# provide, so it never waits on an upstream compile.

# Embedded rather than a checked-in .mjs so that the manifest format and its one
# reader stay in the same file. Escape sequences are doubled: this is a Starlark
# string literal.
_STRICT_DEPS_MJS = """\
import { readFileSync, writeFileSync } from "node:fs";
import { builtinModules } from "node:module";

const MODULE_EXTENSIONS = [
  ".d.ts", ".d.mts", ".d.cts",
  ".tsx", ".ts", ".mts", ".cts",
  ".jsx", ".js", ".mjs", ".cjs",
];

const cfg = {
  label: "",
  bin: "",
  aliases: [],
  scan: [],
  own: new Set(),
  direct: new Set(),
  transitive: new Map(),
  provided: [],
  transitiveDirs: new Map(),
  npmDirect: new Set(),
  npmTransitive: new Map(),
  moduleDirect: [],
  moduleTransitive: new Map(),
};

const builtins = new Set(builtinModules);

function stripBin(p) {
  return cfg.bin && p.startsWith(cfg.bin + "/") ? p.slice(cfg.bin.length + 1) : p;
}

function stripExtension(p) {
  for (const ext of MODULE_EXTENSIONS) {
    if (p.endsWith(ext)) return p.slice(0, -ext.length);
  }
  return p;
}

function keysOf(execPath) {
  const raw = stripBin(execPath);
  const bare = stripExtension(raw);
  return raw === bare ? [raw] : [raw, bare];
}

function normalize(p) {
  const out = [];
  for (const part of p.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === ".." && out.length > 0 && out[out.length - 1] !== "..") {
      out.pop();
      continue;
    }
    out.push(part);
  }
  return out.join("/");
}

function packageOf(specifier) {
  if (specifier.startsWith("@")) {
    const parts = specifier.slice(1).split("/");
    return parts.length >= 2 ? "@" + parts[0] + "/" + parts[1] : specifier;
  }
  return specifier.split("/")[0];
}

function underPrefix(specifier, prefix) {
  if (specifier === prefix) return true;
  return specifier.startsWith(prefix.endsWith("/") ? prefix : prefix + "/");
}

// ── The specifier scanner ────────────────────────────────────────────────────
//
// A character walk rather than a regex: a quoted string is an import specifier
// only when the tokens before it say so, which is what keeps `{ from: "x" }`
// and `declare module "x"` out of the results.
//
// Gazelle's ScanImports (gazelle/imports.go) is this same walk: a specifier
// only one of them sees is either a dep Gazelle cannot generate or drift the
// build never notices, so //tests/strict_deps pins the two against one table.
const KEYWORDS_BEFORE_REGEX = new Set([
  "return", "typeof", "instanceof", "in", "of", "new", "delete", "void",
  "do", "else", "yield", "await", "case", "throw",
]);

const CLOSERS = ")]";

function specifiersIn(source) {
  const found = [];
  let line = 1;
  let lastWord = "";
  let lastKind = "";
  let lastPunct = "";
  const i = { at: 0 };

  const isWordChar = (c) => /[A-Za-z0-9_$]/.test(c);

  while (i.at < source.length) {
    const c = source[i.at];

    if (c === "\\n") {
      line += 1;
      i.at += 1;
      continue;
    }
    if (c === " " || c === "\\t" || c === "\\r") {
      i.at += 1;
      continue;
    }
    if (c === "/" && source[i.at + 1] === "/") {
      while (i.at < source.length && source[i.at] !== "\\n") i.at += 1;
      continue;
    }
    if (c === "/" && source[i.at + 1] === "*") {
      i.at += 2;
      while (i.at < source.length && !(source[i.at] === "*" && source[i.at + 1] === "/")) {
        if (source[i.at] === "\\n") line += 1;
        i.at += 1;
      }
      i.at += 2;
      continue;
    }
    if (c === "/" && ((lastKind === "punct" && !CLOSERS.includes(lastPunct)) || (lastKind === "word" && KEYWORDS_BEFORE_REGEX.has(lastWord)))) {
      // A regex literal. Its body can hold quotes and slashes, so it has to be
      // skipped whole rather than tokenized.
      i.at += 1;
      let inClass = false;
      while (i.at < source.length) {
        const r = source[i.at];
        if (r === "\\\\") { i.at += 2; continue; }
        if (r === "\\n") break;
        if (r === "[") inClass = true;
        else if (r === "]") inClass = false;
        else if (r === "/" && !inClass) { i.at += 1; break; }
        i.at += 1;
      }
      lastKind = "punct";
      continue;
    }
    if (c === "`") {
      i.at += 1;
      while (i.at < source.length && source[i.at] !== "`") {
        if (source[i.at] === "\\\\") { i.at += 2; continue; }
        if (source[i.at] === "\\n") line += 1;
        i.at += 1;
      }
      i.at += 1;
      lastKind = "string";
      continue;
    }
    if (c === '"' || c === "'") {
      const startLine = line;
      const quote = c;
      let value = "";
      i.at += 1;
      while (i.at < source.length && source[i.at] !== quote) {
        if (source[i.at] === "\\\\") {
          value += source[i.at + 1] ?? "";
          i.at += 2;
          continue;
        }
        if (source[i.at] === "\\n") break;
        value += source[i.at];
        i.at += 1;
      }
      i.at += 1;
      const isSpecifier =
        (lastKind === "word" && (lastWord === "from" || lastWord === "import")) ||
        (lastKind === "call" && (lastWord === "import" || lastWord === "require"));
      if (isSpecifier && value !== "") found.push({ specifier: value, line: startLine });
      lastKind = "string";
      continue;
    }
    if (isWordChar(c)) {
      let word = "";
      while (i.at < source.length && isWordChar(source[i.at])) {
        word += source[i.at];
        i.at += 1;
      }
      lastWord = word;
      lastKind = "word";
      continue;
    }
    if (c === "(" && lastKind === "word") {
      lastKind = "call";
      i.at += 1;
      continue;
    }
    lastPunct = c;
    lastKind = "punct";
    i.at += 1;
  }

  return found;
}

// ── Manifest ─────────────────────────────────────────────────────────────────

const [stampPath, ...rest] = process.argv.slice(2);
const manifest = [];
for (const arg of rest) {
  if (arg.startsWith("@")) manifest.push(...readFileSync(arg.slice(1), "utf8").split("\\n"));
  else manifest.push(arg);
}

for (const entry of manifest) {
  if (entry === "") continue;
  const field = entry.split("\\t");
  switch (field[0]) {
    case "label": cfg.label = field[1]; break;
    case "bin": cfg.bin = field[1]; break;
    case "alias": cfg.aliases.push(field[1]); break;
    case "scan": cfg.scan.push(field[1]); break;
    case "own": for (const k of keysOf(field[1])) cfg.own.add(k); break;
    case "direct": for (const k of keysOf(field[1])) cfg.direct.add(k); break;
    case "own-dir": case "direct-dir": cfg.provided.push(stripBin(field[1])); break;
    case "transitive-dir":
      if (!cfg.transitiveDirs.has(stripBin(field[1]))) {
        cfg.transitiveDirs.set(stripBin(field[1]), field[2]);
      }
      break;
    case "transitive":
      for (const k of keysOf(field[1])) {
        if (!cfg.transitive.has(k)) cfg.transitive.set(k, field[2]);
      }
      break;
    case "npm-direct": cfg.npmDirect.add(field[1]); break;
    case "npm-transitive": cfg.npmTransitive.set(field[1], field[2]); break;
    case "module-direct": cfg.moduleDirect.push(field[1]); break;
    case "module-transitive": cfg.moduleTransitive.set(field[1], field[2]); break;
  }
}

// ── Classification ───────────────────────────────────────────────────────────

function undeclared(specifier, file) {
  // The fragment starts at the FIRST # only when something precedes it: a
  // leading one makes the whole specifier a package-imports name.
  const query = specifier.split("?")[0];
  const hash = query.indexOf("#", 1);
  const clean = hash > 0 ? query.slice(0, hash) : query;
  if (clean === "") return null;

  if (clean.startsWith("./") || clean.startsWith("../")) {
    const dir = stripBin(file).split("/").slice(0, -1).join("/");
    const resolved = normalize(dir + "/" + clean);
    const candidates = [resolved, stripExtension(resolved)];
    for (const c of [...candidates]) candidates.push(c + "/index");
    for (const c of candidates) {
      if (cfg.own.has(c) || cfg.direct.has(c)) return null;
    }
    // A directory answers for everything under it: whether the file is really
    // there is the compiler's question, not this one's.
    for (const dir of cfg.provided) {
      if (underPrefix(resolved, dir)) return null;
    }
    for (const c of candidates) {
      if (cfg.transitive.has(c)) return cfg.transitive.get(c);
    }
    for (const [dir, label] of cfg.transitiveDirs) {
      if (underPrefix(resolved, dir)) return label;
    }
    return null;
  }

  if (clean.startsWith("node:")) return null;
  for (const alias of cfg.aliases) {
    if (underPrefix(clean, alias)) return null;
  }
  const pkg = packageOf(clean);
  if (builtins.has(pkg) || cfg.npmDirect.has(pkg)) return null;
  for (const name of cfg.moduleDirect) {
    if (underPrefix(clean, name)) return null;
  }
  if (cfg.npmTransitive.has(pkg)) return cfg.npmTransitive.get(pkg);
  for (const [name, label] of cfg.moduleTransitive) {
    if (underPrefix(clean, name)) return label;
  }
  return null;
}

const findings = [];
for (const file of cfg.scan) {
  let source;
  try {
    source = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  for (const { specifier, line } of specifiersIn(source)) {
    const label = undeclared(specifier, file);
    if (label !== null) findings.push({ file: stripBin(file), line, specifier, label });
  }
}

if (findings.length === 0) {
  writeFileSync(stampPath, "");
  process.exit(0);
}

const width = Math.max(...findings.map((f) => `${f.file}:${f.line}`.length));
const lines = [
  `${cfg.label} imports ${findings.length === 1 ? "a module" : "modules"} no direct dep provides:`,
  "",
];
for (const f of findings) {
  lines.push(`  ${`${f.file}:${f.line}`.padEnd(width)}  imports ${JSON.stringify(f.specifier)}`);
  lines.push(`  ${" ".repeat(width)}  add ${JSON.stringify(f.label)} to deps`);
}
lines.push(
  "",
  "Each of those resolves today only because it reaches this target through",
  "another dep's own deps, and stops resolving the moment that dep drops it.",
  "Re-run gazelle to regenerate deps, or add the labels above by hand.",
);
process.stderr.write(lines.join("\\n") + "\\n");
process.exit(1);
"""

def label_text(label):
    """The label as a deps list writes it: the main repo's canonical @@ dropped."""
    text = str(label)
    return text[2:] if text.startswith("@@//") else text

def _npm_hub_entry(npm_info):
    """The hub label a deps list writes for an npm package in the closure.

    The closure carries NpmPackageInfo, not labels: a transitive package was
    never named in any deps list here. Its own repository is
    `<hub>__<package>__<version>...`, so the hub the extension created -- which
    is what a deps list names -- is recoverable from the file it provides.
    """
    name = npm_info.package_name
    label_name = name[1:].replace("/", "_") if name.startswith("@") else name
    hub = "npm"
    owner = npm_info.package_dir.owner if npm_info.package_dir else None
    if owner and owner.repo_name:
        candidate = owner.repo_name.split("__")[0].split("+")[-1]
        if candidate:
            hub = candidate
    return struct(name = name, label = "@{}//:{}".format(hub, label_name))

# A directory travels as its own path, and expansion is off wherever these are
# added: the check's inputs are the target's own srcs, so a dep's tree is one it
# holds no input for and Bazel fails the action rather than expanding it.
def _own_manifest_entry(f):
    kind = "own-dir" if f.is_directory else "own"
    return "{}\t{}".format(kind, f.path)

def _direct_manifest_entry(f):
    kind = "direct-dir" if f.is_directory else "direct"
    return "{}\t{}".format(kind, f.path)

def _transitive_manifest_entry(f):
    owner = f.owner

    # An external-repo file is an npm package's, and no relative specifier in a
    # first-party source reaches one; the npm entries below carry those by name.
    if not owner or owner.repo_name:
        return None
    kind = "transitive-dir" if f.is_directory else "transitive"
    return "{}\t{}\t{}".format(kind, f.path, label_text(owner))

def _strict_deps_check(
        ctx,
        scan_srcs,
        own_files,
        direct_provided,
        transitive_provided,
        npm_direct,
        npm_reachable,
        module_direct,
        module_reachable):
    """Registers the action that fails on an import no direct dep provides.

    Args:
        ctx:                 Rule context.
        scan_srcs:           This target's own sources, the files to read.
        own_files:           Files this target already stages: srcs and
                             path_alias_srcs.
        direct_provided:     depset of File: what the direct deps produce.
        transitive_provided: depset of File: the whole closure, for the label
                             an undeclared import has to be attributed to.
        npm_direct:          npm package names of the direct deps.
        npm_reachable:       struct(name, label) per npm package in the closure.
        module_direct:       module_name of each direct dep that set one.
        module_reachable:    struct(module_name, label, ...) for the closure.

    Returns:
        struct(stamp, checker): the stamp the compile actions take as an input,
        and the checker that wrote it.
    """
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "ts_compile: checking {} for undeclared imports needs a JS tool ".format(ctx.label) +
            "toolchain, and none is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    checker = ctx.actions.declare_file("{}.strictdeps.mjs".format(ctx.label.name))
    ctx.actions.write(output = checker, content = _STRICT_DEPS_MJS)
    stamp = ctx.actions.declare_file("{}.strictdeps".format(ctx.label.name))

    # A params file, so the closure is expanded when the action runs rather than
    # materialised at analysis time.
    manifest = ctx.actions.args()
    manifest.use_param_file("@%s", use_always = True)
    manifest.set_param_file_format("multiline")

    # The scalars first: the reader keys paths off bin_dir as it parses.
    manifest.add("label\t" + label_text(ctx.label))
    manifest.add("bin\t" + ctx.bin_dir.path)
    for alias in ctx.attr.path_aliases:
        manifest.add("alias\t" + alias)
    for name in npm_direct:
        manifest.add("npm-direct\t" + name)
    for pkg in npm_reachable:
        if pkg.name not in npm_direct:
            manifest.add("npm-transitive\t{}\t{}".format(pkg.name, pkg.label))
    for name in module_direct:
        manifest.add("module-direct\t" + name)
    for module in module_reachable:
        if module.module_name:
            manifest.add("module-transitive\t{}\t{}".format(module.module_name, module.label))

    manifest.add_all(scan_srcs, format_each = "scan\t%s")
    manifest.add_all(own_files, map_each = _own_manifest_entry, expand_directories = False)
    manifest.add_all(direct_provided, map_each = _direct_manifest_entry, expand_directories = False)
    manifest.add_all(transitive_provided, map_each = _transitive_manifest_entry, expand_directories = False)

    ctx.actions.run(
        inputs = depset(scan_srcs + [checker]),
        outputs = [stamp],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [checker.path, stamp.path, manifest],
        mnemonic = "TsStrictDeps",
        progress_message = "TsStrictDeps %{label}",
    )
    return struct(stamp = stamp, checker = checker)

# ─── Global declarations ─────────────────────────────────────────────────────
#
# A .d.ts with no top-level import or export declares globals, and a global
# belongs to every program the file is part of. `include` names only a target's
# own srcs, so a dep's global .d.ts has no route into a consumer's program;
# `files` is that route, the one an @types/* package's globals already take.
#
# Which srcs are global cannot be decided here, because Starlark cannot read a
# file. So an action decides, and writes its answer as a generated .d.ts of
# references to the global ones: a file whose contents are known only after it
# runs, but whose path a consumer's `files` can name at analysis time.
#
# That a file declares globals is a fact about TypeScript; that its globals are
# part of the package's public type surface is a decision about packaging, and
# `public_globals` is where a target makes it. Only a src named there is
# referenced from the entry. Every other src stays in this target's own program
# unchanged and has no route into a consumer's, which is the default because a
# leaked ambient is silent in the consumer that it breaks.

_GLOBAL_DTS_MJS = """\
import { readFileSync, writeFileSync } from "node:fs";

function* tokens(source) {
  const isWordChar = (c) => /[A-Za-z0-9_$]/.test(c);
  let i = 0;
  while (i < source.length) {
    const c = source[i];
    if (c === "/" && source[i + 1] === "/") {
      while (i < source.length && source[i] !== "\\n") i += 1;
      continue;
    }
    if (c === "/" && source[i + 1] === "*") {
      i += 2;
      while (i < source.length && !(source[i] === "*" && source[i + 1] === "/")) i += 1;
      i += 2;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      i += 1;
      while (i < source.length && source[i] !== c) i += source[i] === "\\\\" ? 2 : 1;
      i += 1;
      yield { kind: "string" };
      continue;
    }
    if (isWordChar(c)) {
      let word = "";
      while (i < source.length && isWordChar(source[i])) {
        word += source[i];
        i += 1;
      }
      yield { kind: "word", value: word };
      continue;
    }
    i += 1;
    if (c !== " " && c !== "\\t" && c !== "\\r" && c !== "\\n") yield { kind: "punct", value: c };
  }
}

// TypeScript's own test: a top-level import or export declaration makes the
// file a module, and everything it declares is scoped to it.
function isModule(source) {
  let depth = 0;
  let pending = "";
  let prev = null;
  for (const token of tokens(source)) {
    // `import(...)` is a type query, `import.meta` an expression, and
    // `export as namespace X` declares a UMD global rather than an export.
    if (pending === "import" && !(token.kind === "punct" && (token.value === "(" || token.value === "."))) return true;
    if (pending === "export" && !(token.kind === "word" && token.value === "as")) return true;
    pending = "";
    if (depth === 0 &&
        token.kind === "word" &&
        (token.value === "import" || token.value === "export") &&
        !(prev && prev.kind === "punct" && prev.value === ".")) {
      pending = token.value;
    }
    if (token.kind === "punct") {
      if (token.value === "{" || token.value === "(" || token.value === "[") depth += 1;
      if (token.value === "}" || token.value === ")" || token.value === "]") depth -= 1;
    }
    prev = token;
  }
  return pending !== "";
}

const [entryPath, ...rest] = process.argv.slice(2);
const manifest = [];
for (const arg of rest) {
  if (arg.startsWith("@")) manifest.push(...readFileSync(arg.slice(1), "utf8").split("\\n"));
  else manifest.push(arg);
}

const references = [];
const notGlobal = [];
for (const entry of manifest) {
  if (entry === "") continue;
  const field = entry.split("\\t");
  const exported = field[2] === "public";
  if (isModule(readFileSync(field[0], "utf8"))) {
    if (exported) notGlobal.push(field[0]);
    continue;
  }
  if (exported) references.push('/// <reference path="' + field[1] + '" />');
}
if (notGlobal.length > 0) {
  process.stderr.write(
    "ts_compile: public_globals names " + notGlobal.join(", ") + ", and a top-level " +
      "import or export makes a .d.ts a module.\\nA module has no globals to export -- its " +
      "declarations are scoped to it -- so exporting them is a claim about this file that " +
      "is not true.\\nDrop the entry from public_globals, or drop the import/export that " +
      "makes the file a module; a consumer reaches a module's declarations by importing " +
      "it.\\n",
  );
  process.exit(1);
}
writeFileSync(entryPath, references.join("\\n") + "\\n");
"""

def _global_dts_entry(ctx, dts_srcs, claims):
    """Registers the action that writes this target's global-declaration entry.

    Args:
        ctx:      Rule context.
        dts_srcs: The .d.ts files in srcs, module-scoped ones included.
        claims:   File.path -> "public", from _global_claims. A src it names is
                  referenced from the entry, once the scan confirms it is global.

    Returns:
        File: a generated .d.ts referencing the exported global ones among them.
    """
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "ts_compile: telling a global .d.ts in {} from a module-scoped one ".format(ctx.label) +
            "needs a JS tool toolchain, and none is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    scanner = ctx.actions.declare_file("{}.globals.mjs".format(ctx.label.name))
    ctx.actions.write(output = scanner, content = _GLOBAL_DTS_MJS)
    entry = ctx.actions.declare_file("{}.globals.d.ts".format(ctx.label.name))

    manifest = ctx.actions.args()
    manifest.use_param_file("@%s", use_always = True)
    manifest.set_param_file_format("multiline")
    for f in dts_srcs:
        manifest.add("{}\t{}\t{}".format(
            f.path,
            _relative_path(entry.dirname, f.dirname) + "/" + f.basename,
            claims.get(f.path, ""),
        ))

    ctx.actions.run(
        inputs = depset(dts_srcs + [scanner]),
        outputs = [entry],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [scanner.path, entry.path, manifest],
        mnemonic = "TsGlobalDts",
        progress_message = "TsGlobalDts %{label}",
    )
    return entry

# ─── Attribute validation ────────────────────────────────────────────────────

def _classify_srcs(ctx):
    """Splits srcs into the TypeScript, JavaScript and ambient-declaration sets."""
    compile_srcs = []
    js_srcs = []
    passthrough_dts = []
    for f in ctx.files.srcs:
        if f.is_directory:
            fail(
                "ts_compile: '{}' on {} is a directory.\n".format(f.short_path, ctx.label) +
                "srcs declares one output per file at analysis time, and a directory has " +
                "no file list until its action has run.\nA tree of already-compiled output " +
                "-- ts_codegen(out_dir = ...) -- belongs in deps, where it is staged whole " +
                "and named by module_name.",
            )
        if _is_dts_source(f):
            passthrough_dts.append(f)
        elif f.extension in _TS_EXTENSIONS:
            compile_srcs.append(f)
        elif f.extension in _JS_EXTENSIONS:
            js_srcs.append(f)
        elif f.extension == "jsx":
            fail(
                "ts_compile: '{}' on {} is a .jsx file, which oxc has no ".format(f.short_path, ctx.label) +
                "output extension for.\nRename it to .tsx -- TypeScript accepts the " +
                "JavaScript in it unchanged -- or drop the JSX and call it .js.",
            )
        else:
            fail(
                "ts_compile: srcs must contain only .ts, .tsx, .js, .mjs, .cjs, " +
                ".d.ts, .d.mts or .d.cts files; got '{}' (extension: .{}).\n".format(f.short_path, f.extension) +
                "Remove this file from srcs, or if you need to pass through assets " +
                "use a filegroup or a dedicated rule for that file type.\n" +
                "Did you mean to add it to a different attribute?",
            )
    return compile_srcs, js_srcs, passthrough_dts

def _global_claims(ctx, passthrough_dts):
    """The srcs whose owner says their globals are part of its public surface.

    A src public_globals does not name carries no claim and is private: a
    package's ambient is part of its own build and nothing else.

    Args:
        ctx:             Rule context.
        passthrough_dts: The .d.ts files in srcs.

    Returns:
        dict: File.path -> "public", for the srcs public_globals names.
    """
    dts_paths = {f.path: True for f in passthrough_dts}
    claims = {}
    for f in ctx.files.public_globals:
        if f.path not in dts_paths:
            same_name = [d.short_path for d in passthrough_dts if d.basename == f.basename]
            listing = sorted([d.short_path for d in passthrough_dts])
            fail(
                "ts_compile: public_globals on {} names '{}', which is ".format(ctx.label, f.short_path) +
                "not in srcs.\npublic_globals hands a src's globals to every consumer, and a " +
                "file this target does not compile has no globals of its own to hand over.\n" +
                ("Did you mean '{}'?\n".format(same_name[0]) if same_name else "") +
                "The .d.ts in srcs are:\n  " + ("\n  ".join(listing) if listing else "(none)"),
            )
        claims[f.path] = "public"
    return claims

def _validate_tsgo_args(ctx):
    """Rejects a tsgo flag that would move an output or change resolution."""
    for arg in ctx.attr.tsgo_args:
        if arg not in _ALLOWED_TSGO_ARGS:
            fail(
                "ts_compile: tsgo_args on {} contains \"{}\".\n".format(ctx.label, arg) +
                "Only flags that report on the program are allowed:\n  " +
                " ".join(_ALLOWED_TSGO_ARGS) + "\n" +
                "A compilerOption belongs in compiler_options (or in the file `tsconfig` " +
                "points at), where the Bazel-owned-key guard can see it. Anything else " +
                "would move outputs this rule already declared to Bazel.",
            )

def _validate_path_aliases(ctx, srcs):
    """Rejects an alias that points into bazel-out, or at files no action stages.

    An alias is a source-level path, so its target directory has to be staged by
    the same action that resolves it. Only srcs and path_alias_srcs are inputs,
    so an alias covered by neither resolves against whatever the sandbox happens
    to hold -- which is what makes the build order-dependent.
    """
    covered = [f.short_path for f in srcs] + [
        f.short_path
        for f in ctx.files.path_alias_srcs
    ]
    for alias_key, alias_dir in ctx.attr.path_aliases.items():
        if alias_dir.startswith("bazel-out/") or alias_dir.startswith("bazel-bin/"):
            fail(
                "ts_compile: path_aliases[\"{}\"] on {} points into the output tree ({}).\n".format(
                    alias_key,
                    ctx.label,
                    alias_dir,
                ) +
                "That path embeds the build configuration, so it breaks under -c opt or a " +
                "different exec platform.\nTo import another package by bare specifier, set " +
                "module_name on the target that produces its declarations and depend on it.",
            )
        prefix = alias_dir if alias_dir.endswith("/") else alias_dir + "/"
        found = False
        for path in covered:
            if path == alias_dir or path.startswith(prefix):
                found = True
                break
        if not found:
            fail(
                "ts_compile: path_aliases[\"{}\"] on {} points at \"{}\", where none of ".format(
                    alias_key,
                    ctx.label,
                    alias_dir,
                ) +
                "this target's inputs live.\nThe type-check action stages srcs and " +
                "path_alias_srcs and nothing else, so the alias would resolve against " +
                "whatever another action happened to leave in the sandbox.\nList the " +
                "files it resolves to in path_alias_srcs, or set module_name on the " +
                "target that produces them and depend on it instead.",
            )

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_compile_impl(ctx):
    oxc = get_oxc_toolchain(ctx)
    pkg = ctx.label.package

    compile_srcs, js_srcs, passthrough_dts = _classify_srcs(ctx)
    global_claims = _global_claims(ctx, passthrough_dts)
    _validate_tsgo_args(ctx)
    _validate_path_aliases(ctx, compile_srcs + js_srcs + passthrough_dts)
    _fail_on_untyped_conflict(ctx)

    # The packages this target's type program leaves out: no `paths` key, no
    # `files` entry, nothing that loads their declarations. Checked against the
    # closure once it is known, below.
    untyped = {name: True for name in ctx.attr.untyped_packages}

    # Every package name the closure offers, filtered or not, which is what an
    # untyped_packages entry is checked against.
    resolved_npm_names = {}

    # Where an excluded package's files live, so that a `paths` key belonging to
    # some other package cannot be pointed into one of them. `ms` ships no
    # declarations and resolves to @types/ms; excluding @types/ms has to stop
    # that redirection, or the key still reaches the declarations by another
    # name. Identity rather than a derived `@types/x` spelling, since the
    # pairing this consults is the one npm recorded.
    untyped_pkg_dirs = {}

    # Collect transitive deps.
    transitive_dts_sets = []
    dep_npm_closure_sets = []
    global_entry_sets = []
    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_css_sets = []
    transitive_css_module_sets = []
    transitive_css_exports_sets = []
    transitive_asset_sets = []

    # What the direct deps produce themselves, which is the set an import has to
    # be satisfied from. The transitive sets above stay the action inputs.
    direct_provided_sets = []

    # npm_pkg_dirs: list of (package_name, package_dir_path) for tsconfig paths.
    # We collect ALL transitive npm deps so that tsgo can resolve bare module
    # specifiers in transitively-imported .d.ts files (e.g. vitest's index.d.ts
    # imports from @vitest/runner which must be in the tsconfig paths).
    #
    # Pass 1: collect ALL transitive package dirs from all direct npm deps.
    # This builds a complete map of pkg_name → dir_path that covers both direct
    # and transitive packages. Materializing transitive_deps is O(transitive npm
    # packages) which is bounded (typically tens to low hundreds of packages).
    #
    # We separate this into two passes:
    #  Pass 1: collect all transitive package infos (name → dir_path).
    #  Pass 2: for @types/* packages, find which runtime package they type-annotate
    #           and override the runtime package's dir with the @types dir.
    # This avoids the bug where transitive @types entries from unrelated deps
    # pollute the mapping (e.g. vitest's transitive @types/estree dep being used
    # for vitest).
    #
    # ambient_dts: the `files` entries -- each direct @types/* dep's entry plus
    # what it reaches through `/// <reference types=...>` -- keyed by path to dedupe.
    ambient_dts = {}

    # transitive_package_dir_sets: depset of package.json Files from all
    # direct npm deps, used as inputs to the tsgo validation action so that
    # moduleResolution:"Bundler" can read exports/types fields.
    transitive_package_dir_sets = []

    # Step 1a: collect ALL package info entries (direct + transitive) into a map.
    # pkg_info_map: pkg_name → NpmPackageInfo (direct deps first, then the
    # transitive ones, first seen winning within each pass).
    pkg_info_map = {}
    direct_npm_names = {}
    direct_npm_infos = []

    # The deps a `types` entry can name: the ones the loop below actually offers
    # it, so the guard after the loop answers about the same set that resolved.
    types_candidates = []

    for dep in ctx.attr.deps:
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            dep_npm_closure_sets.append(dep[TsDeclarationInfo].transitive_npm_packages)
            global_entry_sets.append(dep[TsDeclarationInfo].transitive_global_entry_files)
            direct_provided_sets.append(dep[TsDeclarationInfo].declaration_files)

            # Direct deps only, plus what their entries reference: declaring
            # @types/foo is how a target asks for foo's globals, and a package
            # that merely appears in some dep's closure does not silently put
            # its globals in this target's scope.
            #
            # `files` is the other route into the program, and an excluded
            # package has to leave by both: naming its entry point here would put
            # every global in it in scope with no `paths` key in sight.
            if NpmPackageInfo in dep and dep[NpmPackageInfo].package_name not in untyped:
                npm_info = dep[NpmPackageInfo]
                types_candidates.append(npm_info)

                # An entry naming this package says which of its declarations
                # the target wants; the root joins unasked only when none does.
                entries = _requested_type_files(ctx, npm_info)
                if not entries and npm_info.ambient_types_file:
                    entries = [npm_info.ambient_types_file]
                for entry in entries:
                    for reached in referenced_type_files(entry, npm_info, untyped):
                        ambient_dts[reached.file.path] = reached.file
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_js_map_sets.append(dep[JsInfo].transitive_js_map_files)
            direct_provided_sets.append(dep[JsInfo].js_files)
        if CssInfo in dep:
            transitive_css_sets.append(dep[CssInfo].transitive_css_files)
            direct_provided_sets.append(dep[CssInfo].css_files)
        if CssModuleInfo in dep:
            transitive_css_module_sets.append(dep[CssModuleInfo].transitive_css_files)
            transitive_css_exports_sets.append(dep[CssModuleInfo].transitive_exports_files)
            direct_provided_sets.append(dep[CssModuleInfo].css_files)
        if AssetInfo in dep:
            transitive_asset_sets.append(dep[AssetInfo].transitive_asset_files)
            direct_provided_sets.append(dep[AssetInfo].asset_files)
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]
            direct_npm_names[npm_info.package_name] = True
            direct_npm_infos.append(npm_info)

            # Collect transitive package.json files as a depset (no to_list).
            transitive_package_dir_sets.append(npm_info.transitive_package_dirs)

    _fail_on_unresolved_types(ctx, types_candidates)
    answered_types = _dep_answered_types(ctx, types_candidates)

    # Every direct dep claims its name before any transitive one is offered:
    # `paths` has one key per package name, and a transitive dependent's older
    # copy is not what this target's own imports mean.
    for npm_info in direct_npm_infos:
        if npm_info.package_dir:
            resolved_npm_names[npm_info.package_name] = True
            if npm_info.package_name in untyped:
                untyped_pkg_dirs[npm_info.package_dir.dirname] = True
        if npm_info.package_dir and npm_info.package_name not in pkg_info_map and npm_info.package_name not in untyped:
            pkg_info_map[npm_info.package_name] = npm_info

    # Then every transitive dep, for full coverage in tsconfig paths. Unavoidable
    # to_list: `paths` is a dict written into a file, not a depset.
    #
    # untyped_packages applies here too: the leak this attribute exists for
    # arrives through a dependency's own .d.ts, so a package named only there is
    # the usual one to keep out.
    reached = []
    for npm_info in direct_npm_infos:
        reached.extend(npm_info.transitive_deps.to_list())

    # A ts_compile dep's closure last: its .d.ts import what it declared, and
    # this target's own npm deps claim every name first.
    reached.extend(depset(transitive = dep_npm_closure_sets, order = "postorder").to_list())
    for transitive_info in reached:
        trans_name = transitive_info.package_name
        if transitive_info.package_dir:
            resolved_npm_names[trans_name] = True
            if trans_name in untyped:
                untyped_pkg_dirs[transitive_info.package_dir.dirname] = True
        if trans_name not in pkg_info_map and transitive_info.package_dir and trans_name not in untyped:
            pkg_info_map[trans_name] = transitive_info

    _untyped_names(ctx, resolved_npm_names)

    # Step 1b: build a map from runtime package name → @types package dir.
    # When a package like 'react' has a separate @types/react package, TypeScript
    # must resolve 'react' to the @types/react directory (since react itself ships
    # no .d.ts files).  The pairing shows up as a declaration file outside the
    # runtime package's own directory, which is the @types/* package dir.
    #
    # Read for every package in `paths`, not only the direct deps: an untyped
    # package reached transitively (vitest -> @vitest/expect -> chai) is named in
    # `paths` all the same, and a paths entry pointing at a package that ships no
    # declarations resolves to no types at all.
    types_override = {}  # pkg_name → @types_dir (when a types dep is paired)

    # ships_declarations: pkg_name → True when the package declares anything
    # itself. Step 1d below reads it to decide whether a @types/* package may
    # answer the name it types, and it comes from the list this loop already
    # materializes rather than a second pass over the same depsets.
    ships_declarations = {}
    for pkg_name, npm_info in pkg_info_map.items():
        # One package's own declarations, not a transitive closure.
        own_declarations = npm_info.declaration_files.to_list()
        if own_declarations:
            ships_declarations[pkg_name] = True
        if pkg_name.startswith("@types/"):
            continue  # @types/* packages don't need an override
        runtime_pkg_dir = npm_info.package_dir.dirname

        # The paired @types/* package's own root, not the directory whichever
        # declaration file came first happens to sit in: @types/culori keeps
        # `all/`, `css/` and `fn/` beside its index, so the first file listed
        # named `all/` and every `culori` import resolved inside that one
        # module. The pairing is npm's, so the answer is the package.
        if npm_info.types_package_dir:
            if npm_info.types_package_dir.dirname not in untyped_pkg_dirs:
                types_override[pkg_name] = npm_info.types_package_dir.dirname
            continue
        for dts_file in own_declarations:
            if not dts_file.path.startswith(runtime_pkg_dir):
                if dts_file.dirname not in untyped_pkg_dirs:
                    types_override[pkg_name] = dts_file.dirname
                break

    # Step 1c: build npm_pkg_dirs from pkg_info_map using types_override.
    # npm_pkg_dirs entries: (pkg_name, pkg_dir_or_file_path, is_file, pkg_root)
    #   When is_file is True, pkg_dir_or_file_path points directly to a .d.ts file
    #   (from exports_types_file). This generates a more precise paths entry like:
    #     "pkg": ["path/to/index.d.ts"]
    #   rather than:
    #     "pkg": ["path/to/pkg/dir"]
    npm_pkg_dirs = []
    npm_subpath_dts = []

    # Step 1d: the same entries under the name each @types/* package types,
    # which is the only name anything imports it by. @types/estree has no runtime
    # package at all, so without this it is in the program under no name that
    # resolves -- silently, since the imports that need it are in other packages'
    # .d.ts, where skipLibCheck hides the TS2307 and what they export widens.
    npm_types_aliases = []
    for pkg_name, npm_info in pkg_info_map.items():
        pkg_dir = npm_info.package_dir.dirname

        # npm answers `x` from node_modules/x and reaches node_modules/@types/x
        # only when it finds no declarations there.
        alias = types_package_alias(pkg_name)
        if alias in ships_declarations:
            alias = None

        # untyped_packages names the specifier an import writes, and for a
        # `@types/x` package that is `x`: leaving the alias standing would keep
        # answering the very key the entry asked to have no answer.
        if alias in untyped:
            alias = None

        # Override with @types/* dir when the runtime package has separate types.
        if pkg_name in types_override:
            pkg_dir = types_override[pkg_name]
            entry, is_file = pkg_dir, False
        elif npm_info.exports_types_file:
            # Package has conditional exports with a 'types' entry.
            # Point directly at the .d.ts file for precise resolution.
            entry, is_file = npm_info.exports_types_file.path, True
        else:
            entry, is_file = pkg_dir, False
        npm_pkg_dirs.append((pkg_name, entry, is_file, pkg_dir))
        if alias:
            npm_types_aliases.append(struct(
                key = alias,
                path = entry,
                is_file = is_file,
                root = pkg_dir,
                wildcard = True,
            ))

        for subpath in sorted(npm_info.subpath_types):
            declaration = npm_info.subpath_types[subpath].path
            npm_subpath_dts.append((pkg_name + subpath[1:], declaration))
            if alias:
                npm_types_aliases.append(struct(
                    key = alias + subpath[1:],
                    path = declaration,
                    is_file = True,
                    root = pkg_dir,
                    wildcard = False,
                ))

    dep_dts_depset = depset(transitive = transitive_dts_sets, order = "postorder")
    dep_globals_depset = depset(transitive = global_entry_sets, order = "postorder")

    declaration_types = _declaration_type_files(ctx, dep_dts_depset)

    # module_name deps: every module reachable from here, direct or not, since a
    # bare specifier in a dep's .d.ts has to resolve too.
    module_sets = [
        dep[TsModuleInfo].transitive_modules
        for dep in ctx.attr.deps
        if TsModuleInfo in dep
    ]
    module_paths = depset(transitive = module_sets).to_list()

    # No deps, no closure to arrive through: nothing an import could resolve to
    # that a direct dep does not provide.
    scan_srcs = compile_srcs + js_srcs + passthrough_dts
    strict_deps = None
    if ctx.attr.deps and scan_srcs:
        strict_deps = _strict_deps_check(
            ctx = ctx,
            scan_srcs = scan_srcs,
            own_files = ctx.files.srcs + ctx.files.path_alias_srcs,
            direct_provided = depset(transitive = direct_provided_sets),
            transitive_provided = depset(transitive = (
                transitive_dts_sets + transitive_js_sets + transitive_css_sets +
                transitive_css_module_sets + transitive_asset_sets
            )),
            npm_direct = sorted(direct_npm_names),
            npm_reachable = [
                _npm_hub_entry(pkg_info_map[name])
                for name in sorted(pkg_info_map)
            ],
            module_direct = sorted([
                dep[TsModuleInfo].module_name
                for dep in ctx.attr.deps
                if TsModuleInfo in dep and dep[TsModuleInfo].module_name
            ]),
            module_reachable = module_paths,
        )
    strict_deps_inputs = [strict_deps.stamp] if strict_deps else []
    strict_deps_gated = False

    # The rest of the user's `extends` chain. Starlark cannot read the tsconfig
    # to follow it, so a ts_config target declares it and we make every file in
    # it an action input.
    tsconfig_chain = []
    baseline_file = None
    if ctx.file.tsconfig:
        baseline_file = _write_baseline_tsconfig(ctx)
        tsconfig_chain = [ctx.file.tsconfig, baseline_file]
        if TsConfigInfo in ctx.attr.tsconfig:
            tsconfig_chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()

    # Who emits the .d.ts decides what each action is on the hook for.
    #   "oxc":  oxc emits declarations syntactically, which REQUIRES isolated
    #           declarations; tsgo then only reports diagnostics, so checking
    #           stays off the critical path.
    #   "tsgo": oxc transpiles JS only and tsgo emits declarations from the full
    #           program, so no source annotations are required.
    #
    # enable_check = False under "tsgo" means there is no type program at all,
    # and therefore no declarations: an opt-out of types, not of correctness.
    # That is the right shape for terminal targets -- app entry points, dev
    # servers, bundle inputs -- whose types nothing consumes.
    oxc_emits_dts = ctx.attr.declarations == "oxc"
    tsgo_emits_dts = not oxc_emits_dts and ctx.attr.enable_check
    emits_dts = oxc_emits_dts or tsgo_emits_dts

    if ctx.attr.declaration_map and not tsgo_emits_dts:
        fail(
            "ts_compile: declaration_map on {} needs the tsgo declaration emit.\n".format(ctx.label) +
            "oxc writes declarations syntactically and emits no map for them, and a " +
            "target that emits no declarations has nothing to map.\nSet declarations = " +
            "\"tsgo\" with enable_check = True, or drop declaration_map.",
        )

    # ── Declare outputs ───────────────────────────────────────────────────
    #
    # Every output keeps its package-relative path, so a target may hold a
    # subtree. oxc's --strip-dir-prefix takes a single value, so the sources are
    # grouped by the root their package-relative path hangs off (the package
    # directory for a checked-in file, the bin directory for a generated one)
    # and each group gets its own invocation.
    out_base = "/".join([
        p
        for p in [ctx.bin_dir.path, ctx.label.workspace_root, pkg]
        if p
    ])

    oxc_srcs_by_root = {}
    oxc_outs_by_root = {}
    js_outputs = []
    js_map_outputs = []
    dts_outputs = []
    dts_map_outputs = []

    for src in compile_srcs:
        stem = _package_relative_stem(src, pkg)
        root = _source_root(src, pkg)
        group_outs = oxc_outs_by_root.setdefault(root, [])
        oxc_srcs_by_root.setdefault(root, []).append(src)

        js_out = ctx.actions.declare_file(stem + ".js")
        js_outputs.append(js_out)
        group_outs.append(js_out)
        if ctx.attr.source_map:
            js_map_out = ctx.actions.declare_file(stem + ".js.map")
            js_map_outputs.append(js_map_out)
            group_outs.append(js_map_out)
        if emits_dts:
            dts_out = ctx.actions.declare_file(stem + ".d.ts")
            dts_outputs.append(dts_out)
            if oxc_emits_dts:
                group_outs.append(dts_out)
        if ctx.attr.declaration_map:
            dts_map_outputs.append(ctx.actions.declare_file(stem + ".d.ts.map"))

    # JavaScript needs no transform, so it is staged in the output tree as-is:
    # a relative import of it from compiled TypeScript has to resolve at runtime
    # in the same directory layout.
    # TypeScript keeps the higher-priority extension of a .mjs / .d.mts pair listed
    # together, so the .mjs leaves the program and tsgo writes nothing for it.
    checked_in_dts = {_package_relative_path(d, pkg): True for d in passthrough_dts}
    js_passthrough = []
    for src in js_srcs:
        rel = _package_relative_path(src, pkg)
        staged = ctx.actions.declare_file(rel)
        ctx.actions.symlink(output = staged, target_file = src)
        js_passthrough.append(staged)
        stem = rel[:-(len(src.extension) + 1)]
        dts_rel = stem + _JS_DECLARATION_EXTENSION[src.extension]
        if tsgo_emits_dts and dts_rel not in checked_in_dts:
            dts_outputs.append(ctx.actions.declare_file(dts_rel))
            if ctx.attr.declaration_map:
                dts_map_outputs.append(ctx.actions.declare_file(dts_rel + ".map"))

    # JavaScript srcs whose declarations are all checked in leave tsgo nothing
    # to write: a program to check, not one to emit from.
    tsgo_emits_dts = tsgo_emits_dts and bool(dts_outputs)

    all_outputs = js_outputs + js_map_outputs + dts_outputs + dts_map_outputs + js_passthrough

    # ── Compile actions ───────────────────────────────────────────────────
    for root in sorted(oxc_srcs_by_root.keys()):
        args = ctx.actions.args()
        args.add("--files")
        args.add_all(oxc_srcs_by_root[root])
        args.add("--out-dir", out_base)
        if root:
            args.add("--strip-dir-prefix", root)

        args.add("--target", ctx.attr.target)
        if ctx.attr.jsx_mode:
            args.add("--jsx", ctx.attr.jsx_mode)
        if ctx.attr.source_map:
            args.add("--source-map")
        if oxc_emits_dts:
            args.add("--declaration")
            args.add("--isolated-declarations")

        strict_deps_gated = True
        ctx.actions.run(
            inputs = depset(oxc_srcs_by_root[root] + strict_deps_inputs, transitive = [dep_dts_depset]),
            outputs = oxc_outs_by_root[root],
            executable = oxc.oxc_binary,
            arguments = [args],
            mnemonic = "OxcCompile",
            progress_message = "OxcCompile %{label}",
        )

    # ── tsgo action: declaration emit, or diagnostics only ────────────────
    #
    # This action already had to construct the complete program -- every
    # transitive .d.ts and every npm package.json -- to type-check. In tsgo mode
    # it keeps the declarations it computes instead of discarding them, which is
    # what removes the isolated-declarations requirement at near-zero cost.
    program_srcs = compile_srcs + js_srcs
    validation_outputs = []
    tsconfig = None
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if tsgo_emits_dts and not tsgo_toolchain_info and program_srcs:
        fail(
            "ts_compile: declarations = \"tsgo\" needs a tsgo toolchain, and none " +
            "is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")\n" +
            "Or set declarations = \"oxc\" to emit declarations with oxc instead " +
            "(which requires an explicit type on every export).",
        )
    if tsgo_toolchain_info and ctx.attr.enable_check and program_srcs:
        tsgo = tsgo_toolchain_info.tsgo_info

        emit_root_dir = None
        if tsgo_emits_dts:
            roots = {}
            for src in program_srcs:
                roots[_source_root(src, pkg)] = True
            root_list = sorted(roots.keys())
            if len(root_list) > 1:
                fail(
                    "ts_compile: srcs on {} hang off {} different roots, and one ".format(
                        ctx.label,
                        len(root_list),
                    ) +
                    "declaration emit has one rootDir:\n  " + "\n  ".join(root_list) + "\n" +
                    "A target may hold a whole subtree, but not a mix of checked-in and " +
                    "generated sources. Put the generated sources in their own ts_compile " +
                    "target and depend on it, or set declarations = \"oxc\" (or " +
                    "enable_check = False), neither of which emits from tsgo.",
                )
            emit_root_dir = root_list[0]

        # Include .ts/.tsx sources, JavaScript sources and ambient .d.ts files
        # in the tsconfig — ambient declarations provide type context for
        # checking.
        check_srcs = compile_srcs + js_srcs + passthrough_dts
        tsconfig = _generate_tsconfig(
            ctx = ctx,
            srcs = check_srcs,
            npm_pkg_dirs = npm_pkg_dirs if npm_pkg_dirs else None,
            npm_subpath_dts = npm_subpath_dts if npm_subpath_dts else None,
            npm_types_aliases = npm_types_aliases if npm_types_aliases else None,
            # Project globals first: where two `declare module` blocks name the
            # same pattern the earlier one wins, and native tsc reaches a
            # `types` package after the root files, not before them.
            ambient_dts = dep_globals_depset.to_list() +
                          [ambient_dts[path] for path in sorted(ambient_dts)],
            module_paths = module_paths,
            extends_file = ctx.file.tsconfig,
            baseline_file = baseline_file,
            allow_js = bool(js_srcs),
            emit_declarations = tsgo_emits_dts,
            isolated_declarations = oxc_emits_dts,
            emit_root_dir = emit_root_dir,
            emit_out_dir = out_base if tsgo_emits_dts else None,
            types_files = declaration_types,
            answered_types = answered_types,
        )

        # Build the depset of transitive npm package.json files so that
        # moduleResolution:"Bundler" can read exports/types fields from each
        # package. This must be computed before the action is registered.
        npm_pkg_dirs_depset = depset(transitive = transitive_package_dir_sets)

        # A package is free to publish data its consumers import, and
        # `resolveJsonModule` resolves that through the same `paths` key as any
        # other subpath. The declaration depsets carry no .json, so without
        # this the file the key names is simply not in the sandbox and the
        # import is TS2307 no matter what the key says. pkg_info_map is the
        # flattened closure, which is the same set `paths` is written from.
        npm_json_depset = depset(transitive = [
            npm_info.json_files
            for npm_info in pkg_info_map.values()
        ])

        strict_deps_gated = True
        tsgo_inputs = depset(
            check_srcs + [tsconfig, tsgo.tsgo_binary] + tsconfig_chain +
            ctx.files.path_alias_srcs + list(declaration_types.values()) + strict_deps_inputs,
            transitive = [
                dep_dts_depset,
                dep_globals_depset,
                npm_pkg_dirs_depset,
                npm_json_depset,
            ],
        )
        if not tsgo_emits_dts:
            # Diagnostics only. Stays in the _validation output group so it runs
            # concurrently with downstream compilation.
            stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
            check_args = ctx.actions.args()
            check_args.add("-stamp", stamp)
            check_args.add("--")
            check_args.add(tsgo.tsgo_binary)
            check_args.add("--project", tsconfig)
            check_args.add_all(ctx.attr.tsgo_args)
            check_args.add("--noEmit")
            ctx.actions.run(
                inputs = tsgo_inputs,
                outputs = [stamp],
                executable = ctx.executable._tsaction,
                arguments = ["stamp", check_args],
                mnemonic = "TsgoCheck",
                progress_message = "TsgoCheck %{label}",
            )
            validation_outputs.append(stamp)
        else:
            # The .d.ts files are real outputs, so a type error fails the build
            # by construction -- no --output_groups=+_validation needed, and a
            # broken target cannot hand a stale declaration to a consumer.
            tsgo_args = ctx.actions.args()
            tsgo_args.add("--project", tsconfig.path)
            tsgo_args.add_all(ctx.attr.tsgo_args)
            ctx.actions.run(
                inputs = tsgo_inputs,
                outputs = dts_outputs + dts_map_outputs,
                executable = tsgo.tsgo_binary,
                arguments = [tsgo_args],
                mnemonic = "TsgoDeclare",
                progress_message = "TsgoDeclare %{label}",
            )

    # A target with sources but no compile action of its own -- JavaScript srcs
    # with checking off -- has nothing to hang the stamp on, so it goes in the
    # output group Bazel requests for every target in the build.
    if strict_deps and not strict_deps_gated:
        validation_outputs.append(strict_deps.stamp)

    # ── Build providers ───────────────────────────────────────────────────
    direct_dts = depset(dts_outputs + passthrough_dts, order = "postorder")
    direct_js = depset(js_outputs + js_passthrough, order = "postorder")
    direct_js_map = depset(js_map_outputs, order = "postorder")

    transitive_dts = depset(
        dts_outputs + passthrough_dts,
        transitive = transitive_dts_sets,
        order = "postorder",
    )
    transitive_js = depset(
        js_outputs + js_passthrough,
        transitive = transitive_js_sets,
        order = "postorder",
    )
    transitive_js_map = depset(
        js_map_outputs,
        transitive = transitive_js_map_sets,
        order = "postorder",
    )

    # ts_compile produces no CSS and no assets of its own, so it only forwards
    # what its deps carry: the direct fields stay empty and the closure travels
    # in the transitive ones.
    transitive_css = depset(transitive = transitive_css_sets, order = "postorder")
    transitive_css_modules = depset(transitive = transitive_css_module_sets, order = "postorder")
    transitive_css_exports = depset(transitive = transitive_css_exports_sets, order = "postorder")
    transitive_assets = depset(transitive = transitive_asset_sets, order = "postorder")

    # No claim, nothing to reference: the entry would be an empty file every
    # consumer lists in `files`, so the scan does not run at all.
    own_global_entries = [
        _global_dts_entry(ctx, passthrough_dts, global_claims),
    ] if passthrough_dts and global_claims else []

    # Nothing but a consumer's tsconfig names the entry, so a leaf target would
    # never run the scan -- and the claim public_globals makes about a src is
    # checked in that scan.
    validation_outputs.extend(own_global_entries)

    providers = [
        # This target's own outputs. A dep's files reach a consumer through the
        # provider that describes them, not through this one.
        DefaultInfo(files = depset(all_outputs + passthrough_dts)),
        JsInfo(
            js_files = direct_js,
            js_map_files = direct_js_map,
            transitive_js_files = transitive_js,
            transitive_js_map_files = transitive_js_map,
        ),
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            transitive_npm_packages = depset(
                direct_npm_infos,
                transitive = [info.transitive_deps for info in direct_npm_infos] + dep_npm_closure_sets,
                order = "postorder",
            ),
            global_entry_files = depset(own_global_entries),
            transitive_global_entry_files = depset(
                own_global_entries,
                transitive = global_entry_sets,
                order = "postorder",
            ),
        ),
    ]

    # Derived from bin_dir rather than from a declared File so that a target
    # with no sources of its own still forwards its deps' modules.
    declaration_root = out_base
    source_root = "/".join([
        p
        for p in [ctx.label.workspace_root, pkg]
        if p
    ])
    own_modules = []
    if ctx.attr.module_name:
        own_modules.append(struct(
            module_name = ctx.attr.module_name,
            label = label_text(ctx.label),
            declaration_root = declaration_root,
            source_root = source_root,
            declared_paths = (),
        ))
    providers.append(TsModuleInfo(
        module_name = ctx.attr.module_name,
        label = label_text(ctx.label),
        declaration_root = declaration_root,
        source_root = source_root,
        declared_paths = (),
        transitive_modules = depset(own_modules, transitive = module_sets),
    ))

    # Always propagate CssInfo so ts_compile targets can be used as CSS deps.
    providers.append(CssInfo(
        css_files = depset(),
        transitive_css_files = transitive_css,
    ))

    # Propagate CssModuleInfo so ts_compile targets can carry CSS Module deps.
    providers.append(CssModuleInfo(
        css_files = depset(),
        transitive_css_files = transitive_css_modules,
        exports_files = depset(),
        transitive_exports_files = transitive_css_exports,
    ))

    # Propagate AssetInfo so ts_compile targets can carry asset deps.
    providers.append(AssetInfo(
        asset_files = depset(),
        transitive_asset_files = transitive_assets,
    ))

    output_groups = {}

    # The tsconfig this target hands the compiler, so a test can read the
    # resolution the build sees and compare it against the editor's. Absent on a
    # target with no program to check, which generates none.
    if tsconfig:
        output_groups["tsconfig"] = depset([tsconfig])
    if validation_outputs:
        output_groups["_validation"] = depset(validation_outputs)
    if strict_deps:
        # Requesting this group alone checks every target's deps without
        # compiling anything, since the check reads only the target's own srcs.
        output_groups["strict_deps"] = depset([strict_deps.stamp, strict_deps.checker])
    if output_groups:
        providers.append(OutputGroupInfo(**output_groups))

    return providers

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_compile = rule(
    implementation = _ts_compile_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = """Sources to compile.

.ts / .tsx      compiled by oxc; one .js (+ .js.map, + .d.ts) output each.
.js / .mjs/.cjs staged into the output tree unchanged and added to the type
                program. allowJs is set for them, so JSDoc types cross the
                package boundary; add checkJs through compiler_options to have
                them type-checked. Under declarations = "tsgo" each one also
                gets a declaration (.d.ts / .d.mts / .d.cts), the same as tsc,
                unless srcs already holds that file.
.d.ts / .d.mts / .d.cts
                declarations: type context for the check, passed straight
                through to consumers. One with no top-level import or export
                declares globals, and those are in scope in this target only,
                unless public_globals names the file. A .d.mts is the
                declaration of the .mjs of the same stem, whether or not that
                .mjs is in srcs: "./x.mjs" resolves to x.d.mts, and a .mjs
                listed beside its .d.mts is staged but leaves the type program,
                as under tsc, so the checked-in file is its only declaration.

Paths are kept relative to the target's package, so srcs may span a subtree.
""",
            allow_files = [".ts", ".tsx", ".d.ts", ".d.mts", ".d.cts", ".js", ".jsx", ".mjs", ".cjs"],
            mandatory = True,
        ),
        "public_globals": attr.label_list(
            doc = """The .d.ts in srcs whose globals every consumer gets too.

A .d.ts with no top-level import or export declares globals, and TypeScript
puts a global in every program the file is part of. That is a fact about the
language; whether those globals are part of the package's public type surface
is a decision about packaging, and this attribute is where a target makes it.

Unnamed is private. A src no entry here names types this target's own compile
-- it is in srcs and in the tsconfig this rule generates -- and is left out of
the generated <name>.globals.d.ts that consumers list in `files`, which is the
only route its declarations have into a consumer's program. The default is that
way round because the other way is silent: a library's `declare const process`
shim, real to its own standalone `tsc -p`, lands in `files` ahead of
@types/node in every consumer that has the real thing, and the duplicate
identifier is inside a .d.ts, where skipLibCheck hides it.

Name a file here for the declarations consumers are meant to have: a Worker's
generated `worker-configuration.d.ts`, a `declare module "*.svg"` a bundler
plugin backs. The unit is the file, because the module-or-global question
TypeScript answers is per file, so a .d.ts holding declarations for both
audiences is two files.

A consumer that turns out to need a global this does not name sees the
identifier as undefined; nothing distinguishes a global that stayed private
from one that never existed. Give that consumer the declaration through a dep
of its own -- @types/node for `process` -- or name the file here.

Every entry must be in srcs, and must be global: naming a module-scoped .d.ts
fails the build rather than passing as a no-op.
""",
            allow_files = [".d.ts", ".d.mts", ".d.cts"],
        ),
        "deps": attr.label_list(
            doc = "Other ts_compile, ts_npm_package, css_library, css_module, asset_library, or json_library targets that this target depends on.",
            providers = [[TsDeclarationInfo, JsInfo], [TsDeclarationInfo], [CssInfo], [CssModuleInfo], [AssetInfo]],
        ),
        "untyped_packages": attr.string_list(
            doc = """npm packages this target's type program leaves out entirely.

A named package gets no `paths` key -- not its own, not one per `exports`
subpath, and not the bare name a `@types/*` package would answer for it -- and
no `files` entry.

An entry names one package, and a package's declarations live wherever npm put
them: `ms` ships none and is typed by `@types/ms`, so `["@types/ms"]` is the
entry that takes those declarations out -- after which `ms` resolves to the
runtime package it names rather than being redirected into them. `["ms"]` takes
away the bare name `@types/ms` answered for it. Name both to leave nothing.

The package stays in `deps`, its files stay among the action's inputs, and no
JavaScript moves.

Strict deps sees the exclusion, though. A DIRECT dep stays declared, so an
import of it is still attributed. A package that was only REACHABLE -- the
case this attribute is for -- leaves the reachable set with the key, so an
import of it is a bare `TS2307` rather than "add this dep". That is the honest
answer: adding the dep back would not type it, because the exclusion is what
removed the types.

The case is a package whose declarations are a GLOBAL SCRIPT -- a .d.ts with no
top-level import or export. Everything such a file declares merges into every
program the file is part of, and a dynamic `import()` loads it exactly like a
static one. One `void import("@sentry/cloudflare")` in a browser component put
@cloudflare/workers-types' `interface Element` and `interface Body` into
lib.dom for 21 files in //web:web that name neither.

An import of a named package then resolves to nothing, which is TS2307. Give
this target a `declare module "<name>"` of its own in a .d.ts src to say what
the import means here. That declaration answers only because the `paths` key is
gone -- with the key in place TypeScript loads the file first and the globals
arrive anyway -- so the two go together.

Per target, and it does not travel: a dependent that needs the package resolves
it as before. The editor is a different matter, because the tsconfig
`bazel run //:refresh_tsconfig` writes has ONE `paths` map for the whole
workspace: it drops a package no reached target contributes any more, and
ts_refresh_tsconfig fails when one target here disagrees with another about it,
naming `host_only_packages` as the one place a workspace-wide answer fits.

Entries are npm package names as the lockfile spells them; one that matches no
package in this target's closure fails the build rather than passing as a
no-op.""",
        ),
        "target": attr.string(
            doc = "ECMAScript target version passed to oxc-bazel (e.g. 'es2022').",
            default = "es2022",
        ),
        "jsx_mode": attr.string(
            doc = "JSX transform mode: 'react-jsx', 'react', 'preserve'. Empty disables JSX.",
            default = "react-jsx",
        ),
        "declarations": attr.string(
            doc = """Which tool emits the .d.ts files.

"tsgo" (default): the tsgo action emits declarations from the full type
program. Source needs no explicit export annotations, and the declarations are
exactly what tsc would produce. Type errors fail the build because the .d.ts
are real outputs. Type-checking is on the critical path.

"oxc": oxc emits declarations syntactically, per file, without a type program.
This REQUIRES isolated declarations -- every export needs an explicit type --
and oxc errors if that does not hold. In exchange, type-checking moves off the
critical path into the _validation output group, so downstream targets compile
while checking runs concurrently.""",
            default = "tsgo",
            values = ["tsgo", "oxc"],
        ),
        "enable_check": attr.bool(
            doc = "Run tsgo type-checking as a validation action (requires tsgo toolchain).",
            default = True,
        ),
        "source_map": attr.bool(
            doc = """Emit one .js.map next to every .js oxc writes.

Off drops the outputs and the flag, for a target whose JavaScript nothing
debugs -- a codegen step, or a bundle input whose bundler makes its own map.""",
            default = True,
        ),
        "declaration_map": attr.bool(
            doc = """Emit a .d.ts.map next to every declaration.

This is what makes go-to-definition across a package boundary land on the
.ts source instead of the generated .d.ts. Needs the tsgo declaration emit
(declarations = "tsgo" with enable_check = True); oxc emits no map.""",
            default = False,
        ),
        "tsgo_args": attr.string_list(
            doc = """Extra flags for the tsgo invocation.

Only flags that report on the program are accepted -- --traceResolution,
--explainFiles, --listFiles, --listEmittedFiles, --diagnostics,
--extendedDiagnostics, --noErrorTruncation. A compilerOption belongs in
compiler_options instead, where the Bazel-owned-key guard can see it; any other
flag would move an output this rule already declared to Bazel.""",
        ),
        "tsconfig": attr.label(
            doc = """The project's own tsconfig.json, used as the compilerOptions baseline.

Either a .json file or a ts_config target (which additionally declares the
files the tsconfig `extends`). The file is referenced where it lives, not
copied, so relative paths inside it keep resolving against the directory they
were written for.

The generated tsconfig `extends` this file and overrides the options Bazel owns
(see _BAZEL_OWNED_OPTIONS) plus paths and include. Everything else -- lib, the
strict* family, verbatimModuleSyntax and the rest -- is whatever the file says,
so tsgo checks the code under the same options `tsc` would.

Setting this attribute adds the file's options; it never takes the ruleset's
baseline away. strict, module Preserve, skipLibCheck and esModuleInterop apply
either way, and with a `tsconfig` they sit UNDER it: every one of them the file
(or its own extends chain) mentions comes from the file, and only the ones it
says nothing about fall back to the baseline.

moduleResolution is the exception, asserted only when no `tsconfig` sets a
`module` the ruleset cannot see: TypeScript couples the two, so a baseline
value would outlive the module it belongs to. tsgo derives the resolver from
whichever `module` wins, which is Bundler for all of them but Node16/NodeNext.""",
            allow_single_file = [".json"],
        ),
        "compiler_options_json": attr.string(
            doc = """JSON object of compilerOptions overrides, on top of `tsconfig`.

Set through the ts_compile macro's lib / types / target / jsx_mode /
jsx_import_source / compiler_options arguments rather than written by hand.
Entries in `types` and `typeRoots` are treated as relative to the target's
package, matching how they are written in a package's own tsconfig.json.""",
        ),
        "module_name": attr.string(
            doc = """The bare specifier this target is importable as, e.g. "@acme/ui".

Dependents get a paths entry mapping this name (and its subpaths) to the .d.ts
files this target produces, wherever the current configuration puts them. The
entry point is index.d.ts.""",
        ),
        "path_aliases": attr.string_dict(
            doc = """Source-level path alias mappings to inject into the tsgo tsconfig.

Maps alias prefixes (as they appear in import statements) to workspace-relative
**source** directory paths. These are added to the compilerOptions.paths section
of the generated tsconfig so that tsgo can resolve path aliases that are defined
in the project's tsconfig.json (compilerOptions.paths cannot be inherited from
it: paths is one key, and the rule owns it).

An alias must resolve to files this target already stages -- its own srcs, or
files listed in path_alias_srcs. An alias pointing anywhere else is an analysis
error, because the action would resolve it against whatever another action
happened to leave in the sandbox. A value pointing into bazel-out/ is rejected
for the same reason, and is not needed: the rule maps each prefix onto both the
source directory and its bazel-bin mirror, so a css_library, asset_library or
json_library declaration for a file under the alias resolves too. A target whose
declarations land somewhere else is still out of reach -- set module_name on it
and depend on it.

Examples:
    # tsconfig.json has: {"@/*": ["./src/*"]}, and this target compiles src/.
    path_aliases = {"@/": "src/"}

    # The aliased files belong to another target.
    path_aliases = {"@lib/": "packages/lib/src/"}
    path_alias_srcs = ["//packages/lib/src:sources"]
""",
        ),
        "_tsaction": attr.label(
            default = Label("//ts/tools/tsaction"),
            executable = True,
            cfg = "exec",
        ),
        "_lib_check": attr.label(default = Label("//ts:lib_check")),
        "path_alias_srcs": attr.label_list(
            doc = """Files a path_aliases entry resolves to, when they are not in srcs.

They become inputs to the type-check action, which is what makes an alias into
another target's sources resolve the same way every time. tsgo type-checks them
as part of this program, so a type error in one of them fails this target: a
dep with module_name is the cheaper boundary where one is available.""",
            allow_files = True,
        ),
        "types_srcs": attr.label_list(
            doc = """The declarations a relative compilerOptions.types entry names.

`types = ["../../worker-configuration.d.ts"]` is a path, and a path resolves
against the sandbox: only what this action stages is in it. A src or a dep's
declaration is already there; this is the attribute for the file that is
neither, and an entry no staged file sits at is an analysis error naming the
label to add, where the compiler's own TS2688 from the action would name none.

A label, so the file may live in another package or be a build output -- the
entry is written to point wherever the file is; and, unlike a .d.ts in srcs,
it is not passed through as this target's own declaration. tsgo parses it as
part of this program, so a syntax error in it fails this target (TS1434 and
friends). What it declares is not checked: it is a .d.ts under the baseline's
skipLibCheck, so a type error inside it surfaces only under --//ts:lib_check or
compiler_options = {"skipLibCheck": False}.

Globals are what an entry here is for. A module -- a .d.ts with a top-level
import or export -- resolves and joins the program, but its declarations stay
scoped to it, so nothing global arrives; public_globals rejects one outright and
this does not, because a module in the program is still what a module
augmentation inside it needs.""",
            allow_files = [".d.ts", ".d.mts", ".d.cts"],
        ),
    },
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Compiles TypeScript source files using oxc-bazel.

Produces one .js, .js.map, and .d.ts output per .ts/.tsx input file, and stages
every .js/.mjs/.cjs input into the output tree as-is. Output paths stay relative
to the target's package, so srcs may span a subtree.

The .d.ts outputs are the compilation boundary: downstream ts_compile targets
only depend on the .d.ts files, enabling fine-grained Bazel caching.

When a tsgo toolchain is registered, type-checking runs as a validation
action in the _validation output group — it executes during `bazel build`
but does not block downstream targets.

Compiler options come from `tsconfig` (the project's own file, whatever it
says) and from `compiler_options_json`, in that order, with the options Bazel
owns applied last. Use the ts_compile macro in //ts:defs.bzl rather than this
rule directly: it takes lib / types / compiler_options as Starlark values.
""",
)
