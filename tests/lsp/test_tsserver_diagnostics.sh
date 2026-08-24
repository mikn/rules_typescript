#!/usr/bin/env bash
# test_tsserver_diagnostics.sh — gold test for the tsserver resolution hook.
#
#   bazel test //tests/lsp:test_tsserver_diagnostics --test_output=all
#
# Hermetic: node comes from the registered JS runtime toolchain, and typescript
# and zod come from the lockfile through @npm, laid out as a node_modules tree
# in the runfiles. Nothing is read from the host and nothing is skipped -- an
# absent prerequisite fails the test rather than turning it into a no-op.

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
[[ -d "${RUNFILES}/_main" ]] && RUNFILES="${RUNFILES}/_main"

NODE="${RUNFILES}/ts/toolchain/node_resolved/node"
HOOK_JS="${RUNFILES}/tools/tsserver-hook.js"
DTS_ENTRY_MJS="${RUNFILES}/tests/lsp/dts_entry.mjs"
DIAG_TEST_MJS="${RUNFILES}/tests/lsp/tsserver_diag_test.mjs"
NODE_MODULES="${RUNFILES}/tests/lsp/lsp_node_modules"

for f in "${NODE}" "${HOOK_JS}" "${DTS_ENTRY_MJS}" "${DIAG_TEST_MJS}"; do
  [[ -f "${f}" ]] || fail "missing runfile: ${f}"
done
[[ -d "${NODE_MODULES}" ]] || fail "missing node_modules tree: ${NODE_MODULES}"

echo "INFO: node $("${NODE}" --version)"

# A host install would satisfy require('typescript') silently, which is exactly
# the non-hermeticity this test used to have.
[[ -f "${NODE_MODULES}/typescript/package.json" ]] || \
  fail "typescript is not in ${NODE_MODULES} -- is @npm//:typescript still a dep of //tests/lsp:lsp_node_modules?"

PRELOAD_MAP="$("${NODE}" "${DTS_ENTRY_MJS}" "${NODE_MODULES}" zod)"
echo "INFO: preload_map = ${PRELOAD_MAP}"

ZOD_DTS="$(M="${PRELOAD_MAP}" "${NODE}" --eval \
  'process.stdout.write(JSON.parse(process.env.M).zod)')"

NODE_PATH="${NODE_MODULES}" \
TSSERVER_HOOK_PRELOAD_MAP="${PRELOAD_MAP}" \
TSSERVER_HOOK_NO_WORKER=1 \
  "${NODE}" --require "${HOOK_JS}" "${DIAG_TEST_MJS}" "${ZOD_DTS}"
