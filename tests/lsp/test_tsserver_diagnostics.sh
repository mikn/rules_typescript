#!/usr/bin/env bash
# test_tsserver_diagnostics.sh — Gold test: TypeScript Language Service + hook.
#
# Usage (direct, from workspace root):
#   bash tests/lsp/test_tsserver_diagnostics.sh
#
# Usage (via Bazel):
#   bazel test //tests/lsp:test_tsserver_diagnostics --test_output=all
#
# What it tests (Test 5 from the LSP test plan — gold test):
#   Load the Bazel hook, create a TypeScript Language Service for a virtual
#   file that imports from "zod", request semantic diagnostics, and assert
#   that NO "Cannot find module 'zod'" error is reported.
#
# Why TypeScript Language Service API (not standalone tsserver.js):
#   tsserver.js is a self-contained bundle that does not call require('typescript').
#   The hook patches ts.resolveModuleName via Module._load — visible only to
#   callers that require('typescript').  ts.createLanguageService() uses TypeScript
#   as a module, so it sees the patched function.  This correctly models the real
#   use case: editors (neovim, emacs, VS Code) that load TypeScript as a module
#   in the same Node process where the hook is loaded.
#
# Exit code: 0 = pass, 1 = failure.

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
TSSERVER_TEST_MJS="${TESTS_LSP_DIR}/tsserver_diag_test.mjs"

[[ -f "${HOOK_JS}" ]] || fail "tsserver-hook.js not found at ${HOOK_JS}"
[[ -f "${TSSERVER_TEST_MJS}" ]] || fail "tsserver_diag_test.mjs not found at ${TSSERVER_TEST_MJS}"

echo "INFO: workspace_root = ${WORKSPACE_ROOT}"
echo "INFO: hook           = ${HOOK_JS}"

# ── Node.js available? ────────────────────────────────────────────────────────
command -v node >/dev/null 2>&1 || fail "node not found on PATH"
echo "INFO: node $(node --version)"

# ── TypeScript available via node? ────────────────────────────────────────────
TS_AVAILABLE=false
if node --input-type=module --eval "import { createRequire } from 'module'; const r = createRequire('${WORKSPACE_ROOT}/x.mjs'); r.resolve('typescript');" 2>/dev/null; then
  TS_AVAILABLE=true
fi
# Fallback: check system location
if [[ "${TS_AVAILABLE}" == "false" ]] && [[ -f "/usr/share/nodejs/typescript/lib/typescript.js" ]]; then
  TS_AVAILABLE=true
fi

if [[ "${TS_AVAILABLE}" == "false" ]]; then
  skip "typescript not available — install 'typescript' npm package to run this test"
  echo "ALL SKIPPED"
  exit 0
fi

echo "INFO: typescript is available"

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

# ── Locate zod .d.ts (optional) ───────────────────────────────────────────────
ZOD_DTS="skip"

if [[ -n "${BAZEL_OUTPUT_BASE}" ]]; then
  for _CANDIDATE in \
    "${BAZEL_OUTPUT_BASE}/external/+npm+npm/zod__3_24_2/index.d.ts" \
    "${BAZEL_OUTPUT_BASE}/external/npm/zod__3_24_2/index.d.ts"
  do
    if [[ -f "${_CANDIDATE}" ]]; then
      ZOD_DTS="${_CANDIDATE}"
      break
    fi
  done
fi

echo "INFO: zod_dts = ${ZOD_DTS}"

# ── Build TSSERVER_HOOK_PRELOAD_MAP ───────────────────────────────────────────
# Pre-populate the hook's resolution cache so ts.resolveModuleName('zod', ...)
# returns a valid path without waiting for the async worker.
PRELOAD_MAP="{}"
if [[ "${ZOD_DTS}" != "skip" ]]; then
  PRELOAD_MAP="$(python3 -c "import json,sys; print(json.dumps({'zod': sys.argv[1]}))" "${ZOD_DTS}")"
fi
echo "INFO: preload_map = ${PRELOAD_MAP}"

# ── Run the Language Service gold test ────────────────────────────────────────
echo "INFO: running tsserver_diag_test.mjs..."
TSSERVER_HOOK_PRELOAD_MAP="${PRELOAD_MAP}" \
TSSERVER_HOOK_NO_WORKER=1 \
node \
  --require "${HOOK_JS}" \
  "${TSSERVER_TEST_MJS}" \
  "${HOOK_JS}" \
  "${ZOD_DTS}"

echo ""
echo "ALL PASSED"
