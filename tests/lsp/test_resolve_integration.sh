#!/usr/bin/env bash
# test_resolve_integration.sh — Integration tests for the tsserver-hook
# monkey-patch (Tests 1-5 from the LSP test plan).
#
# Usage (direct, from workspace root):
#   bash tests/lsp/test_resolve_integration.sh
#
# Usage (via Bazel):
#   bazel test //tests/lsp:test_resolve_integration --test_output=all
#
# What it tests:
#   1. Hook loads without errors (no TypeError on modern TypeScript).
#   2. ts._bazelPatched is set to true after the hook is loaded.
#   3. ts.resolveModuleName is replaced by the Bazel wrapper function.
#   4. ts.resolveModuleName("zod", ...) resolves to a .d.ts in the Bazel
#      output base (when npm packages are available).
#   5. ts.resolveModuleName("vitest", ...) resolves similarly.
#   6. Path-alias and unknown module resolution do not throw.
#
# Prerequisites:
#   bazel build @npm//...   (or bazel build //...) must have run to populate
#   the @npm external repo.
#
# Exit code: 0 = all assertions passed, non-zero = failure.

set -euo pipefail

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
skip() { echo "SKIP: $*"; }

# ── Locate files ───────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

_realpath() {
  python3 -c "import os,sys; print(os.path.realpath(sys.argv[1]))" "$1"
}

if [[ -n "${TEST_SRCDIR:-}" ]]; then
  _RUNFILES_MAIN="${TEST_SRCDIR}/_main"
  [[ -d "${_RUNFILES_MAIN}" ]] || _RUNFILES_MAIN="${TEST_SRCDIR}"
  TOOLS_DIR="${_RUNFILES_MAIN}/tools"
  TESTS_LSP_DIR="${_RUNFILES_MAIN}/tests/lsp"

  _MODULE_SYMLINK="${_RUNFILES_MAIN}/MODULE.bazel"
  if [[ -L "${_MODULE_SYMLINK}" ]]; then
    WORKSPACE_ROOT="$(dirname "$(_realpath "${_MODULE_SYMLINK}")")"
  elif [[ -f "${_MODULE_SYMLINK}" ]]; then
    WORKSPACE_ROOT="$(_realpath "$(dirname "${_MODULE_SYMLINK}")")"
  else
    WORKSPACE_ROOT="$(dirname "$(dirname "$(_realpath "${SCRIPT_DIR}")")")"
  fi
else
  WORKSPACE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  TOOLS_DIR="${WORKSPACE_ROOT}/tools"
  TESTS_LSP_DIR="${WORKSPACE_ROOT}/tests/lsp"
fi

HOOK_JS="${TOOLS_DIR}/tsserver-hook.js"
RESOLVE_TEST_MJS="${TESTS_LSP_DIR}/resolve_test.mjs"

[[ -f "${HOOK_JS}" ]] || fail "tsserver-hook.js not found at ${HOOK_JS}"
[[ -f "${RESOLVE_TEST_MJS}" ]] || fail "resolve_test.mjs not found at ${RESOLVE_TEST_MJS}"

echo "INFO: workspace_root = ${WORKSPACE_ROOT}"
echo "INFO: hook           = ${HOOK_JS}"
echo "INFO: resolve_test   = ${RESOLVE_TEST_MJS}"

# ── Node.js available? ────────────────────────────────────────────────────────
command -v node >/dev/null 2>&1 || fail "node not found on PATH"
echo "INFO: node $(node --version)"

# ── Test A: Hook loads without TypeError on modern TypeScript ─────────────────
echo "INFO: testing hook loads without errors..."
TSSERVER_HOOK_NO_WORKER=1 node --require "${HOOK_JS}" --eval "process.exit(0)"
pass "hook loads without errors"

# ── Derive Bazel output base ───────────────────────────────────────────────────
BAZEL_OUTPUT_BASE=""

if [[ -n "${TEST_SRCDIR:-}" ]]; then
  if [[ "${TEST_SRCDIR}" == */execroot/* ]]; then
    BAZEL_OUTPUT_BASE="${TEST_SRCDIR%%/execroot/*}"
  fi
fi

if [[ -z "${BAZEL_OUTPUT_BASE}" ]]; then
  BAZEL_OUTPUT_BASE="$(bazel info output_base 2>/dev/null || true)"
fi

echo "INFO: bazel_output_base = ${BAZEL_OUTPUT_BASE:-<not found>}"

# ── Locate npm .d.ts paths ────────────────────────────────────────────────────
# These are optional: if npm packages aren't built the tests skip gracefully.
ZOD_DTS="skip"
VITEST_DTS="skip"

if [[ -n "${BAZEL_OUTPUT_BASE}" ]]; then
  _NPM_DIR=""
  for _CANDIDATE in \
    "${BAZEL_OUTPUT_BASE}/external/+npm+npm" \
    "${BAZEL_OUTPUT_BASE}/external/npm"
  do
    if [[ -f "${_CANDIDATE}/BUILD.bazel" ]]; then
      _NPM_DIR="${_CANDIDATE}"
      break
    fi
  done

  if [[ -n "${_NPM_DIR}" ]]; then
    _ZOD_CANDIDATE="${_NPM_DIR}/zod__3_24_2/index.d.ts"
    _VITEST_CANDIDATE="${_NPM_DIR}/vitest__3_0_9/dist/index.d.ts"
    [[ -f "${_ZOD_CANDIDATE}" ]]    && ZOD_DTS="${_ZOD_CANDIDATE}"
    [[ -f "${_VITEST_CANDIDATE}" ]] && VITEST_DTS="${_VITEST_CANDIDATE}"
  else
    echo "INFO: @npm external directory not found — npm resolution tests will be skipped"
  fi
fi

echo "INFO: zod_dts    = ${ZOD_DTS}"
echo "INFO: vitest_dts = ${VITEST_DTS}"

# ── Build TSSERVER_HOOK_PRELOAD_MAP ───────────────────────────────────────────
# Pre-populate the hook's resolution cache synchronously so the test does not
# have to wait for the async worker thread to finish.
PRELOAD_MAP="{}"

if [[ "${ZOD_DTS}" != "skip" ]] || [[ "${VITEST_DTS}" != "skip" ]]; then
  # Build JSON using Python to avoid quoting issues with shell.
  PRELOAD_MAP="$(python3 - "${ZOD_DTS}" "${VITEST_DTS}" << 'PYEOF'
import json, sys
zod = sys.argv[1]
vitest = sys.argv[2]
m = {}
if zod != "skip":
    m["zod"] = zod
if vitest != "skip":
    m["vitest"] = vitest
print(json.dumps(m))
PYEOF
)"
fi

echo "INFO: preload_map = ${PRELOAD_MAP}"

# ── Test B-G: Run the Node.js integration script ──────────────────────────────
# TSSERVER_HOOK_NO_WORKER=1: skip the background worker thread so the test
# process exits promptly.  The cache is pre-populated via PRELOAD_MAP.
echo "INFO: running resolve_test.mjs..."
TSSERVER_HOOK_PRELOAD_MAP="${PRELOAD_MAP}" \
TSSERVER_HOOK_NO_WORKER=1 \
node \
  --require "${HOOK_JS}" \
  "${RESOLVE_TEST_MJS}" \
  "${ZOD_DTS}" \
  "${VITEST_DTS}" \
  "${WORKSPACE_ROOT}"

echo ""
echo "ALL PASSED"
