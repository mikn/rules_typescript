#!/usr/bin/env bash
# verify_bundle.sh — Assert ts_binary/ts_bundle produces correct output.
#
# Verifies:
#   1. Individual compiled .js/.d.ts files exist for each source.
#   2. ts_bundle (canonical name) produces a correct placeholder bundle artifact.
#   3. ts_binary runfiles contain the transitive .js files (for `bazel run`).

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

# ── Individual compile outputs (from :app and :lib) ───────────────────────────
check_file    "tests/bundle/lib.js"
check_file    "tests/bundle/lib.d.ts"
check_file    "tests/bundle/lib.js.map"
check_file    "tests/bundle/app.js"
check_file    "tests/bundle/app.d.ts"
check_file    "tests/bundle/app.js.map"

check_contains "tests/bundle/lib.js" "function greet"
check_contains "tests/bundle/lib.d.ts" "greet"
# app.js imports lib — uses an import statement (compilation boundary)
check_contains "tests/bundle/app.js" "import"
check_contains "tests/bundle/app.js" "message"

# ── ts_binary: transitive .js files are in runfiles (available for `bazel run`) ──
# ts_binary is now an executable — it doesn't produce a bundle artifact by
# default.  Its DefaultInfo.runfiles include all transitive .js files so they
# are accessible when the runner script executes.
check_file    "tests/bundle/app.js"
check_file    "tests/bundle/lib.js"

# ── ts_bundle (canonical name) ────────────────────────────────────────────────
# Rule name is "bundle_canonical" so the output path is:
#   bundle_canonical_bundle/bundle_canonical.js
check_file    "tests/bundle/bundle_canonical_bundle/bundle_canonical.js"
check_contains "tests/bundle/bundle_canonical_bundle/bundle_canonical.js" "function greet"
check_contains "tests/bundle/bundle_canonical_bundle/bundle_canonical.js" "message"
# Placeholder header comment is present
check_contains "tests/bundle/bundle_canonical_bundle/bundle_canonical.js" "Placeholder bundle"

echo "ALL PASSED"
