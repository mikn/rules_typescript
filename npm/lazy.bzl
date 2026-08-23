"""Declares one repository per npm package, plus an alias hub.

`npm_translate_lock` puts every package in a single repository. It has to: it
reads `bin` and `exports` out of each extracted `package.json` to generate
targets, so nothing can be emitted until everything is downloaded. On a
2731-package lockfile that is roughly five minutes and five gigabytes, paid in
full by a target whose only npm dependency is vitest, in one serial Starlark
loop with no resume.

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
"""

load(
    "//ts/private:npm_import.bzl",
    "npm_hub",
    "npm_import",
)
load(
    "//ts/private:npm_translate_lock.bzl",
    "detect_and_break_cycles",
    "npm_tarball_url",
    "package_dir_name",
    "package_name_to_label",
    "parse_pnpm_lock",
    "parse_workspace_aliases",
    "pkg_matches_host_platform",
    "resolve_dep_version",
    "semver_gt",
    "semver_parts",
    "versioned_label_name",
)

def _host(module_ctx):
    """Host os/cpu, in the vocabulary pkg_matches_host_platform expects."""
    name = module_ctx.os.name.lower()
    if name.startswith("mac os"):
        host_os = "darwin"
    elif name.startswith("windows"):
        host_os = "win32"
    else:
        host_os = "linux"

    arch = module_ctx.os.arch.lower()
    if arch in ("aarch64", "arm64"):
        host_cpu = "arm64"
    else:
        host_cpu = "x64"
    return host_os, host_cpu

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

def declare_lazy_npm_repos(module_ctx, hub_name, pnpm_lock):
    """Declares one npm_import per package plus one npm_hub of aliases.

    Args:
        module_ctx: The module extension context.
        hub_name:   Name of the alias hub repository, conventionally "npm".
        pnpm_lock:  Label of the pnpm-lock.yaml to read.
    """
    lock_content = module_ctx.read(pnpm_lock)
    packages = parse_pnpm_lock(lock_content)["packages"]
    host_os, host_cpu = _host(module_ctx)

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
    # label graph, matching npm_translate_lock's behaviour.
    deps_by_pkg = {}
    optional_by_pkg = {}
    for pkg_id, pkg in live.items():
        resolved = []
        optional = []
        for section in ("dependencies", "optionalDependencies"):
            for dep_name, dep_spec in pkg.get(section, {}).items():
                dep_id = _resolve_dep_id(live, dep_name, dep_spec)
                if dep_id != None and dep_id != pkg_id:
                    resolved.append(dep_id)
                    if section == "optionalDependencies":
                        optional.append(dep_id)
        deps_by_pkg[pkg_id] = resolved
        optional_by_pkg[pkg_id] = optional

    label_graph = {}
    for pkg_id, dep_ids in deps_by_pkg.items():
        label_graph[label_of[pkg_id]] = [label_of[d] for d in dep_ids]
    broken = detect_and_break_cycles(label_graph)

    allowed = {}
    for label, dep_labels in label_graph.items():
        allowed[label] = {d: True for d in dep_labels}

    # Declare the packages.
    repo_of = {pkg_id: _repo_name(hub_name, live[pkg_id]["name"], live[pkg_id]["version"]) for pkg_id in live}
    for pkg_id, pkg in live.items():
        own_label = label_of[pkg_id]
        dep_labels = []
        for dep_id in deps_by_pkg[pkg_id]:
            if allowed.get(own_label, {}).get(label_of[dep_id]):
                dep_labels.append("@{}//:pkg".format(repo_of[dep_id]))

        types_id = None
        if not pkg["name"].startswith("@types/"):
            types_id = _types_pkg_for(pkg)

        npm_import(
            name = repo_of[pkg_id],
            package = pkg["name"],
            version = pkg["version"],
            url = npm_tarball_url(pkg["name"], pkg["version"], pkg.get("resolution", {})),
            integrity = pkg.get("resolution", {}).get("integrity", ""),
            deps = sorted({d: True for d in dep_labels}.keys()),
            types_dep = "@{}//:pkg".format(repo_of[types_id]) if types_id else "",
            is_types_package = pkg["name"].startswith("@types/"),
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

    for name, path in parse_workspace_aliases(lock_content).items():
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
