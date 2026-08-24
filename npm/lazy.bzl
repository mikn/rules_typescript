"""Declares one repository per npm package, plus an alias hub.

A single repository for the whole lockfile has to download everything before it
can generate any target: it reads `bin` and `exports` out of each extracted
`package.json`. On a 2731-package lockfile that is roughly five minutes and five
gigabytes, paid in full by a target whose only npm dependency is vitest, in one
serial Starlark loop with no resume.

Here the extension does the whole-graph analysis -- which it can do from the
lockfile text alone, with no network -- and then declares one `npm_import` per
package. Each package reads its own `package.json` and writes its own BUILD
file, so Bazel fetches repositories on demand.

The analysis stays here rather than moving into the packages because all of it
is a whole-graph decision that one package cannot make about itself:

  platform filtering    optional native packages for other operating systems
                        must be dropped before anything references them
  label assignment      `@npm//:react` means the highest version present, which
                        is only knowable across the whole lockfile
  @types pairing        matching react@19 to @types/react@19 requires seeing both
  cycle breaking        npm has genuine circular peer dependencies and a Bazel
                        target graph may not contain a cycle
  alias naming          an npm alias is a name no package claims as its own, so
                        only the graph knows the alias exists
  patch routing         `patchedDependencies` is keyed by name@version at the
                        top of the lockfile, nowhere near the package it patches
"""

load(
    "//ts/private:npm_import.bzl",
    "npm_hub",
    "npm_import",
)
load(
    "//ts/private:npm_translate_lock.bzl",
    "detect_and_break_cycles",
    "host_platform",
    "npm_tarball_url",
    "package_dir_name",
    "package_name_to_label",
    "parse_importers",
    "parse_patched_dependencies",
    "parse_pnpm_lock",
    "pkg_matches_host_platform",
    "resolve_dep_version",
    "semver_gt",
    "semver_parts",
    "versioned_label_name",
)

def _resolve_dep_id(live, dep_name, dep_spec):
    """Maps a declared dependency to its lockfile key, or None.

    resolve_dep_version returns the concrete VERSION, not the key, and for an
    npm alias (`h3-v2: h3@2.0.1`) that version is already the key of the aliased
    package. Getting this wrong drops the edge silently rather than failing.
    """
    version = resolve_dep_version(live, dep_name, dep_spec)
    if version == None:
        return None
    pkg_id = "{}@{}".format(dep_name, version)
    if pkg_id in live:
        return pkg_id
    if version in live:
        return version
    return None

def _repo_name(prefix, package, version):
    return "{}__{}".format(prefix, package_dir_name(package, version))

def _alias_target_name(alias_package):
    """Target name inside the aliased package's own repository.

    The `alias_` prefix keeps it clear of `pkg` and `bin`, and the shared
    name-to-label rule keeps a scoped alias (`@my/thing`) out of subpackage
    territory.
    """
    return "alias_{}".format(package_name_to_label(alias_package))

_HEX = "0123456789abcdef"

def _is_sha256_hex(value):
    """Whether the lockfile's recorded digest is one we can check the file against.

    pnpm 9 and later record the plain sha256 of the patch bytes. Older lockfiles
    record something else, and a digest we cannot interpret is not a digest we
    may reject a file over.
    """
    if len(value) != 64:
        return False
    for ch in value.elems():
        if ch not in _HEX:
            return False
    return True

def _patch_by_package(lock_content, patch_labels):
    """Pairs each patchedDependencies entry with the patch file that satisfies it.

    Matching is by filename, which is pnpm's own: `pnpm patch-commit` writes
    `patches/<name with / replaced by __>@<version>.patch`, and the lockfile key
    is `<name>@<version>`. The alternative -- reading `path:` out of
    pnpm-workspace.yaml -- cannot be done at all: turning `patches/foo.patch`
    into a label requires knowing where the consumer's Bazel package boundaries
    fall, which the extension has no way to discover.

    Both directions are errors, because both mean the build would not be
    building what pnpm installs:
      a declared patch with no file      the package would install unpatched
      a patch file no entry declares     the patch is stale, or misnamed

    Returns:
        dict: {pkg_id: struct(label = Label, sha256 = str)}
    """
    patched = parse_patched_dependencies(lock_content)
    unclaimed = {}
    for label in patch_labels:
        unclaimed[label.name.split("/")[-1]] = label

    result = {}
    missing = []
    for pkg_id, digest in patched.items():
        filename = "{}.patch".format(pkg_id.replace("/", "__"))
        if filename in unclaimed:
            result[pkg_id] = struct(
                label = unclaimed.pop(filename),
                sha256 = digest if _is_sha256_hex(digest) else "",
            )
        else:
            missing.append("  {} -> expected a patch file named {}".format(pkg_id, filename))

    if missing:
        fail(
            "npm: pnpm-lock.yaml declares patchedDependencies with no patch file passed " +
            "to npm.translate_lock(patches = [...]):\n" + "\n".join(missing) +
            "\nWithout the patch these packages would install as published, which is " +
            "exactly what the lockfile says they must not be.",
        )
    if unclaimed:
        fail(
            "npm: patch files passed to npm.translate_lock(patches = [...]) that no " +
            "patchedDependencies entry claims: {}\n".format(", ".join(sorted(unclaimed.keys()))) +
            "Either the lockfile is stale (re-run pnpm install) or the file is not named " +
            "after the package it patches (<name with / as __>@<version>.patch).",
        )
    return result

