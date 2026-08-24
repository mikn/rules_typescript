#!/usr/bin/env bash
# test_tsserver_diagnostics.sh — gold test for the tsserver resolution hook.
#
#   bazel test //tests/lsp:test_tsserver_diagnostics --test_output=all
#
# The subject is a TypeScript language service running under the hook, so every
# assertion is in tsserver_diag_test.mjs and this file is the shim that gives it
# a hermetic environment: node from the registered JS runtime toolchain, and
# typescript and zod from the lockfile through @npm, laid out as a node_modules
# tree in the runfiles. Nothing is read from the host and nothing is skipped --
# an absent prerequisite fails the test rather than turning it into a no-op.

# --- begin runfiles.bash initialization v3 ---
# Copy-pasted from the Bazel Bash runfiles library v3.
set -uo pipefail; set +e; f=bazel_tools/tools/bash/runfiles/runfiles.bash
# shellcheck disable=SC1090
source "${RUNFILES_DIR:-/dev/null}/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "${RUNFILES_MANIFEST_FILE:-/dev/null}" | cut -f2- -d' ')" 2>/dev/null || \
  source "$0.runfiles/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.exe.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || \
  { echo>&2 "ERROR: cannot find $f"; exit 1; }; f=; set -e
# --- end runfiles.bash initialization v3 ---

fail() { echo "FAIL: $*" >&2; exit 1; }

# rlocation answers an absent runfile with a non-zero exit and no output, which
# would otherwise become an empty path handed to node.
runfile() {
  local resolved
  resolved="$(rlocation "${TEST_WORKSPACE:-_main}/$1")" || resolved=""
  [[ -n "${resolved}" && -e "${resolved}" ]] || fail "missing runfile: $1"
  printf '%s\n' "${resolved}"
}

NODE="$(runfile ts/toolchain/node_resolved/node)"
HOOK_JS="$(runfile tools/tsserver-hook.js)"
DTS_ENTRY_MJS="$(runfile tests/lsp/dts_entry.mjs)"
DIAG_TEST_MJS="$(runfile tests/lsp/tsserver_diag_test.mjs)"
NODE_MODULES="$(runfile tests/lsp/lsp_node_modules)"
[[ -d "${NODE_MODULES}" ]] || fail "not a node_modules tree: ${NODE_MODULES}"

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
