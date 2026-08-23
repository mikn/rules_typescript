#!/usr/bin/env bash
# verify_declarations.sh — Assert declarations = "tsgo" emits REAL types.
#
# Regression test for the failure mode that motivated tsgo declaration emit:
# a syntactic emitter faced with un-annotated exports produces `{}` and
# `unknown`, the target still builds, and only a downstream consumer fails —
# pointing at the wrong file. Here we assert the declarations carry the
# inferred types instead.

set -euo pipefail

RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"
if [[ -z "$RUNFILES" ]]; then
  echo "FAIL: RUNFILES_DIR and TEST_SRCDIR are both unset" >&2
  exit 1
fi
BASE="$RUNFILES/${TEST_WORKSPACE:-_main}"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

DTS="$BASE/tests/validation/inferred.d.ts"
[[ -f "$DTS" ]] || fail "inferred.d.ts was not emitted (path: $DTS)"
echo "--- inferred.d.ts ---"
cat "$DTS"
echo "---------------------"

# The types must be inferred, not widened away.
grep -q "RegExp" "$DTS" || fail "inferred.d.ts lost the RegExp types; got no 'RegExp' at all"
grep -qE ':[[:space:]]*\{\}' "$DTS" && fail "inferred.d.ts widened an export to '{}'"
grep -qE ':[[:space:]]*unknown' "$DTS" && fail "inferred.d.ts widened an export to 'unknown'"
pass "inferred.d.ts carries inferred types (RegExp present, no '{}' or 'unknown' widening)"

# The inferred function return type must be the string-literal union, not string.
grep -q '"preview"' "$DTS" || fail "classify()'s inferred literal return type was lost"
pass "classify() kept its inferred string-literal union"

# Annotated sources emit too, obviously.
CORRECT="$BASE/tests/validation/correct.d.ts"
[[ -f "$CORRECT" ]] || fail "correct.d.ts was not emitted (path: $CORRECT)"
grep -q "declare function add" "$CORRECT" || fail "correct.d.ts is missing add()"
pass "correct.d.ts emitted"

echo "ALL PASSED"
