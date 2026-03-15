#!/usr/bin/env bash
# oxlint_wrapper.sh — Thin wrapper around the oxlint native binary.
#
# This script is used in Bazel tests where the npm_bin runner is not suitable
# (action sandboxes without RUNFILES_DIR setup). It directly invokes the
# platform-specific native oxlint binary from the package files.
#
# The native binary is included as a data dep on the sh_binary that wraps
# this script.  At runtime, Bazel makes all data files available at their
# short_path within the runfiles tree.

set -euo pipefail

RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"

# If no RUNFILES_DIR (action sandbox), look for the binary relative to this script.
# Bazel action sandbox: files are at exec paths, not runfiles paths.
# The script itself is at: external/+npm+npm/... or similar exec path.
# We look for the native binary relative to the script directory.

if [[ -n "$RUNFILES" ]]; then
  # Running via bazel run or bazel test — use runfiles tree.
  NATIVE_BIN="$RUNFILES/+npm+npm/oxlint_linux-x64-gnu__0_16_6/oxlint"
  if [[ ! -f "$NATIVE_BIN" ]]; then
    # Try musl variant
    NATIVE_BIN="$RUNFILES/+npm+npm/oxlint_linux-x64-musl__0_16_6/oxlint"
  fi
else
  # Action sandbox: RUNFILES_DIR is not set. The binary is at the exec path.
  # Find it relative to this script's directory.
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  # Walk up from script dir to find the oxlint native binary.
  NATIVE_BIN=""
  for candidate in \
    "${SCRIPT_DIR}/../oxlint_linux-x64-gnu__0_16_6/oxlint" \
    "${SCRIPT_DIR}/../../oxlint_linux-x64-gnu__0_16_6/oxlint"
  do
    if [[ -f "$candidate" ]]; then
      NATIVE_BIN="$(realpath "$candidate")"
      break
    fi
  done
fi

if [[ -z "${NATIVE_BIN:-}" || ! -f "$NATIVE_BIN" ]]; then
  echo "oxlint_wrapper: could not find native oxlint binary" >&2
  echo "  RUNFILES=${RUNFILES:-<unset>}" >&2
  echo "  Tried: $NATIVE_BIN" >&2
  exit 1
fi

exec "$NATIVE_BIN" "$@"
