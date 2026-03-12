#!/usr/bin/env bash
# verify_multi.sh — Assert .d.ts compilation boundary correctness.
#
# The critical property: tests/multi/app imports from tests/multi/lib.
# The compiled app.js must use an import statement (not inline the lib source),
# proving that the .d.ts boundary is respected — app compiled against lib's
# .d.ts, not its .ts source.
#
# Checks:
#   1. lib.js, lib.d.ts, lib.js.map exist
#   2. lib.d.ts exposes add and multiply with explicit types
#   3. app/main.js exists and contains the calculate function
#   4. app/main.js uses an import (compilation boundary enforced — lib code
#      is NOT inlined into app; each package compiles independently)
#   5. app/main.d.ts exists and declares calculate

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

check_not_contains() {
  local rel="$1"
  local pattern="$2"
  local full="$BASE/$rel"
  if grep -q "$pattern" "$full"; then
    fail "$rel should NOT contain '$pattern' — lib code leaked into app output"
  fi
  pass "$rel correctly does not contain '$pattern'"
}

# ── lib target ────────────────────────────────────────────────────────────────
check_file    "tests/multi/lib/math.js"
check_file    "tests/multi/lib/math.d.ts"
check_file    "tests/multi/lib/math.js.map"

# lib.d.ts must carry explicit type signatures (isolated declarations)
check_contains "tests/multi/lib/math.d.ts" "add"
check_contains "tests/multi/lib/math.d.ts" "multiply"
check_contains "tests/multi/lib/math.d.ts" "number"

# ── app target ────────────────────────────────────────────────────────────────
check_file    "tests/multi/app/main.js"
check_file    "tests/multi/app/main.d.ts"
check_file    "tests/multi/app/main.js.map"

check_contains "tests/multi/app/main.js" "calculate"

# .d.ts boundary: app compiled against lib's .d.ts, so app.js must use an
# import statement for lib symbols — the lib function bodies must NOT appear
# verbatim in app's output (that would indicate tree-shaking/bundling, not
# compilation boundary).
check_contains    "tests/multi/app/main.js" "import"
check_not_contains "tests/multi/app/main.js" "return a + b"

# app.d.ts must declare the exported calculate function
check_contains "tests/multi/app/main.d.ts" "calculate"

echo "ALL PASSED"
