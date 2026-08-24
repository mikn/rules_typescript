#!/usr/bin/env bash
# Asserts that every environment-shaping attribute reaches the generated vitest
# config, and that the layers are composed in the documented order.
# reporters and coverage thresholds have no observable effect from inside a
# passing test, so the config itself is what gets pinned here.

set -euo pipefail

RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"
if [[ -z "$RUNFILES" ]]; then
  echo "FAIL: RUNFILES_DIR and TEST_SRCDIR are both unset" >&2
  exit 1
fi

CONFIG="$RUNFILES/${TEST_WORKSPACE:-_main}/tests/vitest/attrs/_attrs_test_vitest.config.mjs"
[[ -f "$CONFIG" ]] || {
  echo "FAIL: no generated config at $CONFIG" >&2
  exit 1
}

failed=0
expect() {
  local description="$1" pattern="$2"
  if grep -qF -- "$pattern" "$CONFIG"; then
    echo "PASS: $description"
  else
    echo "FAIL: $description — no line matching: $pattern" >&2
    failed=1
  fi
}

expect "environment attr"          'environment: "node"'
expect "setup_files attr"          'setupFiles: [abs("./setup.js")]'
expect "globals attr"              'globals: true'
expect "reporters attr"            'reporters: ["default"]'
expect "coverage_thresholds attr"  'coverage: { thresholds: { "lines": 0, "perFile": true } }'
expect "inline config dict"        '{"test":{"testTimeout":20000}}'
expect "layer order: bazel, then user config, then attrs" \
  'merge(merge(bazelLayer, user), attrLayer)'

if [[ "$failed" -ne 0 ]]; then
  echo "generated config was:" >&2
  cat "$CONFIG" >&2
  exit 1
fi
