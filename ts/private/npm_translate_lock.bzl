"""pnpm lockfile reader: the whole-graph analysis behind the npm extension.

Everything here runs on lockfile text alone, with no network and no extracted
package, so npm/lazy.bzl can analyse the whole graph inside the module extension
and then declare one already-resolved repository per package.

What the lockfile has already decided, and we therefore must NOT re-derive:

  catalogs           every use site carries a concrete `version:`, so the
                     `catalogs:` block is dead weight.
  overrides          an override is a version SELECTION, so each outcome is
                     already its own `name@version` key in `packages:`.
  packageExtensions  injected peers appear in `snapshots:` as resolved
                     dependency edges, which _parse_snapshots_deps ingests.

Each of those blocks is shaped exactly like the ones we do read -- `catalogs:`
entries look like importers, `overrides:` entries look like package keys -- so
every reader below anchors on its own `<section>:` header at indent 0 and stops
at the next indent-0 line. Nothing scans the file globally.

What the lockfile does NOT resolve is `patchedDependencies:`. The `packages:`
integrity is the byte-identical PRE-patch registry tarball, so the patch has to
be fetched and applied by whoever downloads the package.

Lockfile support: pnpm lockfile format v6 and v9.
"""

# ─── pnpm lockfile parsing ─────────────────────────────────────────────────────
#
# Starlark does not have a YAML parser built in, so we implement a minimal
# line-oriented parser sufficient for pnpm-lock.yaml format v6 and v9.
#
# pnpm-lock.yaml structure (simplified):
#
#   lockfileVersion: '6.0'
#
#   packages:
#     /react@19.0.0:
#       resolution: {integrity: sha512-...}
#       dependencies:
#         loose-envify: ^1.1.0
#
# In lockfile v9 the key format changes to `react@19.0.0` (no leading slash).

def _strip_leading_slash(s):
    if s.startswith("/"):
        return s[1:]
    return s

def _parse_package_key(key):
    """Parses a pnpm lockfile package key into (name, version).

    Handles both v6 (/name@version) and v9 (name@version) formats.
    Handles scoped packages: /@scope/name@version or @scope/name@version.
    Handles pnpm v9 format where scoped package keys are quoted:
      '@types/estree@1.0.8' → ("@types/estree", "1.0.8")
    Handles pnpm v9 peer-dependency suffixes:
      react-dom@19.0.0(react@19.0.0) → ("react-dom", "19.0.0")
    """
    key = key.strip()
    # Strip surrounding single quotes added by pnpm v9 for scoped packages.
    if key.startswith("'") and key.endswith("'"):
        key = key[1:-1]
    key = _strip_leading_slash(key)
    # Strip pnpm v9 peer-dependency suffix before parsing name@version.
    # e.g. "react-dom@19.0.0(react@19.0.0)" → "react-dom@19.0.0"
    paren_idx = key.find("(")
    if paren_idx != -1:
        key = key[:paren_idx]
    if key.startswith("@"):
        # scoped: @scope/name@version
        slash_idx = key.find("/")
        if slash_idx == -1:
            return (key, "")
        rest = key[slash_idx + 1:]  # "name@version"
        at_idx = rest.rfind("@")
        if at_idx == -1:
            return (key, "")
        pkg_name = key[:slash_idx + 1 + at_idx]  # "@scope/name"
        version = rest[at_idx + 1:]
        return (pkg_name, version)
    else:
        at_idx = key.rfind("@")
        if at_idx == -1:
            return (key, "")
        return (key[:at_idx], key[at_idx + 1:])

def _indent_level(line):
    """Returns the number of leading spaces in a line."""
    count = 0
    for ch in line.elems():
        if ch == " ":
            count += 1
        else:
            break
    return count

def _new_pkg_entry():
    """Returns a fresh mutable dict for a package entry."""
    return {
        "name": "",
        "version": "",
        "resolution": {},
        "dependencies": {},
        "optionalDependencies": {},
        "peerDependencies": {},
        "os": [],
        "cpu": [],
    }

