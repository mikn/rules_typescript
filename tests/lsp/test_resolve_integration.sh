#!/usr/bin/env bash
# test_resolve_integration.sh — the tsserver hook's monkey-patch, end to end.
#
#   bazel test //tests/lsp:test_resolve_integration --test_output=all
#
# Hermetic: node from the JS runtime toolchain, typescript/zod/vitest from the
# lockfile via //tests/lsp:lsp_node_modules. The hook's resolution cache is
# pre-populated through TSSERVER_HOOK_PRELOAD_MAP so the assertions do not race
# the background worker; what the worker itself produces is
# //tests/lsp:test_resolution_map's job.

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
[[ -d "${RUNFILES}/_main" ]] && RUNFILES="${RUNFILES}/_main"

NODE="${RUNFILES}/ts/toolchain/node_resolved/node"
HOOK_JS="${RUNFILES}/tools/tsserver-hook.js"
DTS_ENTRY_MJS="${RUNFILES}/tests/lsp/dts_entry.mjs"
RESOLVE_TEST_MJS="${RUNFILES}/tests/lsp/resolve_test.mjs"
NODE_MODULES="${RUNFILES}/tests/lsp/lsp_node_modules"

for f in "${NODE}" "${HOOK_JS}" "${DTS_ENTRY_MJS}" "${RESOLVE_TEST_MJS}"; do
  [[ -f "${f}" ]] || fail "missing runfile: ${f}"
done
[[ -d "${NODE_MODULES}" ]] || fail "missing node_modules tree: ${NODE_MODULES}"

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
