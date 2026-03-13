#!/usr/bin/env bash
# verify_staging_srcs.sh — Assert that staging_srcs correctly sets up the staging dir.
#
# Verifies:
#   1. The bundle output exists (Vite ran successfully with staging_srcs).
#   2. VITE_STAGING_ROOT was set (staging_mock_plugin injects sentinel).
#   3. The bundle still contains original source content (Bazel integration intact).
#   4. Must NOT be a placeholder bundle.

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
    fail "$rel does not contain pattern '$pattern' (first 5 lines: $(head -5 "$full" 2>/dev/null || echo '(unreadable)'))"
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

# ── Lib-mode bundle with staging_srcs ─────────────────────────────────────────
# ts_bundle name "entry_vite_staging", bundle_name "entry", format "esm"
LIB_JS="tests/vite_bundle/entry_vite_staging_bundle/entry.es.js"

check_file "$LIB_JS"

# The staging mock plugin must have injected the staging-root sentinel.
check_contains "$LIB_JS" "_STAGING_ROOT_WAS_SET = true"

# The original source content must still be present (Bazel integration intact).
check_contains "$LIB_JS" "add"
check_contains "$LIB_JS" "PI"

# Must NOT be a placeholder.
check_not_contains "$LIB_JS" "Placeholder bundle"

echo "ALL PASSED"
