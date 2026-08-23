#!/usr/bin/env bash
# verify_validation.sh — Assert tsgo type-checking validates correct code.
#
# This test verifies the POSITIVE case for declarations = "oxc", where checking
# is a validation action: the tscheck stamp was created, proving tsgo ran and
# found no type errors. Under the "tsgo" default there is no stamp — the .d.ts
# themselves are the proof, covered by declaration_types_test.
#
# The stamp file is included as a data dep (via the :correct_tscheck filegroup
# which exposes the _validation output group). Its presence proves tsgo passed.

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

# ── Confirm the tscheck stamp file was created ────────────────────────────────
# The stamp file is written by the TsgoCheck action only when tsgo exits 0.
# If the stamp file is present in the test runfiles, tsgo succeeded.
STAMP="$BASE/tests/validation/annotated_oxc.tscheck"
if [[ ! -f "$STAMP" ]]; then
  fail "annotated_oxc.tscheck stamp not found — tsgo may not be installed or validation did not run (path: $STAMP)"
fi
pass "annotated_oxc.tscheck stamp exists (tsgo ran successfully under declarations = \"oxc\")"

# Sanity: the stamp file should be empty (touch-created by tsgo action)
# We just verify it exists, which is sufficient proof of tsgo success.
echo "ALL PASSED"
