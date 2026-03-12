#!/usr/bin/env bash
# verify_vite_config_injection.sh — Assert that vite_config attr injects user plugins.
#
# Verifies:
#   1. The bundle output exists (Vite ran successfully with the user config).
#   2. The bundle contains the sentinel comment injected by mock_plugin.mjs
#      (_VITE_PLUGIN_INJECTED), proving the user-supplied plugin ran.
#   3. The bundle also contains the original source content (Bazel integration
#      was not broken — resolve.alias, outDir, etc. still work).

set -euo pipefail

RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"
if [[ -z "$RUNFILES" ]]; then
  echo "FAIL: RUNFILES_DIR and TEST_SRCDIR are both unset" >&2
  exit 1
fi

WORKSPACE="${TEST_WORKSPACE:-_main}"
BASE="$RUNFILES/$WORKSPACE"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

check_file() {
  local rel="$1"
  local full="$BASE/$rel"
  if [[ ! -f "$full" ]]; then
    fail "$rel not found at $full"
  fi
  pass "$rel exists"
}

check_contains() {
  local rel="$1"
  local pattern="$2"
  local full="$BASE/$rel"
  if ! grep -q "$pattern" "$full"; then
    fail "$rel does not contain pattern '$pattern' (content: $(head -5 "$full"))"
  fi
  pass "$rel contains '$pattern'"
}

check_not_contains() {
  local rel="$1"
  local pattern="$2"
  local full="$BASE/$rel"
  if grep -q "$pattern" "$full"; then
    fail "$rel should NOT contain '$pattern'"
  fi
  pass "$rel does not contain '$pattern' (expected)"
}

# ── Lib-mode bundle with vite_config ──────────────────────────────────────────
# ts_bundle name "entry_vite_with_config", bundle_name "entry", format "esm"
# The mock_plugin.mjs injects "/* _VITE_PLUGIN_INJECTED */" via renderChunk.
LIB_JS="tests/vite_bundle/entry_vite_with_config_bundle/entry.es.js"

check_file "$LIB_JS"

# The mock plugin's renderChunk hook must have injected the sentinel.
check_contains "$LIB_JS" "_VITE_PLUGIN_INJECTED"

# The original source content must still be present (Bazel integration intact).
check_contains "$LIB_JS" "add"
check_contains "$LIB_JS" "PI"

# Must NOT be a placeholder.
check_not_contains "$LIB_JS" "Placeholder bundle"

# ── App-mode bundle with vite_config ──────────────────────────────────────────
# ts_bundle name "entry_vite_app_with_config", mode = "app"
# Same mock plugin should inject sentinel into at least one output JS chunk.
APP_DIR="tests/vite_bundle/entry_vite_app_with_config_bundle"

if [[ ! -d "$BASE/$APP_DIR" ]]; then
  fail "directory $APP_DIR not found at $BASE/$APP_DIR"
fi
pass "directory $APP_DIR exists"

# Find all .js files in the app output directory.
JS_FILES=()
while IFS= read -r -d '' f; do
  JS_FILES+=("$f")
done < <(find "$BASE/$APP_DIR" -name "*.js" -print0 2>/dev/null)

if [[ "${#JS_FILES[@]}" -lt 1 ]]; then
  fail "directory $APP_DIR contains no .js files"
fi
pass "directory $APP_DIR contains ${#JS_FILES[@]} .js file(s)"

# At least one JS chunk must contain the sentinel.
found_sentinel=0
for js_file in "${JS_FILES[@]}"; do
  if grep -q "_VITE_PLUGIN_INJECTED" "$js_file"; then
    found_sentinel=1
    pass "sentinel _VITE_PLUGIN_INJECTED found in $(basename "$js_file")"
    break
  fi
done

if [[ "$found_sentinel" -eq 0 ]]; then
  fail "sentinel _VITE_PLUGIN_INJECTED not found in any .js file in $APP_DIR"
fi

echo "ALL PASSED"