def _record_package(packages, current_pkg_key, current_pkg):
    """Records current_pkg into packages if current_pkg_key is set.

    Records ALL packages including those with os/cpu constraints.  Platform
    filtering happens in the caller, once the host platform is known.
    """
    if current_pkg_key:
        name, version = _parse_package_key(current_pkg_key)
        if not name or not version:
            return
        pkg_id = "{}@{}".format(name, version)
        entry = dict(current_pkg)
        entry["name"] = name
        entry["version"] = version
        packages[pkg_id] = entry

def _parse_inline_resolution(value):
    """Parses an inline YAML mapping like '{integrity: sha512-...}' into a dict."""
    inner = value.strip()
    if inner.startswith("{"):
        inner = inner[1:]
    if inner.endswith("}"):
        inner = inner[:-1]
    result = {}
    for pair in inner.split(","):
        pair = pair.strip()
        if ":" in pair:
            rk, _, rv = pair.partition(":")
            result[rk.strip()] = rv.strip().strip("'\"")
    return result

def _find_lockfile_version(lines):
    """Scans lines for lockfileVersion and returns it as a string."""
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("lockfileVersion:"):
            parts = stripped.split(":", 1)
            return parts[1].strip().strip("'\"")
    return ""

def _find_packages_start(lines):
    """Returns the index of the 'packages:' section header, or -1."""
    for idx in range(len(lines)):
        if lines[idx].rstrip() == "packages:":
            return idx
    return -1

def _find_snapshots_start(lines):
    """Returns the index of the 'snapshots:' section header, or -1."""
    for idx in range(len(lines)):
        if lines[idx].rstrip() == "snapshots:":
            return idx
    return -1

def _parse_snapshots_deps(lines, snapshots_start, packages):
    """Parses the snapshots: section and merges dependency info into packages.

    In pnpm lockfile v9, the dependencies: block lives in snapshots:, not packages:.
    This function fills in the "dependencies" field for each existing package entry.

    A snapshot key may have a peer-dependency suffix like:
      '@vitest/mocker@3.0.9(vite@6.4.1)'
    which we strip to match the canonical 'name@version' key in packages.

    Args:
        lines:           All lines of the lockfile.
        snapshots_start: Line index of the 'snapshots:' header.
        packages:        The dict of already-parsed packages (mutated in place).
    """
    state = {
        "current_pkg_key": None,
        "current_section": None,
        "done": False,
    }

    for idx in range(snapshots_start + 1, len(lines)):
        if state["done"]:
            break

        line = lines[idx]
        raw_line = line.rstrip()

        if not raw_line or raw_line.startswith("#"):
            continue

        indent = _indent_level(raw_line)
        stripped = raw_line.strip()

        # Top-level key ends the snapshots section.
        if indent == 0 and not raw_line.startswith(" "):
            state["done"] = True
            continue

        # Snapshot key: indent 2, ends with colon.
        if indent == 2 and stripped.endswith(":"):
            raw_key = stripped[:-1]
            # Strip surrounding single quotes first (pnpm v9 scoped package keys).
            if raw_key.startswith("'") and raw_key.endswith("'"):
                raw_key = raw_key[1:-1]
            # Strip peer-dep suffix e.g. name@ver(peer@ver) → name@ver
            paren_idx = raw_key.find("(")
            if paren_idx != -1:
                raw_key = raw_key[:paren_idx]
            name, version = _parse_package_key(raw_key)
            canonical = "{}@{}".format(name, version)
            state["current_pkg_key"] = canonical if canonical in packages else None
            state["current_section"] = None
            continue

        if state["current_pkg_key"] == None:
            continue

        # Section header at indent 4.
        if indent == 4 and stripped.endswith(":") and ":" not in stripped[:-1]:
            section_name = stripped[:-1]
            if section_name in ("dependencies", "optionalDependencies"):
                state["current_section"] = section_name
            else:
                state["current_section"] = None
            continue

        # Dependency entries at indent 6.
        if indent == 6 and ":" in stripped and state["current_section"] in ("dependencies", "optionalDependencies"):
            kv_k, _, kv_v = stripped.partition(":")
            # Strip surrounding quotes from dep names (pnpm v9 quotes scoped packages).
            dep_name = kv_k.strip().strip("'\"")
            dep_version = kv_v.strip().strip("'\"")
            # Strip peer suffix from version spec in snapshots.
            paren_idx = dep_version.find("(")
            if paren_idx != -1:
                dep_version = dep_version[:paren_idx]
            pkg_entry = packages[state["current_pkg_key"]]
            pkg_entry[state["current_section"]][dep_name] = dep_version
            continue

