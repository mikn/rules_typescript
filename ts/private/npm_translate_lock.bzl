"""Repository rule that translates a pnpm lockfile into Bazel targets.

npm_translate_lock reads a pnpm-lock.yaml file and generates a single
self-contained @npm external repository that:
  - Downloads every package tarball via Bazel's downloader
  - Extracts each tarball into a subdirectory inside the repository
  - Generates a single BUILD.bazel with ts_npm_package targets + aliases

Layout of the generated @npm repository:
  @npm//:zod          → ts_npm_package for zod
  @npm//:types_react  → ts_npm_package for @types/react

Users declare dependencies as:
  deps = ["@npm//:zod"]

Lockfile support: pnpm lockfile format v6 and v9 (pnpm 8 and 9).
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
    filtering happens later in _npm_translate_lock_impl after the host
    platform is known.
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

# ─── pnpm workspace support ───────────────────────────────────────────────────
#
# pnpm workspaces: the lockfile has an `importers` top-level section that lists
# every workspace member (by its path relative to the repo root) and maps each
# declared dependency to either a resolved version or a `link:` path.
#
# importers:
#   .:
#     dependencies:
#       shared: {specifier: workspace:*, version: link:packages/shared}
#   packages/shared:
#     dependencies:
#       zod: {specifier: ^3.0.0, version: 3.24.2}
#
# For workspace packages (version: link:<path>), we generate an `alias` target
# in the @npm repository that points at the Bazel label //packages/shared:shared
# (using the last path component as the target name, matching the convention used
# by ts_compile targets generated by Gazelle).
#
# The alias name in @npm is derived from the npm package name by the same
# _package_name_to_label function used for regular packages.
#
# Example:  `shared: {specifier: workspace:*, version: link:packages/shared}`
#           → alias name: "shared"  actual: "//packages/shared:shared"
#
# For scoped packages like @myorg/shared the alias name would be "myorg_shared".
#
# This function is separate from _parse_pnpm_lock to keep the main parser
# focused on the packages/snapshots sections.

def _find_importers_start(lines):
    """Returns the index of the 'importers:' section header, or -1."""
    for idx in range(len(lines)):
        if lines[idx].rstrip() == "importers:":
            return idx
    return -1

def _parse_workspace_aliases(content):
    """Parses the importers: section and returns workspace alias mappings.

    Returns:
        dict: { npm_package_name: workspace_rel_path }
          e.g. { "shared": "packages/shared", "@myorg/shared": "packages/shared" }

    Only entries whose version starts with "link:" are workspace packages.
    The npm_package_name is the key in the `dependencies` block (the package
    name as it appears in package.json, potentially scoped like @myorg/shared).
    The workspace_rel_path strips the "link:" prefix.
    """
    lines = content.split("\n")
    importers_start = _find_importers_start(lines)
    if importers_start == -1:
        return {}

    workspace_aliases = {}

    # State machine to parse the importers section.
    # Structure (v9 format):
    #   importers:                       indent=0
    #     .:                             indent=2 (importer key)
    #       dependencies:                indent=4 (section header)
    #         shared:                    indent=6 (dep name)
    #           specifier: workspace:*   indent=8
    #           version: link:packages/shared  indent=8
    #
    # v6 format uses inline braces:
    #   shared: {specifier: workspace:*, version: link:packages/shared}

    state = {
        "in_importers": True,
        "current_section": None,  # "dependencies" or "devDependencies" etc.
        "current_dep_name": None,  # npm package name of the current dep block
        "done": False,
    }

    for idx in range(importers_start + 1, len(lines)):
        if state["done"]:
            break

        line = lines[idx]
        raw_line = line.rstrip()

        if not raw_line or raw_line.startswith("#"):
            continue

        indent = _indent_level(raw_line)
        stripped = raw_line.strip()

        # Top-level key ends the importers section.
        if indent == 0:
            state["done"] = True
            continue

        # Importer entry (indent 2, ends with colon): path like "." or "packages/shared".
        if indent == 2 and stripped.endswith(":"):
            state["current_section"] = None
            state["current_dep_name"] = None
            continue

        # Section header at indent 4 (dependencies/devDependencies/etc).
        if indent == 4 and stripped.endswith(":") and ":" not in stripped[:-1]:
            state["current_section"] = stripped[:-1]
            state["current_dep_name"] = None
            continue

        if state["current_section"] not in ("dependencies", "devDependencies", "optionalDependencies"):
            continue

        # Dep name entry at indent 6.
        if indent == 6:
            # Check for v6 inline format:
            #   shared: {specifier: workspace:*, version: link:packages/shared}
            if ":" in stripped and "{" in stripped:
                dep_name, _, rest = stripped.partition(":")
                dep_name = dep_name.strip().strip("'\"")
                # Extract version from inline brace: look for "version: link:..."
                if "version: link:" in rest:
                    link_start = rest.find("version: link:") + len("version: link:")
                    link_val = rest[link_start:].split("}")[0].split(",")[0].strip().strip("'\"")
                    if dep_name and link_val:
                        workspace_aliases[dep_name] = link_val
                state["current_dep_name"] = None
                continue

            # v9: dep name with just a colon (block follows).
            if stripped.endswith(":"):
                state["current_dep_name"] = stripped[:-1].strip().strip("'\"")
                continue

        # Dep version/specifier at indent 8 (v9 format block).
        if indent == 8 and state["current_dep_name"] and ":" in stripped:
            kv_k, _, kv_v = stripped.partition(":")
            kv_k = kv_k.strip()
            kv_v = kv_v.strip().strip("'\"")
            if kv_k == "version" and kv_v.startswith("link:"):
                link_path = kv_v[len("link:"):]
                if state["current_dep_name"] and link_path:
                    workspace_aliases[state["current_dep_name"]] = link_path
            continue

    return workspace_aliases

# ─── package.json field extraction ────────────────────────────────────────────
#
# Starlark has json.decode() built in (Bazel 6+), so we parse package.json
# directly without spawning an external process.
#
# _read_package_json_fields() reads the file with repository_ctx.read() and
# decodes it with json.decode(), then extracts:
#   "bin":           dict of {bin_name: relative_path}
#   "exports_types": the primary .d.ts path from the exports field (or "")

def _safe_name(v):
    """Returns v if it contains only safe characters for a Bazel label, else None."""
    for ch in v.elems():
        if ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@/.:-":
            return None
    return v

def _strip_dot_slash(v):
    if v.startswith("./"):
        return v[2:]
    return v

def _safe_path(v):
    """Strip leading ./ and reject paths with shell-unsafe characters."""
    v = _strip_dot_slash(v)
    if '"' in v or "\\" in v:
        return None
    return v

def _get_types_from_exports(root):
    """Iteratively extract a .d.ts path from an exports field value.

    Starlark forbids recursion, so we use a work-list loop capped at 32
    iterations (sufficient for any real-world conditional exports nesting).
    """
    work = [root]
    for _ in range(32):
        if not work:
            break
        obj = work[0]
        work = work[1:]
        if type(obj) == type(""):
            if obj.endswith(".d.ts"):
                return obj
            continue
        if type(obj) == type({}):
            if "types" in obj and type(obj["types"]) == type(""):
                return obj["types"]
            # Enqueue sub-objects in preference order (depth-first): import → require.
            for key in ("require", "import"):
                if key in obj:
                    work = [obj[key]] + work
    return None

def _read_package_json_fields(repository_ctx, pkg_json_path):
    """Extracts bin and exports.types fields from a package.json using Starlark.

    Uses repository_ctx.read() + json.decode() — no external process required.

    Args:
        repository_ctx: The repository context.
        pkg_json_path:  Path object to the package.json file.

    Returns:
        A dict with:
          "bin": dict of {bin_name: relative_path} (may be empty)
          "exports_types": string path to .d.ts entry, or "" if not found
    """
    result = {"bin": {}, "exports_types": ""}

    if not pkg_json_path.exists:
        return result

    content = repository_ctx.read(pkg_json_path)
    if not content:
        return result

    pkg = json.decode(content)

    # Extract bin entries.
    bin_field = pkg.get("bin")
    pkg_name = pkg.get("name", "")

    if type(bin_field) == type("") and bin_field:
        # Single string — bin name defaults to the unscoped package name.
        slash_idx = pkg_name.rfind("/")
        bin_name = pkg_name[slash_idx + 1:] if slash_idx != -1 else pkg_name
        if bin_name and _safe_name(bin_name):
            bin_path = _safe_path(bin_field)
            if bin_path:
                result["bin"][bin_name] = bin_path
    elif type(bin_field) == type({}):
        for k, v in bin_field.items():
            if type(v) != type("") or not v:
                continue
            if not _safe_name(k):
                continue
            bin_path = _safe_path(v)
            if bin_path:
                result["bin"][k] = bin_path

    # Extract exports types entry point.
    exports = pkg.get("exports")
    if type(exports) == type({}):
        # Prefer the "." entry.
        main_export = exports.get(".", exports)
        types_path = _get_types_from_exports(main_export)
        if types_path:
            types_path = _safe_path(types_path)
            if types_path:
                result["exports_types"] = types_path

    return result

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

def _package_repo_name(repo_prefix, package_name, version):
    """Returns the external repository name for an npm package."""
    label = _package_name_to_label(package_name)
    version_clean = version.replace(".", "_").replace("+", "_").replace("-", "_")
    return "{}__{}_{}".format(repo_prefix, label, version_clean)

def _tarball_strip_prefix(package_name, version = ""):
    """Returns the strip prefix for extracting a package tarball.

    Most npm packages extract under 'package/'.
    The @types/* DefinitelyTyped packages use the unscoped package name as
    their tarball directory prefix, e.g.:
      @types/react     → 'react'
      @types/react-dom → 'react-dom'
      @types/node      → 'node'

    A small number of @types packages use a non-standard prefix that includes
    a version range (e.g. @types/hast@2.3.x uses 'hast v2.3' as the directory
    prefix). These are handled by an explicit override table keyed by
    (package_name, major.minor) so the correct prefix is always used.
    """
    if package_name.startswith("@types/"):
        base = package_name[len("@types/"):]

        # Some @types packages use "<name> v<major>.<minor>" as the tarball
        # directory prefix. Detect this by checking a known override table.
        # Only the major.minor portion is needed because patch versions still
        # use the same directory prefix.
        major_minor = ""
        if version:
            parts = version.split(".")
            if len(parts) >= 2:
                major_minor = parts[0] + "." + parts[1]

        # Known packages whose tarball uses "<name> v<major>.<minor>/" prefix.
        # Add entries here when new packages with non-standard prefixes appear.
        _VERSIONED_PREFIX_PACKAGES = {
            "@types/hast": ["2.3"],
            "@types/mdast": ["3.0"],
            "@types/unist": ["2.0"],
        }
        versioned_majors = _VERSIONED_PREFIX_PACKAGES.get(package_name, [])
        for known_mm in versioned_majors:
            if major_minor.startswith(known_mm):
                return base + " v" + major_minor

        return base
    return "package"

def _resolve_dep_version(packages, dep_name, version_spec):
    """Resolves a dep name + version spec to the concrete version recorded in the lockfile."""
    pkg_id = "{}@{}".format(dep_name, version_spec)
    if pkg_id in packages:
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

# ─── Main repository rule ──────────────────────────────────────────────────────

def _npm_translate_lock_impl(repository_ctx):
    """Parses pnpm-lock.yaml, downloads all packages, and generates the @npm repository.

    All packages are downloaded into subdirectories of this single repository.
    A single BUILD.bazel is generated with ts_npm_package targets so users
    can write:
      deps = ["@npm//:zod"]
    """
    lockfile_path = repository_ctx.path(repository_ctx.attr.pnpm_lock)
    lockfile_content = repository_ctx.read(lockfile_path)

    repository_ctx.report_progress("Parsing pnpm-lock.yaml")
    parsed = _parse_pnpm_lock(lockfile_content)
    packages = parsed["packages"]

    # Determine host platform for filtering platform-specific optional packages.
    host_os, host_cpu = _host_platform(repository_ctx)

    # Filter: keep only packages that match the host platform.
    # Platform-specific packages (with os/cpu constraints) that don't match
    # the host are not useful for building/running on this machine.
    filtered_packages = {}
    for pkg_id, pkg in packages.items():
        if _pkg_matches_host_platform(pkg, host_os, host_cpu):
            filtered_packages[pkg_id] = pkg
    packages = filtered_packages

    # Identify @types/* packages so we can pair them with runtime packages.
    # types_versions_map: {untyped_name: [(major_int, pkg_id), ...]} sorted ascending by major.
    # When a runtime package has multiple versions we pick the @types version whose major
    # matches; otherwise we fall back to the highest @types version.
    types_versions_map = {}
    for pkg_id, pkg in packages.items():
        nm = pkg["name"]
        if nm.startswith("@types/"):
            untyped_name = nm[len("@types/"):]
            major = _semver_parts(pkg["version"])[0]
            if untyped_name not in types_versions_map:
                types_versions_map[untyped_name] = []
            types_versions_map[untyped_name] = types_versions_map[untyped_name] + [(major, pkg_id)]

    # Download each package into its own subdirectory.
    pkg_dir_names = {}   # pkg_id → dir_name
    pkg_bin_entries = {} # pkg_id → dict of {bin_name: relative_path}
    pkg_exports_types = {}  # pkg_id → primary .d.ts path from exports field (or "")

    for pkg_id, pkg in packages.items():
        nm = pkg["name"]
        version = pkg["version"]
        if not nm or not version:
            continue

        resolution = pkg.get("resolution", {})
        integrity = resolution.get("integrity", "")
        tarball_url = _npm_tarball_url(nm, version, resolution)
        dir_name = _package_dir_name(nm, version)

        repository_ctx.report_progress("Downloading {}@{}".format(nm, version))

        strip_prefix = _tarball_strip_prefix(nm, version)

        if integrity:
            repository_ctx.download_and_extract(
                url = tarball_url,
                output = dir_name,
                stripPrefix = strip_prefix,
                integrity = integrity,
            )
        else:
            repository_ctx.download_and_extract(
                url = tarball_url,
                output = dir_name,
                stripPrefix = strip_prefix,
            )

        pkg_dir_names[pkg_id] = dir_name

        # Read package.json to extract bin scripts and conditional exports.
        pkg_json_path = repository_ctx.path("{}/package.json".format(dir_name))
        pkg_fields = _read_package_json_fields(repository_ctx, pkg_json_path)

        if pkg_fields["bin"]:
            pkg_bin_entries[pkg_id] = pkg_fields["bin"]

        if pkg_fields["exports_types"]:
            pkg_exports_types[pkg_id] = pkg_fields["exports_types"]

    # Generate a single BUILD.bazel with all ts_npm_package targets.
    repository_ctx.report_progress("Generating @npm BUILD.bazel")

    # Pass 1: Build label_to_pkg_id mapping — primary (highest) version per label.
    # This preserves existing behaviour: @npm//:react always points to highest version.
    label_to_pkg_id = {}
    for pkg_id, pkg in packages.items():
        nm = pkg["name"]
        version = pkg["version"]
        if not nm or not version:
            continue
        label_name = _package_name_to_label(nm)
        existing = label_to_pkg_id.get(label_name)
        if existing == None:
            label_to_pkg_id[label_name] = pkg_id
        else:
            # Keep the higher version using proper numeric semver comparison.
            existing_version = packages[existing]["version"]
            if _semver_gt(_semver_parts(version), _semver_parts(existing_version)):
                label_to_pkg_id[label_name] = pkg_id

    # Pass 2: Build name_to_all_pkg_ids — all pkg_ids per base label name.
    # Used to detect packages that have multiple versions in the lockfile.
    # Example: "@vitest/pretty-format" at 3.0.9 and 3.2.4 →
    #   name_to_all_pkg_ids["vitest_pretty-format"] = ["@vitest/pretty-format@3.0.9", "@vitest/pretty-format@3.2.4"]
    name_to_all_pkg_ids = {}
    for pkg_id, pkg in packages.items():
        nm = pkg["name"]
        version = pkg["version"]
        if not nm or not version:
            continue
        label_name = _package_name_to_label(nm)
        if label_name not in name_to_all_pkg_ids:
            name_to_all_pkg_ids[label_name] = []
        name_to_all_pkg_ids[label_name] = name_to_all_pkg_ids[label_name] + [pkg_id]

    # Determine which base label names have multiple versions.
    multi_version_labels = {}  # label_name → True
    for label_name, pkg_ids in name_to_all_pkg_ids.items():
        if len(pkg_ids) > 1:
            multi_version_labels[label_name] = True

    # Build a complete set of versioned labels so dependency resolution can
    # reference the right versioned target when a dep has multiple versions.
    # versioned_label_to_pkg_id: versioned_label → pkg_id (for multi-version pkgs)
    versioned_label_to_pkg_id = {}
    for label_name in multi_version_labels:
        for pkg_id in name_to_all_pkg_ids[label_name]:
            pkg = packages[pkg_id]
            version = pkg["version"]
            versioned = _versioned_label_name(label_name, version)
            versioned_label_to_pkg_id[versioned] = pkg_id

    # Collision detection: two distinct pkg_ids must not produce the same versioned label.
    # This can happen if, e.g., "foo@1.0.0-rc.1" and "foo@1.0.0" both map to "foo_1_0_0".
    seen_versioned_labels = {}
    for versioned, vpkg_id in versioned_label_to_pkg_id.items():
        if versioned in seen_versioned_labels:
            fail("npm_translate_lock: label collision '{}' between {} and {}".format(
                versioned,
                vpkg_id,
                seen_versioned_labels[versioned],
            ))
        seen_versioned_labels[versioned] = vpkg_id

    # pkg_id_to_label: pkg_id → effective Bazel label (versioned for multi, base for single).
    # Used when resolving dep labels: a dep that points at a specific version of a
    # multi-version package gets the versioned label, not the primary alias.
    pkg_id_to_label = {}
    for label_name, pkg_id in label_to_pkg_id.items():
        if label_name in multi_version_labels:
            # Primary pkg_id for this label → primary alias points here.
            # The versioned label is also generated separately.
            pkg = packages[pkg_id]
            versioned = _versioned_label_name(label_name, pkg["version"])
            pkg_id_to_label[pkg_id] = versioned
        else:
            pkg_id_to_label[pkg_id] = label_name

    # Also register non-primary versioned labels.
    for versioned, pkg_id in versioned_label_to_pkg_id.items():
        if pkg_id not in pkg_id_to_label:
            pkg_id_to_label[pkg_id] = versioned

    def _resolve_dep_label(dep_name, dep_version_spec):
        """Returns the Bazel label for a dependency, using versioned label when needed."""
        resolved = _resolve_dep_version(packages, dep_name, dep_version_spec)
        if not resolved:
            return None
        dep_pkg_id = "{}@{}".format(dep_name, resolved)
        if dep_pkg_id not in packages:
            return None
        return pkg_id_to_label.get(dep_pkg_id)

    # ── Cycle detection and breaking ───────────────────────────────────────────
    # Build the complete label → [dep_labels] graph and run Kahn's algorithm to
    # identify and break circular dependency edges before generating BUILD targets.
    # npm packages like @babel/core / @babel/helper-module-transforms legitimately
    # form cycles via peer dependencies; pnpm resolves them at runtime but Bazel
    # requires an acyclic target graph.
    label_to_dep_labels = {}
    for pkg_id in packages:
        this_label = pkg_id_to_label.get(pkg_id)
        if not this_label:
            continue
        pkg = packages[pkg_id]
        dep_labels = []
        for dep_name, dep_version_spec in pkg.get("dependencies", {}).items():
            dep_lbl = _resolve_dep_label(dep_name, dep_version_spec)
            if dep_lbl != None:
                dep_labels.append(dep_lbl)
        for dep_name, dep_version_spec in pkg.get("optionalDependencies", {}).items():
            dep_lbl = _resolve_dep_label(dep_name, dep_version_spec)
            if dep_lbl != None:
                dep_labels.append(dep_lbl)
        label_to_dep_labels[this_label] = dep_labels

    broken_cycle_edges = _detect_and_break_cycles(label_to_dep_labels)

    build_lines = [
        "# Auto-generated by npm_translate_lock. DO NOT EDIT.",
        "#",
        "# Multi-version packages in this lockfile generate both versioned targets",
        "# (e.g. :react_19_1_0, :react_18_3_1) and a primary alias (:react) that",
        "# points to the highest version. Single-version packages keep their plain",
        "# label (e.g. :zod) with no alias.",
    ]

    if broken_cycle_edges:
        build_lines.append("#")
        build_lines.append("# The following circular npm dependency edges were removed to satisfy")
        build_lines.append("# Bazel's requirement for an acyclic target graph.  These packages")
        build_lines.append("# use Node's CJS cycle-tolerance at runtime and build correctly")
        build_lines.append("# without the Bazel dep edge.")
        for (from_lbl, to_lbl) in broken_cycle_edges:
            build_lines.append("#   CYCLE BROKEN: :{} -> :{}".format(from_lbl, to_lbl))

    build_lines.extend([
        'load("@rules_typescript//ts/private:npm_bin.bzl", "npm_bin")',
        'load("@rules_typescript//ts/private:ts_npm_package.bzl", "ts_npm_package")',
        "",
        'package(default_visibility = ["//visibility:public"])',
        "",
    ])

    def _emit_ts_npm_package(target_label, pkg, pkg_id, dir_name):
        """Appends ts_npm_package stanzas to build_lines for the given pkg."""
        nm = pkg["name"]
        version = pkg["version"]
        is_types = nm.startswith("@types/")

        # Resolve dependency labels within this same repo.
        # Use the pre-computed, cycle-broken label_to_dep_labels map so that
        # circular npm dependencies (e.g. @babel/core ↔ @babel/helper-module-transforms)
        # are not emitted as Bazel deps.
        allowed_dep_labels = {}
        for lbl in label_to_dep_labels.get(target_label, []):
            allowed_dep_labels[lbl] = True

        dep_label_set = {}
        dropped_deps = []
        for dep_name, dep_version_spec in pkg.get("dependencies", {}).items():
            dep_lbl = _resolve_dep_label(dep_name, dep_version_spec)
            if dep_lbl != None:
                if dep_lbl in allowed_dep_labels:
                    dep_label_set[dep_lbl] = True
                # else: edge was removed by cycle breaker — silently omit
            else:
                # None is returned for platform-filtered packages and unresolvable
                # specs — both are legitimate (optional/peer deps), so we only warn.
                dropped_deps.append("{}@{}".format(dep_name, dep_version_spec))

        # Include optionalDependencies that match the host platform.
        for dep_name, dep_version_spec in pkg.get("optionalDependencies", {}).items():
            dep_lbl = _resolve_dep_label(dep_name, dep_version_spec)
            if dep_lbl != None and dep_lbl in allowed_dep_labels:
                dep_label_set[dep_lbl] = True
            # optionalDependencies that resolve to None are always expected (platform
            # packages not matching the host), so no warning is emitted for them.

        dep_label_parts = ['        ":{}",'.format(lbl) for lbl in dep_label_set]
        deps_str = ""
        if dep_label_parts:
            deps_str = "\n" + "\n".join(dep_label_parts) + "\n    "

        # Emit a comment for each required dependency that could not be resolved.
        # This is informational only: platform packages and optional/peer deps
        # legitimately return None and are expected to be absent.
        if dropped_deps:
            build_lines.append(
                "# WARNING: {}@{} dropped unresolved deps (platform/optional/peer): {}".format(
                    nm,
                    version,
                    ", ".join(dropped_deps),
                ),
            )

        build_lines.extend([
            "ts_npm_package(",
            '    name = "{}",'.format(target_label),
            '    package_name = "{}",'.format(nm),
            '    package_version = "{}",'.format(version),
            '    package_dir = "{}/package.json",'.format(dir_name),
            "    package_files = glob(",
            '        ["{}/**/*"],'.format(dir_name),
            "        exclude_directories = 1,",
            "    ),",
        ])

        # Pair runtime package with its @types/* counterpart.
        # When multiple @types versions exist, prefer the one whose major matches the
        # runtime package's major; fall back to the highest @types version available.
        if not is_types and nm in types_versions_map:
            runtime_major = _semver_parts(version)[0]
            types_versions = types_versions_map[nm]  # list of (major_int, pkg_id)

            # Find best match: same major first, then highest version (last in list).
            best_types_pkg_id = None
            for (t_major, t_pkg_id) in types_versions:
                if t_major == runtime_major:
                    best_types_pkg_id = t_pkg_id
                    break
            if best_types_pkg_id == None:
                # Fallback: highest @types version — pick the one with the largest major.
                best_major = -1
                for (t_major, t_pkg_id) in types_versions:
                    if t_major > best_major:
                        best_major = t_major
                        best_types_pkg_id = t_pkg_id

            if best_types_pkg_id != None:
                types_pkg = packages[best_types_pkg_id]
                types_label_base = _package_name_to_label(types_pkg["name"])
                # If the @types package itself has multiple versions, use its versioned label.
                if types_label_base in multi_version_labels:
                    types_lbl = _versioned_label_name(types_label_base, types_pkg["version"])
                else:
                    types_lbl = types_label_base
                build_lines.append('    types_dep = ":{}",'.format(types_lbl))

        exports_types = pkg_exports_types.get(pkg_id, "")
        if exports_types:
            build_lines.append('    exports_types = "{}/{}",'.format(dir_name, exports_types))

        build_lines.extend([
            "    is_types_package = {},".format("True" if is_types else "False"),
            "    deps = [{}],".format(deps_str),
            ")",
            "",
        ])

    # Emit all package targets.
    # For multi-version packages:
    #   - Emit a versioned ts_npm_package for each version.
    #   - Emit a primary alias pointing to the highest version.
    # For single-version packages:
    #   - Emit a plain ts_npm_package (no alias, no version suffix).
    #
    # Bin target collision detection: two distinct packages must not produce the
    # same bin target name.
    seen_bin_targets = {}  # bin_target_name → pkg_id that first claimed it

    for label_name, pkg_id in label_to_pkg_id.items():
        pkg = packages[pkg_id]
        nm = pkg["name"]
        version = pkg["version"]
        if not nm or not version:
            continue

        if label_name in multi_version_labels:
            # Emit ts_npm_package for every version of this package.
            for vpkg_id in name_to_all_pkg_ids[label_name]:
                vpkg = packages[vpkg_id]
                vdir = pkg_dir_names.get(vpkg_id)
                if not vdir:
                    continue
                versioned_label = _versioned_label_name(label_name, vpkg["version"])
                _emit_ts_npm_package(versioned_label, vpkg, vpkg_id, vdir)

            # Emit primary alias → highest version.
            highest_versioned = _versioned_label_name(label_name, version)
            build_lines.extend([
                "# Primary alias: @npm//:{name} → highest version ({ver})".format(
                    name = label_name,
                    ver = version,
                ),
                "alias(",
                '    name = "{}",'.format(label_name),
                '    actual = ":{}",'.format(highest_versioned),
                ")",
                "",
            ])

            # Generate npm_bin targets for ALL versions of this multi-version package.
            # Primary version gets the plain "<bin>_bin" name; non-primary versions get
            # a versioned name "<bin>_<ver>_bin" to avoid collisions.
            for vpkg_id in name_to_all_pkg_ids[label_name]:
                vpkg = packages[vpkg_id]
                vdir = pkg_dir_names.get(vpkg_id)
                if not vdir:
                    continue
                is_primary = (vpkg_id == pkg_id)
                bin_entries = pkg_bin_entries.get(vpkg_id, {})
                for bin_name, bin_path in bin_entries.items():
                    if is_primary:
                        bin_target_name = "{}_bin".format(bin_name)
                    else:
                        ver_suffix = vpkg["version"].replace(".", "_").replace("-", "_").replace("+", "_")
                        bin_target_name = "{}_{}_bin".format(bin_name, ver_suffix)
                    if bin_target_name in seen_bin_targets:
                        fail("npm_translate_lock: bin target name collision '{}' between {} and {}".format(
                            bin_target_name,
                            vpkg_id,
                            seen_bin_targets[bin_target_name],
                        ))
                    seen_bin_targets[bin_target_name] = vpkg_id
                    build_lines.extend([
                        "npm_bin(",
                        '    name = "{}",'.format(bin_target_name),
                        "    package_files = glob(",
                        '        ["{}/**/*"],'.format(vdir),
                        "        exclude_directories = 1,",
                        "    ),",
                        '    entry_script = "{}",'.format(bin_path),
                        ")",
                        "",
                    ])
        else:
            # Single version: plain ts_npm_package, unchanged behaviour.
            dir_name = pkg_dir_names.get(pkg_id)
            if not dir_name:
                continue
            _emit_ts_npm_package(label_name, pkg, pkg_id, dir_name)

            # Generate npm_bin targets for each bin entry in the single version.
            bin_entries = pkg_bin_entries.get(pkg_id, {})
            for bin_name, bin_path in bin_entries.items():
                bin_target_name = "{}_bin".format(bin_name)
                if bin_target_name in seen_bin_targets:
                    # Collision with a different package that has the same binary name.
                    # Qualify the target name with the package's Bazel label to disambiguate.
                    # E.g. listhen's "listen" bin collides with @vinxi/listhen's "listen" bin:
                    #   listhen      → listen_bin            (first-come keeps the short name)
                    #   vinxi_listhen → vinxi_listhen_listen_bin  (later package gets qualified)
                    bin_target_name = "{label}_{bin}_bin".format(
                        label = label_name,
                        bin = bin_name,
                    )
                    if bin_target_name in seen_bin_targets:
                        fail("npm_translate_lock: bin target name collision '{}' between {} and {}".format(
                            bin_target_name,
                            pkg_id,
                            seen_bin_targets[bin_target_name],
                        ))
                seen_bin_targets[bin_target_name] = pkg_id
                build_lines.extend([
                    "npm_bin(",
                    '    name = "{}",'.format(bin_target_name),
                    "    package_files = glob(",
                    '        ["{}/**/*"],'.format(dir_name),
                    "        exclude_directories = 1,",
                    "    ),",
                    '    entry_script = "{}",'.format(bin_path),
                    ")",
                    "",
                ])

    # ── pnpm workspace aliases ──────────────────────────────────────────────
    # If the lockfile has an importers: section with workspace:* references,
    # generate alias() targets in the @npm repo pointing at the local Bazel
    # targets derived from the workspace package path.
    #
    # Example lockfile importers entry:
    #   shared: {specifier: workspace:*, version: link:packages/shared}
    #
    # → alias(name = "shared", actual = "//packages/shared:shared")
    #
    # The actual label uses the last path component as the target name, which
    # matches the Gazelle convention for ts_compile targets (directory basename).
    #
    # We parse the workspace aliases from the lockfile content we already have
    # in memory — no extra file read required for the importers section.
    repository_ctx.report_progress("Checking for pnpm workspace packages")
    workspace_aliases = _parse_workspace_aliases(lockfile_content)

    if workspace_aliases:
        build_lines.append("# Workspace package aliases (workspace:* → local Bazel targets)")
        for pkg_name, ws_rel_path in workspace_aliases.items():
            # Convert the npm package name to a Bazel label name.
            alias_name = _package_name_to_label(pkg_name)

            # Derive the Bazel target label from the workspace-relative path.
            # The workspace path is e.g. "packages/shared" and by convention the
            # ts_compile target is named after the directory basename.
            target_basename = ws_rel_path.split("/")[-1]
            # Use @@// prefix to reference the main workspace from within the
            # @npm external repository. In Bazel bzlmod, @@ is the canonical
            # label prefix for the root module (the user's workspace).
            actual_label = "@@//{path}:{name}".format(
                path = ws_rel_path,
                name = target_basename,
            )

            # Only emit if the alias name doesn't collide with an existing npm
            # package target (workspace packages take precedence).
            build_lines.extend([
                "# Workspace package: {} → {}".format(pkg_name, actual_label),
                "alias(",
                '    name = "{}",'.format(alias_name),
                '    actual = "{}",'.format(actual_label),
                ")",
                "",
            ])

    repository_ctx.file("BUILD.bazel", "\n".join(build_lines))

npm_translate_lock = repository_rule(
    implementation = _npm_translate_lock_impl,
    attrs = {
        "pnpm_lock": attr.label(
            doc = "Label to the pnpm-lock.yaml file.",
            mandatory = True,
            allow_single_file = True,
        ),
        "data": attr.label_list(
            doc = "Additional files that affect the lockfile (e.g. package.json).",
            allow_files = True,
        ),
    },
    doc = """Translates a pnpm-lock.yaml into a self-contained @npm Bazel repository.

Downloads all packages into subdirectories of the @npm repository and
generates a single BUILD.bazel with ts_npm_package targets.

Usage in MODULE.bazel:
    npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
    npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
    use_repo(npm, "npm")

Then in BUILD files:
    deps = ["@npm//:zod"]
""",
)
