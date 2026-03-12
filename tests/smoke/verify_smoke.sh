#!/usr/bin/env bash
# verify_smoke.sh — Assert that ts_compile produces correct output files.
#
# Checks:
#   1. hello.js exists and contains the compiled function body
#   2. hello.d.ts exists and contains the type declaration
#   3. hello.js.map exists (source map)
#   4. button.js exists (JSX compiled)
#   5. button.d.ts contains ButtonProps interface declaration

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

# ── hello target ──────────────────────────────────────────────────────────────
check_file    "tests/smoke/hello.js"
check_file    "tests/smoke/hello.d.ts"
check_file    "tests/smoke/hello.js.map"

# oxc strips types so function hello(): string → function hello()
check_contains "tests/smoke/hello.js"  "function hello"
check_contains "tests/smoke/hello.js"  "GREETING"
# d.ts must carry explicit types
check_contains "tests/smoke/hello.d.ts" "declare function hello"
check_contains "tests/smoke/hello.d.ts" "string"
check_contains "tests/smoke/hello.d.ts" "GREETING"

# ── button (JSX) target ───────────────────────────────────────────────────────
# Source file is Button.tsx so the output stem preserves the case: Button.js
check_file    "tests/smoke/Button.js"
check_file    "tests/smoke/Button.d.ts"
check_file    "tests/smoke/Button.js.map"

check_contains "tests/smoke/Button.js"  "function Button"
# oxc transforms JSX — the output must NOT contain raw JSX angle-bracket syntax
if grep -q "<button" "$BASE/tests/smoke/Button.js"; then
  fail "Button.js still contains raw JSX '<button' — JSX transform did not run"
fi
pass "Button.js does not contain raw JSX angle-bracket syntax"

check_contains "tests/smoke/Button.d.ts" "ButtonProps"
check_contains "tests/smoke/Button.d.ts" "Button"

echo "ALL PASSED"