def _parse_pnpm_lock(content):
    """Parses pnpm-lock.yaml content into a dict of package metadata.

    Handles both pnpm lockfile v6 (dependencies in packages: section) and
    v9 (dependencies in snapshots: section; packages: only has resolution info).

    Returns:
        dict: {
            "lockfile_version": str,
            "packages": {
                "name@version": {
                    "name": str,
                    "version": str,
                    "resolution": {integrity: str, tarball: str, ...},
                    "dependencies": {name: version_spec, ...},
                    "optionalDependencies": {name: version_spec, ...},
                    "peerDependencies": {name: version_spec, ...},
                },
            }
        }
    """
    lines = content.split("\n")
    lockfile_version = _find_lockfile_version(lines)
    packages = {}

    packages_start = _find_packages_start(lines)
    if packages_start == -1:
        return {"lockfile_version": lockfile_version, "packages": packages}

    # Parse package entries using a for loop with a mutable state dict.
    # Starlark forbids while loops, so we track state as a single mutable dict.
    state = {
        "current_pkg_key": None,
        "current_section": None,
        "current_pkg": _new_pkg_entry(),
        "done": False,
    }

    for idx in range(packages_start + 1, len(lines)):
        if state["done"]:
            break

        line = lines[idx]
        raw_line = line.rstrip()

        # Skip blank lines and comments.
        if not raw_line or raw_line.startswith("#"):
            continue

        indent = _indent_level(raw_line)
        stripped = raw_line.strip()

        # Detect end of packages section: a top-level key (indent 0).
        if indent == 0 and not raw_line.startswith(" "):
            if stripped != "packages:":
                _record_package(packages, state["current_pkg_key"], state["current_pkg"])
                state["current_pkg_key"] = None
                state["done"] = True
            continue

        # Package key: indent 2, ends with colon.
        if indent == 2 and stripped.endswith(":"):
            _record_package(packages, state["current_pkg_key"], state["current_pkg"])
            state["current_pkg_key"] = stripped[:-1]
            state["current_pkg"] = _new_pkg_entry()
            state["current_section"] = None
            continue

        # Skip if we haven't started a package block.
        if state["current_pkg_key"] == None:
            continue

        # Section header inside a package block (indent 4, ends with colon,
        # no value on same line).
        if indent == 4 and stripped.endswith(":") and ":" not in stripped[:-1]:
            section_name = stripped[:-1]
            if section_name in ("resolution", "dependencies", "optionalDependencies", "peerDependencies", "engines"):
                state["current_section"] = section_name
            else:
                state["current_section"] = None
            continue

        # Key-value at indent 4 (package-level field or inline resolution).
        if indent == 4 and ":" in stripped and not stripped.endswith(":"):
            kv_k, _, kv_v = stripped.partition(":")
            kv_k = kv_k.strip()
            kv_v = kv_v.strip().strip("'\"")
            if kv_k == "resolution":
                state["current_pkg"]["resolution"] = _parse_inline_resolution(kv_v)
                state["current_section"] = None
            elif kv_k == "os":
                # e.g. "os: [linux]" or "os: [darwin, linux]" — extract the list items.
                inner = kv_v.strip().strip("[]")
                state["current_pkg"]["os"] = [x.strip() for x in inner.split(",") if x.strip()]
            elif kv_k == "cpu":
                # e.g. "cpu: [x64]" or "cpu: [arm64, x64]"
                inner = kv_v.strip().strip("[]")
                state["current_pkg"]["cpu"] = [x.strip() for x in inner.split(",") if x.strip()]
            elif kv_k not in ("dependencies", "optionalDependencies", "peerDependencies"):
                pass
            continue

        # Key-value at indent 6 (inside a section).
        if indent == 6 and ":" in stripped and state["current_section"]:
            kv_k, _, kv_v = stripped.partition(":")
            kv_k = kv_k.strip()
            kv_v = kv_v.strip().strip("'\"")
            if state["current_section"] in ("dependencies", "optionalDependencies", "peerDependencies"):
                state["current_pkg"][state["current_section"]][kv_k] = kv_v
            elif state["current_section"] == "resolution":
                state["current_pkg"]["resolution"][kv_k] = kv_v
            continue

    # Flush the last package (if we exhausted all lines without hitting a new top-level key).
    if not state["done"]:
        _record_package(packages, state["current_pkg_key"], state["current_pkg"])

    # In pnpm v9, dependency info lives in snapshots:, not packages:.
    # Merge dependency data from the snapshots section into packages.
    snapshots_start = _find_snapshots_start(lines)
    if snapshots_start != -1:
        _parse_snapshots_deps(lines, snapshots_start, packages)

    return {"lockfile_version": lockfile_version, "packages": packages}

