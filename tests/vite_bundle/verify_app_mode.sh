#!/usr/bin/env bash
# verify_app_mode.sh — Assert ts_bundle mode="app" produces an HTML application.
#
# Verifies:
#   1. The output is a directory (not a single JS file).
#   2. The directory contains at least one .html file.
#   3. The directory contains at least one .js file.
#   4. The output is not a placeholder concatenation.

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

check_dir() {
  local rel="$1"
  local full="$BASE/$rel"
  if [[ ! -d "$full" ]]; then
    fail "directory $rel not found at $full"
  fi
  pass "directory $rel exists"
}

check_dir_has_file() {
  local rel="$1"
  local glob="$2"
  local full="$BASE/$rel"
  local count
  count=$(find "$full" -name "$glob" 2>/dev/null | wc -l)
  if [[ "$count" -lt 1 ]]; then
    fail "directory $rel contains no files matching '$glob' (found $count)"
  fi
  pass "directory $rel contains $count file(s) matching '$glob'"
}

# ── App-mode bundle ───────────────────────────────────────────────────────────
# ts_bundle name "entry_vite_app", mode = "app", html = "index.html"
# Output: entry_vite_app_bundle/ directory
APP_DIR="tests/vite_bundle/entry_vite_app_bundle"

check_dir "$APP_DIR"
check_dir_has_file "$APP_DIR" "*.html"
check_dir_has_file "$APP_DIR" "*.js"

# The HTML file must not be an empty placeholder.
HTML_FILE=$(find "$BASE/$APP_DIR" -name "*.html" | head -1)
if [[ -z "$HTML_FILE" ]]; then
  fail "No HTML file found in $APP_DIR"
fi
if [[ ! -s "$HTML_FILE" ]]; then
  fail "HTML file $HTML_FILE is empty"
fi
pass "HTML file is non-empty"

echo "ALL PASSED"
