#!/usr/bin/env bash
# verify_lint.sh — Assert that ts_lint produces the validation stamp file.
#
# Checks:
#   1. The .tslint stamp file exists (produced by a successful lint run).
#   2. The stamp is at the expected path under the test runfiles.

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

# The stamp file is declared as:
#   ctx.actions.declare_file("{name}.tslint")
# Inside the tests/lint package, it lives at tests/lint/clean_lint.tslint.
STAMP="$BASE/tests/lint/clean_lint.tslint"

if [[ ! -f "$STAMP" ]]; then
  fail "Lint stamp not found at $STAMP"
fi
pass "Lint stamp exists at $STAMP"

echo "ALL PASSED"