def declare_lazy_npm_repos(module_ctx, hub_name, pnpm_lock, patch_labels):
    """Declares one npm_import per package plus one npm_hub of aliases.

    Args:
        module_ctx:   The module extension context.
        hub_name:     Name of the alias hub repository, conventionally "npm".
        pnpm_lock:    Label of the pnpm-lock.yaml to read.
        patch_labels: Labels of the pnpm patch files named by patchedDependencies.
    """
    lock_content = module_ctx.read(pnpm_lock)
    packages = parse_pnpm_lock(lock_content)["packages"]
    host_os, host_cpu = host_platform(module_ctx)
    patches = _patch_by_package(lock_content, patch_labels)

    # Drop packages built for other platforms before anything can depend on them.
    live = {}
    for pkg_id, pkg in packages.items():
        if not pkg.get("name") or not pkg.get("version"):
            continue
        if pkg_matches_host_platform(pkg, host_os, host_cpu):
            live[pkg_id] = pkg

    # Which pkg_id each label points at. A bare label is the highest version
    # present; every version additionally gets a version-suffixed label.
    versions_by_label = {}
    for pkg_id, pkg in live.items():
        label = package_name_to_label(pkg["name"])
        versions_by_label.setdefault(label, []).append(pkg_id)

    # label_of is the package's identity in the dependency graph. extra_labels
    # holds the additional names the hub must also expose: when a package
    # resolves at several versions every version gets a version-suffixed label,
    # and the highest keeps the bare one, so a consumer can pin or not.
    label_of = {}
    extra_labels = {}
    for label, pkg_ids in versions_by_label.items():
        best = pkg_ids[0]
        for pkg_id in pkg_ids[1:]:
            if semver_gt(semver_parts(live[pkg_id]["version"]), semver_parts(live[best]["version"])):
                best = pkg_id
        if len(pkg_ids) == 1:
            label_of[pkg_ids[0]] = label
            continue
        for pkg_id in pkg_ids:
            versioned = versioned_label_name(label, live[pkg_id]["version"])
            label_of[pkg_id] = label if pkg_id == best else versioned
            if pkg_id == best:
                extra_labels[versioned] = pkg_id

    # @types/<x> provides declarations for <x>. Prefer the @types major that
    # matches the runtime major, else the highest available.
    types_for = {}
    for pkg_id, pkg in live.items():
        name = pkg["name"]
        if not name.startswith("@types/"):
            continue
        runtime_name = name[len("@types/"):]
        types_for.setdefault(runtime_name, []).append(pkg_id)

    def _types_pkg_for(runtime_pkg):
        candidates = types_for.get(runtime_pkg["name"], [])
        if not candidates:
            return None
        runtime_major = semver_parts(runtime_pkg["version"])[0]
        best = None
        for pkg_id in candidates:
            if semver_parts(live[pkg_id]["version"])[0] == runtime_major:
                if best == None or semver_gt(semver_parts(live[pkg_id]["version"]), semver_parts(live[best]["version"])):
                    best = pkg_id
        if best != None:
            return best
        for pkg_id in candidates:
            if best == None or semver_gt(semver_parts(live[pkg_id]["version"]), semver_parts(live[best]["version"])):
                best = pkg_id
        return best

    # Resolve declared dependency specs to pkg_ids, then break cycles on the
    # label graph. An edge additionally records the name the dependent imports
    # the package under, which differs from the package's own name exactly when
    # the spec is an npm alias.
    deps_by_pkg = {}
    optional_by_pkg = {}
    aliases_of_pkg = {}
    for pkg_id, pkg in live.items():
        resolved = []
        optional = []
        for section in ("dependencies", "optionalDependencies"):
            for dep_name, dep_spec in pkg.get(section, {}).items():
                dep_id = _resolve_dep_id(live, dep_name, dep_spec)
                if dep_id == None or dep_id == pkg_id:
                    continue
                imported_as = dep_name if live[dep_id]["name"] != dep_name else ""
                if imported_as:
                    aliases_of_pkg.setdefault(dep_id, {})[imported_as] = True
                resolved.append((dep_id, imported_as))
                if section == "optionalDependencies":
                    optional.append(dep_id)
        deps_by_pkg[pkg_id] = resolved
        optional_by_pkg[pkg_id] = optional

    # An alias the workspace itself imports is the one a consumer writes in a
    # BUILD file (`@npm//:h3-v2`), and no package in the graph declares it, so
    # without reading the importers for aliases the label would not exist.
    importers = parse_importers(lock_content)
    for imported_as, target in importers["aliases"].items():
        if target in live and live[target]["name"] != imported_as:
            aliases_of_pkg.setdefault(target, {})[imported_as] = True

    label_graph = {}
    for pkg_id, edges in deps_by_pkg.items():
        label_graph[label_of[pkg_id]] = [label_of[dep_id] for (dep_id, _) in edges]
    broken = detect_and_break_cycles(label_graph)

    allowed = {}
    for label, dep_labels in label_graph.items():
        allowed[label] = {d: True for d in dep_labels}

    # Declare the packages.
    repo_of = {pkg_id: _repo_name(hub_name, live[pkg_id]["name"], live[pkg_id]["version"]) for pkg_id in live}

    def _dep_label(dep_id, imported_as):
        target = _alias_target_name(imported_as) if imported_as else "pkg"
        return "@{}//:{}".format(repo_of[dep_id], target)

    for pkg_id, pkg in live.items():
        own_label = label_of[pkg_id]
        dep_labels = []
        for (dep_id, imported_as) in deps_by_pkg[pkg_id]:
            # The cycle breaker works on package identity, so an alias edge is
            # judged by the package it reaches, not by the name it reaches it by.
            if allowed.get(own_label, {}).get(label_of[dep_id]):
                dep_labels.append(_dep_label(dep_id, imported_as))

        types_id = None
        if not pkg["name"].startswith("@types/"):
            types_id = _types_pkg_for(pkg)

        patch = patches.get(pkg_id)
        npm_import(
            name = repo_of[pkg_id],
            package = pkg["name"],
            version = pkg["version"],
            url = npm_tarball_url(pkg["name"], pkg["version"], pkg.get("resolution", {})),
            integrity = pkg.get("resolution", {}).get("integrity", ""),
            deps = sorted({d: True for d in dep_labels}.keys()),
            types_dep = "@{}//:pkg".format(repo_of[types_id]) if types_id else "",
            is_types_package = pkg["name"].startswith("@types/"),
            aliases = {
                _alias_target_name(alias): alias
                for alias in sorted(aliases_of_pkg.get(pkg_id, {}).keys())
            },
            patch = patch.label if patch else None,
            patch_sha256 = patch.sha256 if patch else "",
            # Platform-matched optionalDependencies are how npm ships native
            # sidecars (oxlint -> @oxlint/linux-x64-gnu). A bin script resolves
            # them through node_modules at runtime, and with one repository per
            # package they are no longer siblings on disk, so they have to be
            # named as targets. Not filtered by the cycle breaker: these are leaf
            # binaries, and this is a runfiles edge rather than a build edge.
            optional_dep_packages = sorted([
                "@{}//:pkg".format(repo_of[dep_id])
                for dep_id in optional_by_pkg[pkg_id]
            ]),
        )

    # The hub: aliases only, no downloads, so referencing one name fetches one
    # package. Bin targets are aliased lazily too -- the package's own BUILD file
    # decides whether a bin target exists, so the alias may dangle for a package
    # with no bin, which only errors if someone actually asks for it.
    aliases = {}
    for pkg_id in live:
        aliases[label_of[pkg_id]] = "@{}//:pkg".format(repo_of[pkg_id])
        aliases[label_of[pkg_id] + "_bin"] = "@{}//:bin".format(repo_of[pkg_id])
    for extra_label, pkg_id in extra_labels.items():
        aliases[extra_label] = "@{}//:pkg".format(repo_of[pkg_id])

    # An npm alias only gets a hub label when no real package owns that name --
    # a package actually called `h3-v2` is what a consumer means by `@npm//:h3-v2`.
    real_labels = {label: True for label in aliases}
    alias_owner = {}
    for pkg_id, alias_names in aliases_of_pkg.items():
        for alias in alias_names:
            label = package_name_to_label(alias)
            if label in real_labels:
                continue
            owner = alias_owner.get(label)
            if owner != None and owner != pkg_id:
                # The hub is one flat namespace, so two importers aliasing the
                # same name at different packages has no right answer. Silently
                # keeping one would hand half the workspace the wrong package.
                fail(
                    "npm: the alias '{}' resolves to both {} and {} in this lockfile, ".format(
                        alias,
                        owner,
                        pkg_id,
                    ) +
                    "so @{}//:{} cannot mean one of them.".format(hub_name, label),
                )
            alias_owner[label] = pkg_id
            aliases[label] = "@{}//:{}".format(repo_of[pkg_id], _alias_target_name(alias))

    for name, path in importers["links"].items():
        if path:
            aliases[package_name_to_label(name)] = "@@//{path}:{target}".format(
                path = path,
                target = path.split("/")[-1],
            )

    npm_hub(
        name = hub_name,
        aliases = aliases,
        broken_cycle_edges = ["{} -> {}".format(a, b) for (a, b) in broken],
    )
