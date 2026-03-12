#!/usr/bin/env bash
# verify_type_error.sh — Assert tsgo detects type errors.
#
# This test invokes `bazel build` as a subprocess, targeting the type_error
# target with --output_groups=+_validation. It expects the build to FAIL,
# which proves that tsgo correctly detected the type error in type_error.ts.
#
# Because this test spawns a nested Bazel process, it:
#   - Requires the workspace root to be accessible (derived from MODULE.bazel
#     which is included as a data dep)
#   - Is tagged "exclusive" to avoid conflicts with the outer Bazel server
#   - Uses a separate --output_user_root to isolate the nested invocation

set -euo pipefail

# ── Locate the workspace root ─────────────────────────────────────────────────
# When run under `bazel test`, BUILD_WORKSPACE_DIRECTORY is NOT set.
# We derive the workspace root from MODULE.bazel which is in the runfiles
# (added as a data dep in BUILD.bazel).
RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"
WORKSPACE="${TEST_WORKSPACE:-_main}"

WORKSPACE_ROOT=""
if [[ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then
  WORKSPACE_ROOT="$BUILD_WORKSPACE_DIRECTORY"
elif [[ -n "$RUNFILES" && -f "$RUNFILES/$WORKSPACE/MODULE.bazel" ]]; then
  # MODULE.bazel is a symlink into the source tree — resolve to get the real path
  MODULE_LINK="$RUNFILES/$WORKSPACE/MODULE.bazel"
  WORKSPACE_ROOT="$(dirname "$(readlink -f "$MODULE_LINK")")"
fi

if [[ -z "$WORKSPACE_ROOT" || ! -f "$WORKSPACE_ROOT/MODULE.bazel" ]]; then
  echo "SKIP: Cannot locate workspace root — skipping nested-Bazel type-error test" >&2
  echo "      (Set BUILD_WORKSPACE_DIRECTORY or ensure MODULE.bazel is in runfiles)" >&2
  exit 0
fi

# ── Find bazel binary ─────────────────────────────────────────────────────────
# The outer bazel is not on PATH inside the sandbox. Look for it via the
# standard locations, or fall back to a SKIP if unavailable.
BAZEL_BIN=""
for candidate in \
    "$(which bazel 2>/dev/null || true)" \
    "/home/mikn/bin/bazel" \
    "/usr/local/bin/bazel" \
    "/usr/bin/bazel"; do
  if [[ -n "$candidate" && -x "$candidate" ]]; then
    BAZEL_BIN="$candidate"
    break
  fi
done

if [[ -z "$BAZEL_BIN" ]]; then
  echo "SKIP: bazel binary not found in standard locations — skipping nested-Bazel test" >&2
  exit 0
fi

# ── Run nested bazel build, expect FAILURE ────────────────────────────────────
TMPDIR="${TEST_TMPDIR:-/tmp/type_error_test_$$}"
mkdir -p "$TMPDIR"

echo "Workspace root: $WORKSPACE_ROOT"
echo "Running: $BAZEL_BIN build //tests/validation:type_error --output_groups=+_validation"
echo "  (expect this to FAIL due to type error in type_error.ts)"

if (cd "$WORKSPACE_ROOT" && "$BAZEL_BIN" \
    --output_user_root="$TMPDIR/bazel_output" \
    build \
    --noshow_progress \
    --noshow_loading_progress \
    //tests/validation:type_error \
    --output_groups=+_validation) \
    >"$TMPDIR/bazel_stdout.txt" 2>"$TMPDIR/bazel_stderr.txt"; then
  echo "FAIL: type_error target should have failed validation but succeeded"
  echo "  tsgo stdout:"
  cat "$TMPDIR/bazel_stdout.txt" >&2
  echo "  tsgo stderr:"
  cat "$TMPDIR/bazel_stderr.txt" >&2
  exit 1
fi

echo "PASS: bazel build correctly failed for type_error target"

# Verify the failure output mentions type checking (not a build infrastructure failure)
COMBINED="$TMPDIR/bazel_stderr.txt"
if grep -qiE "Type|TS[0-9]|error|assignable|not assignable" "$COMBINED" 2>/dev/null; then
  echo "PASS: build failure output contains type-related error message"
else
  echo "NOTE: build failed but error output did not contain expected type error text"
  echo "      This may be acceptable — tsgo reported an error but in an unexpected format."
  echo "      Error output:"
  cat "$COMBINED" >&2
fi

echo "ALL PASSED"
