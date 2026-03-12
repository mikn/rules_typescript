#!/usr/bin/env bash
# verify_css.sh — Assert that css_library + ts_compile with CSS deps works.
#
# Checks:
#   1. button.css is present in the output tree via css_library
#   2. theme.css is present via the transitive_styles css_library dep
#   3. Button.js is compiled correctly (CSS import doesn't break compilation)
#   4. Button.d.ts is generated with the exported types

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
    fail "$rel does not contain '$pattern' (content: $(cat "$full"))"
  fi
  pass "$rel contains '$pattern'"
}

# ── CSS files from css_library ─────────────────────────────────────────────────
check_file "tests/css/button.css"
check_file "tests/css/theme.css"
check_contains "tests/css/button.css" "[.]button"
check_contains "tests/css/theme.css" "color-primary"

# ── Compiled TypeScript output ─────────────────────────────────────────────────
check_file "tests/css/Button.js"
check_file "tests/css/Button.d.ts"
check_file "tests/css/Button.js.map"

check_contains "tests/css/Button.js" "describeButton"
check_contains "tests/css/Button.d.ts" "ButtonProps"
check_contains "tests/css/Button.d.ts" "describeButton"

echo "ALL PASSED"
