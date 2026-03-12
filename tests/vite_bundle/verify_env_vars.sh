#!/usr/bin/env bash
# verify_env_vars.sh — Assert ts_bundle env_vars substitution works.
#
# Verifies:
#   1. The bundled output contains the literal string "https://api.example.com"
#      (import.meta.env.VITE_API_URL was replaced at bundle time).
#   2. The output does not contain the raw "import.meta.env.VITE_API_URL" token
#      (proving that substitution happened).
#   3. The output does not contain a placeholder bundle marker.

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
  pass "$rel does not contain '$pattern' (good)"
}

# ── env_vars bundle ───────────────────────────────────────────────────────────
# ts_bundle name "entry_vite_env", bundle_name "entry_env", format "esm"
# env_vars = {"VITE_API_URL": "https://api.example.com"}
ENV_JS="tests/vite_bundle/entry_vite_env_bundle/entry_env.es.js"

check_file "$ENV_JS"
check_not_contains "$ENV_JS" "Placeholder bundle"

# The env_var value must be inlined as a literal string.
check_contains "$ENV_JS" "https://api.example.com"

# The raw import.meta.env reference must have been substituted away.
check_not_contains "$ENV_JS" "import.meta.env.VITE_API_URL"

echo "ALL PASSED"
