"""What a pnpm workspace member's package.json says each of its specifiers means.

A member is imported by whatever its manifest declares it is entered through --
an `exports` map with a subpath per component, a `types` field, a `main` -- and
the compiler needs that as a tsconfig `paths` entry pointing at the declaration
Bazel emits. Guessing `<root>/index.d.ts` and `<root>/*` instead answers only the
members whose entry happens to be an index at the root, and answers every
`exports` subpath with a file that is not there.

The reading is split across the two phases because the information is:

  member_module_paths  runs where the manifest was decoded, in the module
                       extension. An `exports` map is ordered -- a resolver tries
                       its conditions in the order they are written -- and
                       `json.encode` sorts keys, so a manifest that travels to
                       analysis as JSON arrives with its condition order
                       destroyed. What travels is the answer, whose order lives
                       in lists that survive the round trip.
  declared_paths       runs in npm_workspace_package, which is the only place
                       that knows the package of the target compiling the member.
                       That is not always the member's own directory, and a
                       member-relative path would name the difference twice.
"""

load("//npm/private:npm_import.bzl", "export_targets", "root_export")

_DECLARATION_EXTENSIONS = (".d.ts", ".d.mts", ".d.cts")

# The declaration ts_compile emits from each source extension it accepts, which
# is tsc's own naming. A manifest written for a published build names the built
# `.js`; the same table answers that with the declaration beside it.
_DECLARATION_OF = {
    ".ts": ".d.ts",
    ".tsx": ".d.ts",
    ".js": ".d.ts",
    ".mjs": ".d.mts",
    ".cjs": ".d.cts",
}

# TypeScript's own order for a package directory, and the reason not to invent
# one: `exports` first, because a package that ships one has said there which
# files it means to be entered through, then the fields a package with no
# `exports` publishes its declarations in, `typings` before `types` as
# readPackageJsonTypesFields reads them, then `main` with the declaration beside
# it. `module` is absent on purpose -- it is a bundler convention, and no
# TypeScript resolution mode reads it, `bundler` included.
_ENTRY_FIELDS = ("typings", "types", "main")

def _declarations_for(target):
    """The declarations one manifest target designates, in resolution order.

    A member is compiled by Bazel, so a manifest names a source and a consumer's
    program needs the declaration emitted from it -- never the source, which
    would put the member's own uncompiled files in the consumer's program and
    type-check them against the consumer's options.

    An extension no compiler emits a declaration from -- `.css`, `.json`, an
    image -- designates nothing. An extensionless target is the file and then the
    directory index, which is the pair TypeScript itself tries.
    """
    path = target.removeprefix("./")
    if not path or path.startswith("../"):
        return []
    for extension in _DECLARATION_EXTENSIONS:
        if path.endswith(extension):
            return [path]
    for extension, declaration in _DECLARATION_OF.items():
        if path.endswith(extension):
            return [path[:-len(extension)] + declaration]
    if "." in path.split("/")[-1]:
        return []
    return [path + ".d.ts", path + "/index.d.ts"]

def _designated(node):
    """Every declaration an `exports` subtree designates, deduped, in order."""
    out = []
    for target in export_targets(node):
        for declaration in _declarations_for(target):
            if declaration not in out:
                out.append(declaration)
    return out

def _expressible(specifier, declarations):
    """Whether a `paths` pattern can express this subpath at all.

    `paths` substitutes one `*` per pattern, so a subpath with two of them has no
    pattern, and a starred value under an unstarred key has nothing to substitute
    into. Both are malformed `exports` too, and neither gets an entry rather than
    a guessed one.
    """
    if specifier.count("*") > 1:
        return False
    for declaration in declarations:
        if declaration.count("*") > 1:
            return False
        if "*" in declaration and "*" not in specifier:
            return False
    return True

def member_module_paths(manifest):
    """The declarations a member's manifest designates, keyed by specifier.

    A key is the part of an import that follows the package name, so "" is the
    bare specifier itself and "/tokens/*" is a wildcard subpath. Paths are
    relative to the member's own directory; declared_paths rebases them.

    A subpath whose `exports` value is `null` is not exported and designates
    nothing, so it gets no entry and resolves exactly as it did before any
    manifest was read. Enforcing that it must NOT resolve is not this map's job:
    `paths` tells a compiler where a name lives, and what a target is allowed to
    import is the strict-deps check's answer to give.

    Args:
        manifest: A member's decoded package.json.
    """
    if type(manifest) != "dict":
        return {}

    exports = manifest.get("exports")
    out = {}

    root = _designated(root_export(exports))
    if not root:
        for field in _ENTRY_FIELDS:
            value = manifest.get(field)
            if type(value) == "string" and value:
                root = _declarations_for(value)
                if root:
                    break
    if root:
        out[""] = root

    if type(exports) == "dict":
        for key in exports.keys():
            if not key.startswith("./"):
                continue
            designated = _designated(exports[key])
            if designated:
                out[key.removeprefix(".")] = designated

    return {
        specifier: declarations
        for specifier, declarations in out.items()
        if _expressible(specifier, declarations)
    }

def _under_offset(declarations, offset):
    """The same declarations, relative to the compiling target's package.

    A member whose entry sits in a subdirectory is compiled by a target in that
    subdirectory, which is npm_hub's own rule for which target a `link:` means.
    The roots TsModuleInfo reports are then that subdirectory, and a
    member-relative path would name it twice. A declaration outside the
    subdirectory belongs to some other target, and is dropped: one hub target
    speaks for one compiling target.
    """
    if not offset:
        return declarations
    prefix = offset + "/"
    return [d.removeprefix(prefix) for d in declarations if d.startswith(prefix)]

def target_offset(member_dir, package):
    """The compiling target's package, relative to the member's directory."""
    if not member_dir or package == member_dir:
        return ""
    if not package.startswith(member_dir + "/"):
        return ""
    return package[len(member_dir) + 1:]

def declared_paths(module_paths, offset):
    """TsModuleInfo.declared_paths from what member_module_paths worked out.

    A tuple of structs rather than a dict because this travels in a depset, whose
    elements have to be hashable.

    Args:
        module_paths: specifier -> declarations, from member_module_paths.
        offset: The compiling target's package under the member, from
            target_offset.
    """
    out = []
    for specifier in sorted(module_paths):
        declarations = _under_offset(module_paths[specifier], offset)
        if declarations:
            out.append(struct(specifier = specifier, declarations = tuple(declarations)))
    return tuple(out)
