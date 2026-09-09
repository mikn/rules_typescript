#!/usr/bin/env bash
# check_coverage_report.sh -- `bazel coverage` on the fixture writes a report
# naming what --instrumentation_filter selected. docs/CI_CD.md § Coverage
# Report.
#
#   tools/ci/check_coverage_report.sh
#   BAZEL=bazelisk tools/ci/check_coverage_report.sh

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

bazel="${BAZEL:-bazel}"
target=//tests/vitest/coverage:math_coverage_test

fail() {
  printf 'check_coverage_report: %s\n' "$1" >&2
  exit 1
}

# The combined report's SF: lines after one run with the given flags.
report() {
  "$bazel" coverage --combined_report=lcov "$@" "$target" > /dev/null
  local out
  out="$("$bazel" info output_path)/_coverage/_coverage_report.dat"
  [ -f "$out" ] || fail "no combined report at $out"
  grep '^SF:' "$out" | LC_ALL=C sort || true
}

expect() {
  local label="$1" want="$2" got
  shift 2
  got="$(report "$@")"
  if [ "$got" != "$want" ]; then
    fail "$label: the report's SF: lines differ (want, then got):
$(printf '%s\n' "$want" | sed 's/^/  /')
  --
$(printf '%s\n' "$got" | sed 's/^/  /')"
  fi
}

# Bazel's default filter is the target's package, so the dep in the package is
# reported and the one in //tests/vitest is not until a wider filter names it.
expect "the default --instrumentation_filter" \
  'SF:tests/vitest/coverage/same_package.js'
expect "--instrumentation_filter=^//tests/vitest[/:]" \
  $'SF:tests/vitest/coverage/same_package.js\nSF:tests/vitest/math.js' \
  '--instrumentation_filter=^//tests/vitest[/:]'

printf 'check_coverage_report: bazel coverage on %s names what %s selects.\n' \
  "$target" --instrumentation_filter