# ─── pnpm importers: workspace links and npm aliases ─────────────────────────
#
# `importers:` lists every workspace member by its path relative to the
# lockfile, and for each declared dependency records the specifier the member
# asked for and the version pnpm resolved it to:
#
#   importers:
#     .:
#       dependencies:
#         shared:          {specifier: workspace:*,      version: link:packages/shared}
#         tailwindcss-v3:  {specifier: npm:tailwindcss@3.4.18, version: tailwindcss@3.4.18}
#         zod:             {specifier: ^3.0.0,           version: 3.24.2}
#
# Two of the three forms matter to us, and the `version:` value alone tells
# them apart -- the specifier is never needed:
#
#   link:<path>   a workspace member. The path is relative to the IMPORTER, not
#                 to the lockfile, so it is resolved against the importer path
#                 before it can become a "//<path>:<name>" label.
#   <name>@<ver>  an npm alias: the member imports the package under a name
#                 that is not the package's own. A plain resolved version never
#                 contains "@", so an "@" past index 0 (index 0 being a scope,
#                 as in '@typescript/typescript6@6.0.2') is the discriminator.
#
# Peer suffixes are stripped first: `1.2.3(react@19.0.0)` is a plain version
# whose peer decoration would otherwise look like an alias.
#
# This reader anchors on `importers:` and stops at the next indent-0 line. It
# must stay that way: `catalogs:` is the same nesting idiom one level shallower,
# with catalog names sitting at indent 2 exactly where importer paths do and the
# same `specifier:`/`version:` pairs underneath, so a reader that scanned for
# those pairs instead of anchoring would pull catalog entries in as importers.

def _find_importers_start(lines):
    """Returns the index of the 'importers:' section header, or -1."""
    for idx in range(len(lines)):
        if lines[idx].rstrip() == "importers:":
            return idx
    return -1

def _resolve_link_path(importer_path, link_path):
    """Resolves a pnpm `link:` target into a path relative to the workspace root.

    pnpm records link targets relative to the importer that declares them, so
    importer "npm-packages/email-js" with `link:../../packages/api-utils`
    refers to "packages/api-utils". The root importer is spelled ".".

    Returns "" when the link resolves to the workspace root itself or escapes
    it -- neither can be expressed as a "//<path>:<name>" label.
    """
    segments = []
    for part in (importer_path + "/" + link_path).split("/"):
        if not part or part == ".":
            continue
        if part == "..":
            if not segments:
                return ""
            segments = segments[:-1]
            continue
        segments.append(part)
    return "/".join(segments)

def _strip_peer_suffix(version):
    """Drops a pnpm peer/patch decoration: 'h3@2.0.1(crossws@0.4.5)' -> 'h3@2.0.1'."""
    paren = version.find("(")
    return version[:paren] if paren != -1 else version

def _alias_target(version):
    """Returns the 'name@version' an alias resolves to, or "" for a plain version.

    A resolved version is bare digits and dots; only an alias carries the
    package name. Index 0 is skipped so a scope's '@' is not the one we find.
    """
    version = _strip_peer_suffix(version)
    if len(version) > 1 and version.find("@", 1) != -1:
        return version
    return ""

