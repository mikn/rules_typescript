#!/usr/bin/env bash
# tools/gazelle-check/check.sh
#
# Checks that all BUILD files in the workspace are up to date with what Gazelle
# would generate.
#
# Exit code 0: BUILD files are current; no regeneration needed.
# Exit code 1: BUILD files need regeneration (diff is non-empty) or an error
#              occurred while running Gazelle.
#
# Usage in CI:
#   bazel run //tools/gazelle-check
#
# Or directly (requires Bazel in PATH and a configured //:gazelle target):
#   bash tools/gazelle-check/check.sh
#
# The script relies on Gazelle's built-in --mode=diff support, which prints a
# unified diff of any changes it would make but does not write any files. It
# then checks whether that output is non-empty.
#
# ------------------------------------------------------------------
# Environment variables (all optional):
#
#   GAZELLE_TARGET   The Bazel label of the gazelle binary to use.
#                    Default: //:gazelle
#
#   BAZEL            Path to the Bazel executable.
#                    Default: bazel (resolved from PATH)
#
#   GAZELLE_ARGS     Extra arguments forwarded to Gazelle, separated by
#                    whitespace. Example: "-repo_root=/custom/root"
#                    Default: (empty)
# ------------------------------------------------------------------

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────

GAZELLE_TARGET="${GAZELLE_TARGET:-//:gazelle}"
BAZEL="${BAZEL:-bazel}"
# Split GAZELLE_ARGS on whitespace, but allow it to be empty without erroring.
read -r -a EXTRA_ARGS <<< "${GAZELLE_ARGS:-}"

# ── Helper: print to stderr ─────────────────────────────────────────────────

log() {
  echo "[gazelle-check] $*" >&2
}

# ── Locate the workspace root ───────────────────────────────────────────────
# When run via `bazel run`, Bazel sets BUILD_WORKSPACE_DIRECTORY to the
# workspace root. When run directly from a shell, fall back to walking up the
# directory tree until we find MODULE.bazel or WORKSPACE.
if [[ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then
  WORKSPACE_ROOT="${BUILD_WORKSPACE_DIRECTORY}"
else
  dir="$(pwd)"
  while [[ "$dir" != "/" ]]; do
    if [[ -f "$dir/MODULE.bazel" || -f "$dir/WORKSPACE" || -f "$dir/WORKSPACE.bazel" ]]; then
      WORKSPACE_ROOT="$dir"
      break
    fi
    dir="$(dirname "$dir")"
  done
  if [[ -z "${WORKSPACE_ROOT:-}" ]]; then
    log "ERROR: Could not locate workspace root (no MODULE.bazel / WORKSPACE found)."
    exit 1
  fi
fi

log "Workspace root: ${WORKSPACE_ROOT}"
log "Gazelle target: ${GAZELLE_TARGET}"

# ── Check that the gazelle target exists ────────────────────────────────────
# Without this check, `bazel run` would fail with an opaque "target not found"
# error. We print a clear diagnostic instead.
if ! (cd "${WORKSPACE_ROOT}" && "${BAZEL}" query "${GAZELLE_TARGET}" &>/dev/null 2>&1); then
  log "ERROR: No '${GAZELLE_TARGET}' target found in this workspace."
  log "gazelle-check must be run from a workspace that has a gazelle() target."
  log "If your gazelle target has a different label, set GAZELLE_TARGET=//path:label."
  exit 1
fi

# ── Run Gazelle in diff mode ────────────────────────────────────────────────
# Capture stdout (the diff) and let stderr flow through so Bazel progress is
# visible in CI logs.
#
# `bazel run` exits with non-zero if Bazel itself fails; we propagate that.

DIFF_OUTPUT="$(
  cd "${WORKSPACE_ROOT}"
  "${BAZEL}" run "${GAZELLE_TARGET}" -- \
    --mode=diff \
    "${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}" \
    2>&1 | tee /dev/stderr
)" || {
  log "ERROR: Bazel run failed (exit code $?)."
  exit 1
}

# The diff is printed to stdout by Gazelle when run in --mode=diff.
# We captured it in DIFF_OUTPUT above; strip any Bazel informational lines
# that start with "INFO:" or "Loading:" so we only look at actual Gazelle diff
# output.
GAZELLE_DIFF="$(
  echo "${DIFF_OUTPUT}" | grep -v '^INFO:' | grep -v '^Loading:' | grep -v '^Analyzing:' | grep -v '^[[:space:]]*$' || true
)"

# ── Evaluate the diff ───────────────────────────────────────────────────────

if [[ -z "${GAZELLE_DIFF}" ]]; then
  log "OK: BUILD files are up to date."
  exit 0
else
  log "FAIL: BUILD files are out of date. Run 'bazel run //:gazelle' to regenerate."
  log ""
  log "Diff:"
  echo "${GAZELLE_DIFF}" >&2
  exit 1
fi
