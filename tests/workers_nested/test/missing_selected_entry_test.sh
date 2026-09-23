#!/usr/bin/env bash
set -euo pipefail
log="${TEST_TMPDIR}/missing-entry.log"
if "$1" >"${log}" 2>&1; then
  echo 'Workers selected an undeclared entry without failing' >&2
  exit 1
fi
if ! grep -aF 'not-declared.ts' "${log}" | grep -F "which this test's runfiles do not hold"; then
  cat "${log}" >&2
  exit 1
fi
