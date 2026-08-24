#!/usr/bin/env bash
# resolution_parity_test.sh — hermetic wrapper for resolution_parity_test.mjs.
#
#   bazel test //vite/tests:resolution_parity_test --test_output=all
#
# The fixture module graph is written under TEST_TMPDIR by the .mjs, so this
# needs nothing from the host but node from the JS runtime toolchain.

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
if [[ -d "${RUNFILES}/_main" ]]; then
  RUNFILES="${RUNFILES}/_main"
fi

NODE="${RUNFILES}/ts/toolchain/node_resolved/node"
BUNDLE="${RUNFILES}/vite/vite_plugin_bazel.mjs"
TEST_MJS="${RUNFILES}/vite/tests/resolution_parity_test.mjs"

for f in "${NODE}" "${BUNDLE}" "${TEST_MJS}"; do
  [[ -f "${f}" ]] || fail "missing runfile: ${f}"
done

echo "INFO: node $("${NODE}" --version)"
exec "${NODE}" "${TEST_MJS}" "${BUNDLE}"
