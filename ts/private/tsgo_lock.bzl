"""Which TypeScript compiler a pnpm lockfile pins, and where its bytes are.

TypeScript 7 (`typescript` on npm) and its nightly (`@typescript/native-preview`)
are JavaScript launchers over a Go compiler that lives in per-platform optional
dependencies: `@typescript/typescript-linux-x64` holds `lib/tsc`,
`@typescript/native-preview-linux-x64` holds `lib/tsgo`. A consumer's lockfile
therefore already states, for every platform, the tarball and the integrity of
the compiler its own `tsc` runs, and the toolchain can fetch exactly that.

Pure text -> struct, so //tests/toolchain:tsgo_lock_tests can pin every message
without a fetch; the extension turns the error into a fail().
"""

load(
    "//npm/private:npm_translate_lock.bzl",
    "npm_tarball_url",
    "parse_importers",
    "parse_pnpm_lock",
    "pkg_matches_platform",
    "snapshot_parts",
    "verify_integrity",
)
load("//platforms:platforms.bzl", "PLATFORMS", "npm_arch")

# Mirrors lib/getExePath.js in the launcher: tsc for typescript, tsgo otherwise.
TSGO_PACKAGES = {
    "typescript": "tsc",
    "@typescript/native-preview": "tsgo",
}

def tsgo_package_layout(package):
    """The platform-package basename and binary name of a compiler package.

    Args:
        package: An npm package name.

    Returns:
        struct(base, binary), or None for a package that is not one of
        TSGO_PACKAGES.
    """
    if package not in TSGO_PACKAGES:
        return None
    return struct(
        base = package.split("/")[-1],
        binary = TSGO_PACKAGES[package],
    )

def _platform_package(base, platform):
    return "@typescript/{}-{}".format(base, npm_arch(platform))

def _spec(version, binary, platforms):
    return struct(version = version, binary = binary, platforms = platforms, error = "")

def _error(message):
    return struct(version = "", binary = "", platforms = {}, error = message)

def _unknown_package(package):
    return (
        "ts.tsgo(package = \"{}\"): the tsgo toolchain comes from \"typescript\" " +
        "(a TypeScript 7 release, whose compiler is tsc) or " +
        "\"@typescript/native-preview\" (a nightly, whose compiler is tsgo)."
    ).format(package)

def _add_hint(package):
    if package == "typescript":
        return "pnpm add -D typescript@7"
    return "pnpm add -D " + package

def tsgo_from_version(package, version, platforms, registries = {}):
    """The registry tarballs of one named release, with nothing to verify them.

    Args:
        package: "typescript" or "@typescript/native-preview".
        version: The release to download.
        platforms: The keys of PLATFORMS to declare a compiler for.
        registries: {"": default, "@scope": url} from .npmrc, or {}.

    Returns:
        struct(version, binary, platforms = {platform: struct(package, url,
        integrity = "")}, error).
    """
    layout = tsgo_package_layout(package)
    if layout == None:
        return _error(_unknown_package(package))
    resolved = {}
    for platform in platforms:
        dep = _platform_package(layout.base, platform)
        resolved[platform] = struct(
            package = dep,
            url = npm_tarball_url(dep, version, {}, registries),
            integrity = "",
        )
    return _spec(version, layout.binary, resolved)

