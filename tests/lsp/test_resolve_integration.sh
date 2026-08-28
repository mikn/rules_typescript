#!/usr/bin/env bash
# test_resolve_integration.sh — the tsserver hook's monkey-patch, end to end.
#
#   bazel test //tests/lsp:test_resolve_integration --test_output=all
#
# The subject is the patched ts.resolveModuleName, so every assertion is in
# resolve_test.mjs and this file is the shim around it: node from the JS runtime
# toolchain, typescript/zod/vitest from the lockfile via
# //tests/lsp:lsp_node_modules. The hook's resolution cache is pre-populated
# through TSSERVER_HOOK_PRELOAD_MAP so the assertions do not race the background
# worker; what the worker itself produces is //tests/lsp:test_resolution_map's
# job.

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
RESOLVE_TEST_MJS="$(runfile tests/lsp/resolve_test.mjs)"
NODE_MODULES="$(runfile tests/lsp/lsp_node_modules)"
[[ -d "${NODE_MODULES}" ]] || fail "not a node_modules tree: ${NODE_MODULES}"

echo "INFO: node $("${NODE}" --version)"

# What a ts_path_alias directive puts in the cache: the "@/" prefix mapped to a
# directory, so "@/lib/math" must come back as <dir>/lib/math.ts.
ALIAS_DIR="${TEST_TMPDIR:?TEST_TMPDIR is unset}/alias_root"
mkdir -p "${ALIAS_DIR}/lib" "${ALIAS_DIR}/app"
echo 'export const add = (a: number, b: number): number => a + b;' > "${ALIAS_DIR}/lib/math.ts"

PACKAGE_MAP="$("${NODE}" "${DTS_ENTRY_MJS}" "${NODE_MODULES}" zod vitest)"
PRELOAD_MAP="$(M="${PACKAGE_MAP}" A="${ALIAS_DIR}" "${NODE}" --eval \
  'const m = JSON.parse(process.env.M); m["__alias__@/"] = process.env.A; process.stdout.write(JSON.stringify(m))')"
echo "INFO: preload_map = ${PRELOAD_MAP}"

read -r ZOD_DTS VITEST_DTS <<< "$(M="${PACKAGE_MAP}" "${NODE}" --eval \
  'const m = JSON.parse(process.env.M); process.stdout.write(m.zod + " " + m.vitest)')"

NODE_PATH="${NODE_MODULES}" \
TSSERVER_HOOK_PRELOAD_MAP="${PRELOAD_MAP}" \
TSSERVER_HOOK_NO_WORKER=1 \
  "${NODE}" --require "${HOOK_JS}" "${RESOLVE_TEST_MJS}" \
    "${ZOD_DTS}" "${VITEST_DTS}" "${ALIAS_DIR}"