def _record_importer_dep(result, importer, dep_name, version):
    """Classifies one importer dependency as a workspace link, an alias, or neither."""
    if version.startswith("link:"):
        path = _resolve_link_path(importer, version[len("link:"):])
        if path:
            result["links"][dep_name] = path
        return
    alias = _alias_target(version)
    if alias:
        result["aliases"][dep_name] = alias

def _parse_importers(content):
    """Parses the importers: section into workspace links and npm aliases.

    Returns:
        dict: {
            "links":   {npm_package_name: workspace_rel_path},
            "aliases": {imported_name: "real_name@version"},
        }
    """
    lines = content.split("\n")
    importers_start = _find_importers_start(lines)
    result = {"links": {}, "aliases": {}}
    if importers_start == -1:
        return result

    state = {
        "current_importer": ".",
        "current_section": None,
        "current_dep_name": None,
        "done": False,
    }

    for idx in range(importers_start + 1, len(lines)):
        if state["done"]:
            break

        raw_line = lines[idx].rstrip()
        if not raw_line or raw_line.startswith("#"):
            continue

        indent = _indent_level(raw_line)
        stripped = raw_line.strip()

        if indent == 0:
            state["done"] = True
            continue

        # Importer entry: a path like "." or "packages/shared".
        if indent == 2 and stripped.endswith(":"):
            state["current_importer"] = stripped[:-1].strip().strip("'\"")
            state["current_section"] = None
            state["current_dep_name"] = None
            continue

        if indent == 4 and stripped.endswith(":") and ":" not in stripped[:-1]:
            state["current_section"] = stripped[:-1]
            state["current_dep_name"] = None
            continue

        if state["current_section"] not in ("dependencies", "devDependencies", "optionalDependencies"):
            continue

        if indent == 6:
            # v6 inline form: `shared: {specifier: workspace:*, version: link:packages/shared}`
            if ":" in stripped and "{" in stripped:
                dep_name, _, rest = stripped.partition(":")
                dep_name = dep_name.strip().strip("'\"")
                marker = "version:"
                pos = rest.find(marker)
                if dep_name and pos != -1:
                    value = rest[pos + len(marker):].split("}")[0].split(",")[0].strip().strip("'\"")
                    _record_importer_dep(result, state["current_importer"], dep_name, value)
                state["current_dep_name"] = None
                continue

            # v9 form: the dep name on its own line, specifier/version below it.
            if stripped.endswith(":"):
                state["current_dep_name"] = stripped[:-1].strip().strip("'\"")
                continue

        if indent == 8 and state["current_dep_name"] and ":" in stripped:
            kv_k, _, kv_v = stripped.partition(":")
            if kv_k.strip() == "version":
                _record_importer_dep(
                    result,
                    state["current_importer"],
                    state["current_dep_name"],
                    kv_v.strip().strip("'\""),
                )
            continue

    return result

# ─── patchedDependencies ─────────────────────────────────────────────────────
#
# The one pnpm feature the resolved graph does NOT carry. A patched package's
# `packages:` entry holds the integrity of the untouched registry tarball, so
# fetching it faithfully gets you the UNPATCHED package; only the `snapshots:`
# key records `(patch_hash=...)` and that is decoration, not content.
#
# Two spellings, both keyed by `name@version`:
#
#   patchedDependencies:               patchedDependencies:
#     '@pierre/diffs@1.3.1': <sha256>    '@pierre/diffs@1.3.1':
#                                          hash: <sha256>
#                                          path: patches/...
#
# The hash is the plain sha256 of the patch file's bytes (verified against all
# six patches in the consumer monorepo), which makes it a usable content check
# on the patch we are about to apply -- see npm_import's patch handling. The
# `path:` is deliberately ignored: a lockfile-relative path cannot be turned
# into a label without knowing where Bazel package boundaries fall, so patches
# arrive as real labels from the extension's `patches` attribute instead.