def tsgo_from_pnpm_lock(content, package, platforms, lock_name, registries = {}):
    """The compiler a lockfile pins: its version and, per platform, its tarball.

    The root importer's entry for `package` decides the version; a lockfile whose
    root does not pin it falls back to the one `packages:` entry of that name.
    An alias under the name, several versions with no root pin, a version that
    ships no platform packages, and a platform package with no usable integrity
    are each an error naming the lockfile and the fix.

    Args:
        content: The text of pnpm-lock.yaml.
        package: "typescript" or "@typescript/native-preview".
        platforms: The keys of PLATFORMS to declare a compiler for.
        lock_name: How to name the lockfile in a message.
        registries: {"": default, "@scope": url} from .npmrc, or {}.

    Returns:
        struct(version, binary, platforms = {platform: struct(package, url,
        integrity)}, error). `error` is "" on success and the only field to read
        otherwise.
    """
    layout = tsgo_package_layout(package)
    if layout == None:
        return _error(_unknown_package(package))

    parsed = parse_pnpm_lock(content)
    snapshots = parsed["snapshots"]
    packages = parsed["packages"]
    root = parse_importers(content)["importers"].get(".", {"deps": {}})["deps"]

    sid = root.get(package, "")
    if sid and not sid.startswith(package + "@"):
        return _error((
            "{package} in {lock} is an alias for {target}, not the {package} package. " +
            "Depend on {package} itself at the workspace root, or name the package the " +
            "lockfile does pin with ts.tsgo(package = ...)."
        ).format(package = package, lock = lock_name, target = sid))
    if not sid:
        candidates = sorted([s for s in snapshots if snapshots[s]["name"] == package])
        if not candidates:
            return _error((
                "no {package} in {lock}: the tsgo toolchain is the TypeScript the lockfile " +
                "pins. Add it to the root package.json ({hint}) and run pnpm install, or pin " +
                "one explicitly with ts.tsgo(version = \"...\")."
            ).format(package = package, lock = lock_name, hint = _add_hint(package)))
        if len(candidates) > 1:
            return _error((
                "{package} in {lock} resolves to several versions ({versions}) and the root " +
                "importer pins none, so there is no one compiler to fetch. Pin one at the " +
                "workspace root ({hint})."
            ).format(
                package = package,
                lock = lock_name,
                versions = ", ".join(candidates),
                hint = _add_hint(package),
            ))
        sid = candidates[0]

    snap = snapshots.get(sid)
    if snap == None:
        return _error("{} names {} but has no snapshots: entry for it; run pnpm install to rewrite the lockfile.".format(lock_name, sid))

    optional = snap["optionalDependencies"]
    prefix = "@typescript/{}-".format(layout.base)
    if not [dep for dep in optional if dep.startswith(prefix)]:
        return _error((
            "{sid} in {lock} ships no native compiler (no {prefix}<os>-<cpu> optional " +
            "dependencies); rules_typescript needs TypeScript 7 or later."
        ).format(sid = sid, lock = lock_name, prefix = prefix))

    resolved = {}
    for platform in platforms:
        dep = _platform_package(layout.base, platform)
        dep_sid = optional.get(dep, "")
        if not dep_sid:
            return _error("{} in {} has no {} among its optional dependencies, so there is no compiler to fetch for {}.".format(sid, lock_name, dep, platform))
        package_id, _ = snapshot_parts(dep_sid)
        pkg = packages.get(package_id)
        if pkg == None:
            return _error("{} names {} under {} but has no packages: entry for it; run pnpm install to rewrite the lockfile.".format(lock_name, package_id, sid))
        entry = PLATFORMS[platform]
        if not pkg_matches_platform(pkg, entry.npm_os, entry.npm_cpu, entry.npm_libc):
            return _error((
                "{id} in {lock} is constrained to os {os} cpu {cpu} libc {libc}, which does " +
                "not admit {platform} ({npm_os}-{npm_cpu}, libc {npm_libc})."
            ).format(
                id = package_id,
                lock = lock_name,
                os = pkg.get("os", []),
                cpu = pkg.get("cpu", []),
                libc = pkg.get("libc", []),
                platform = platform,
                npm_os = entry.npm_os,
                npm_cpu = entry.npm_cpu,
                npm_libc = entry.npm_libc or "any",
            ))
        resolution = pkg.get("resolution", {})
        if verify_integrity({package_id: pkg}):
            return _error((
                "{id} in {lock} carries no usable integrity (resolution keys: {keys}); Bazel " +
                "would fetch the compiler with nothing to check the bytes against."
            ).format(
                id = package_id,
                lock = lock_name,
                keys = ", ".join(sorted(resolution.keys())) if resolution else "(none)",
            ))
        resolved[platform] = struct(
            package = dep,
            url = npm_tarball_url(dep, pkg["version"], resolution, registries),
            integrity = resolution["integrity"],
        )
    return _spec(snap["version"], layout.binary, resolved)
