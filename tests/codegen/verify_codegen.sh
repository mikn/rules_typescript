#!/usr/bin/env bash
# Verifies that ts_codegen produced the expected generated output file.
set -euo pipefail

# Resolve RUNFILES_DIR.
if [[ -z "${RUNFILES_DIR:-}" && -n "${TEST_SRCDIR:-}" ]]; then
  RUNFILES_DIR="$TEST_SRCDIR"
fi
cd "${RUNFILES_DIR}"

# The generated file is in the test's data dep: :generated_ts.
# Bazel places it under _main/<package-relative-path>.
GENERATED_FILE="_main/tests/codegen/generated.ts"

if [[ ! -f "$GENERATED_FILE" ]]; then
  echo "FAIL: generated file not found: $GENERATED_FILE" >&2
  echo "Files present:" >&2
  find _main/tests/codegen -type f 2>/dev/null | sort >&2 || true
  exit 1
fi

# Verify the file contains the expected content.
if ! grep -q "export const GENERATED = true" "$GENERATED_FILE"; then
  echo "FAIL: generated file does not contain expected export" >&2
  echo "Contents of $GENERATED_FILE:" >&2
  cat "$GENERATED_FILE" >&2
  exit 1
fi

echo "PASS: ts_codegen produced $GENERATED_FILE with expected content"