def _find_patched_dependencies_start(lines):
    """Returns the index of the 'patchedDependencies:' header, or -1."""
    for idx in range(len(lines)):
        if lines[idx].rstrip() == "patchedDependencies:":
            return idx
    return -1

def _parse_patched_dependencies(content):
    """Parses patchedDependencies: into {"name@version": sha256_of_patch_file}.

    An entry whose hash is missing maps to "", which callers must read as
    "patched, but nothing to verify the patch file against".
    """
    lines = content.split("\n")
    start = _find_patched_dependencies_start(lines)
    if start == -1:
        return {}

    patched = {}
    state = {"current_key": None, "done": False}

    for idx in range(start + 1, len(lines)):
        if state["done"]:
            break

        raw_line = lines[idx].rstrip()
        if not raw_line or raw_line.strip().startswith("#"):
            continue

        indent = _indent_level(raw_line)
        stripped = raw_line.strip()

        if indent == 0:
            state["done"] = True
            continue

        if indent == 2:
            key, _, value = stripped.partition(":")
            key = key.strip().strip("'\"")
            if not key:
                continue
            state["current_key"] = key
            patched[key] = value.strip().strip("'\"")
            continue

        if indent == 4 and state["current_key"] != None:
            kv_k, _, kv_v = stripped.partition(":")
            if kv_k.strip() == "hash":
                patched[state["current_key"]] = kv_v.strip().strip("'\"")
            continue

    return patched


# ─── npm tarball URL resolution ────────────────────────────────────────────────

_NPM_REGISTRY = "https://registry.npmjs.org"

def _npm_tarball_url(package_name, version, resolution):
    """Returns the tarball URL for an npm package."""
    if "tarball" in resolution:
        return resolution["tarball"]

    if package_name.startswith("@"):
        # Scoped package: @scope/name
        scope, _, name = package_name[1:].partition("/")
        url = "{registry}/@{scope}/{name}/-/{name}-{version}.tgz".format(
            registry = _NPM_REGISTRY,
            scope = scope,
            name = name,
            version = version,
        )
    else:
        url = "{registry}/{name}/-/{name}-{version}.tgz".format(
            registry = _NPM_REGISTRY,
            name = package_name,
            version = version,
        )
    return url

# ─── Label helpers ─────────────────────────────────────────────────────────────

def _package_name_to_label(package_name):
    """Converts an npm package name to a valid Bazel label name component.

    '@types/react' → 'types_react'
    'react-dom'    → 'react-dom'
    """
    name = package_name
    if name.startswith("@"):
        name = name[1:]
    name = name.replace("/", "_")
    return name

def _package_dir_name(package_name, version):
    """Returns the subdirectory name for an extracted package inside the @npm repo.

    '@types/react' + '19.0.0' → 'types_react__19_0_0'
    """
    label = _package_name_to_label(package_name)
    version_clean = version.replace(".", "_").replace("+", "_").replace("-", "_")
    return "{}__{}".format(label, version_clean)

def _resolve_dep_version(packages, dep_name, version_spec):
    """Resolves a dep name + version spec to the concrete version recorded in the lockfile.

    Handles npm aliases: when version_spec is "actual_pkg@version" (e.g.,
    "h3@2.0.1-rc.16" for alias "h3-v2"), look up the actual package directly.
    """
    # Standard case: dep_name@version_spec
    pkg_id = "{}@{}".format(dep_name, version_spec)
    if pkg_id in packages:
        return version_spec

    # Alias case: version_spec is "actual_package@version" (contains @)
    # e.g., dep_name="h3-v2", version_spec="h3@2.0.1-rc.16"
    if "@" in version_spec and version_spec in packages:
        return version_spec

    return None

def _versioned_label_name(base_label, version):
    """Returns the versioned Bazel label name for a package.

    e.g. ("vitest_pretty-format", "3.0.9") → "vitest_pretty-format_3_0_9"
         ("react", "19.1.0")               → "react_19_1_0"

    The base_label is the result of _package_name_to_label, and the version
    component replaces dots and hyphens with underscores to produce a valid
    Bazel label name component.
    """
    version_suffix = version.replace(".", "_").replace("-", "_").replace("+", "_")
    return "{}_{}".format(base_label, version_suffix)

_DIGIT_VALUES = {
    "0": 0,
    "1": 1,
    "2": 2,
    "3": 3,
    "4": 4,
    "5": 5,
    "6": 6,
    "7": 7,
    "8": 8,
    "9": 9,
}

def _semver_parts(v):
    """Parses a semver string into a list of [major, minor, patch, prerelease_flag] integers.

    The 4th element encodes pre-release status per semver spec:
      0 = stable release  (higher precedence)
      1 = pre-release     (lower precedence, e.g. "19.0.0-rc.1 < 19.0.0")
    """
    parts = v.split(".")
    result = []
    for p in parts:
        n = 0
        for c in p.elems():
            if c not in _DIGIT_VALUES:
                break
            n = n * 10 + _DIGIT_VALUES[c]
        result.append(n)
    for _ in range(3 - len(result)):
        result.append(0)
    # 4th element: 1 if any version component contains a '-' (pre-release), 0 otherwise.
    result.append(1 if "-" in v else 0)
    return result

def _semver_gt(a_parts, b_parts):
    """Returns True if semver a_parts is strictly greater than b_parts.

    Compares major, minor, patch numerically, then resolves ties using the
    pre-release flag: stable (0) beats pre-release (1), matching semver spec.
    """
    for i in range(3):
        if a_parts[i] > b_parts[i]:
            return True
        if a_parts[i] < b_parts[i]:
            return False
    # Equal major.minor.patch: compare pre-release flag.
    # a[3]=0 (stable) > a[3]=1 (pre-release), so a is greater when a[3] < b[3].
    if a_parts[3] < b_parts[3]:
        return True
    return False

# ─── Host platform detection ──────────────────────────────────────────────────

def _host_platform(repository_ctx):
    """Returns (host_os, host_cpu) strings matching pnpm's os/cpu convention.

    pnpm os values: linux, darwin, win32, android, freebsd, etc.
    pnpm cpu values: x64, arm64, arm, ia32, ppc64, s390x, loong64, etc.
    """
    os_name = repository_ctx.os.name.lower()
    arch_name = repository_ctx.os.arch.lower()

    if "linux" in os_name:
        host_os = "linux"
    elif "mac" in os_name or "darwin" in os_name:
        host_os = "darwin"
    elif "windows" in os_name:
        host_os = "win32"
    else:
        host_os = os_name

    if "x86_64" in arch_name or "amd64" in arch_name:
        host_cpu = "x64"
    elif "aarch64" in arch_name or "arm64" in arch_name:
        host_cpu = "arm64"
    elif "armv7" in arch_name or "arm" in arch_name:
        host_cpu = "arm"
    elif "i386" in arch_name or "i686" in arch_name or "x86" in arch_name:
        host_cpu = "ia32"
    else:
        host_cpu = arch_name

    return (host_os, host_cpu)

def _pkg_matches_host_platform(pkg, host_os, host_cpu):
    """Returns True if a package's os/cpu constraints match the host platform.

    A package with no constraints always matches.
    A package with only an 'os' constraint matches if that OS matches.
    A package with only a 'cpu' constraint matches if that CPU matches.
    A package with both must match both.
    """
    pkg_os = pkg.get("os", [])
    pkg_cpu = pkg.get("cpu", [])
    if pkg_os and host_os not in pkg_os:
        return False
    if pkg_cpu and host_cpu not in pkg_cpu:
        return False
    return True

def _find_sccs(label_deps_dict):
    """Finds strongly connected components using iterative Kosaraju's algorithm.

    Starlark forbids recursive functions, so both DFS passes use explicit
    work-lists.

    Returns a list of SCCs, where each SCC is a list of label strings.
    Only SCCs with more than one member can contain cycles.
    """
    labels = list(label_deps_dict.keys())
    n = len(labels)

    # ── Pass 1: iterative DFS to compute post-order finish sequence ────────────
    # Stack items are either ("enter", node) or ("finish", node).
    # When we pop ("enter", node): if already visited, skip; otherwise mark
    # visited, push ("finish", node) so it records finish when all children
    # are done, then push ("enter", nb) for each unvisited neighbour.
    finish_order = []
    visited = {}

    for start in labels:
        if start in visited:
            continue
        stack = [("enter", start)]

        # Bound: at most 2 * total_edges + n iterations.
        for _ in range(200000):
            if not stack:
                break
            kind = stack[-1][0]
            node = stack[-1][1]
            stack = stack[:-1]

            if kind == "finish":
                finish_order.append(node)
                continue

            # kind == "enter"
            if node in visited:
                continue
            visited[node] = True
            stack = stack + [("finish", node)]

            for dep in label_deps_dict.get(node, []):
                if dep in label_deps_dict and dep not in visited:
                    stack = stack + [("enter", dep)]

    # ── Build transpose graph ─────────────────────────────────────────────────
    transpose = {}
    for label in labels:
        if label not in transpose:
            transpose[label] = []
    for label in labels:
        for dep in label_deps_dict.get(label, []):
            if dep in label_deps_dict:
                transpose[dep] = transpose.get(dep, []) + [label]

    # ── Pass 2: BFS on transpose in reverse finish order → SCCs ───────────────
    sccs = []
    assigned = {}

    for i in range(len(finish_order) - 1, -1, -1):
        root = finish_order[i]
        if root in assigned:
            continue
        scc = []
        queue = [root]
        assigned[root] = True

        for _ in range(n + 1):
            if not queue:
                break
            node = queue[0]
            queue = queue[1:]
            scc.append(node)
            for nb in transpose.get(node, []):
                if nb not in assigned:
                    assigned[nb] = True
                    queue.append(nb)

        sccs.append(scc)

    return sccs

def _detect_and_break_cycles(label_deps_dict):
    """Removes intra-cycle edges from a label → [dep_labels] dependency graph.

    Uses iterative Kosaraju's algorithm to find strongly connected components
    (SCCs).  Any SCC with more than one member represents a genuine cycle.
    Edges between nodes in the same SCC are removed.

    Edge direction: label_deps_dict[A] contains B means "A depends on B."

    npm packages such as @babel/core and @babel/helper-module-transforms have
    genuine circular peer-dependency references that pnpm resolves at runtime
    using Node's CJS cycle-tolerance.  Bazel's target graph does not permit
    cycles, so we must break them.  Removing intra-cycle edges is safe because
    the type declarations and CommonJS modules are still present on disk in
    each package's directory — only the Bazel dep edges (used for build
    ordering) are removed.

    Args:
        label_deps_dict: dict mapping each label string to a list of label
            strings it depends on.  This dict is mutated in place.

    Returns:
        A list of (from_label, to_label) tuples representing the removed edges.
    """
    sccs = _find_sccs(label_deps_dict)

    # Build a set of cycle nodes: any node that is in a multi-member SCC.
    cycle_nodes = {}
    for scc in sccs:
        if len(scc) > 1:
            for node in scc:
                cycle_nodes[node] = True

    if not cycle_nodes:
        return []

    # Remove all edges between nodes in the same SCC (cycle edges).
    broken_edges = []
    for label in cycle_nodes:
        deps = label_deps_dict.get(label, [])
        new_deps = []
        for dep in deps:
            if dep in cycle_nodes:
                broken_edges.append((label, dep))
            else:
                new_deps.append(dep)
        label_deps_dict[label] = new_deps

    return broken_edges

# ─── Public surface ────────────────────────────────────────────────────────────

parse_pnpm_lock = _parse_pnpm_lock
parse_importers = _parse_importers
parse_patched_dependencies = _parse_patched_dependencies
npm_tarball_url = _npm_tarball_url
package_name_to_label = _package_name_to_label
package_dir_name = _package_dir_name
resolve_dep_version = _resolve_dep_version
versioned_label_name = _versioned_label_name
semver_parts = _semver_parts
semver_gt = _semver_gt
host_platform = _host_platform
pkg_matches_host_platform = _pkg_matches_host_platform
detect_and_break_cycles = _detect_and_break_cycles
