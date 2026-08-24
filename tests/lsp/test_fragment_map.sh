#!/usr/bin/env bash
# test_fragment_map.sh — hermetic shim for fragment_map_test.mjs.
#
#   bazel test //tests/lsp:test_fragment_map --test_output=all
#
# The two fragments are the bytes tsconfig_aspect's `ide_fragments` group really
# wrote for //tests/lsp/fragment_fixture, one of them for a target this package
# may not name. The .mjs stages them into a scratch workspace and runs the hook
# worker over it, so nothing here needs the host but node from the JS runtime
# toolchain.

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
WORKER_JS="$(runfile tools/tsserver-hook-worker.js)"
TEST_MJS="$(runfile tests/lsp/fragment_map_test.mjs)"
LEAF="$(runfile tests/lsp/fragment_fixture/leaf.tsconfig-fragment.json)"
ROOT="$(runfile tests/lsp/fragment_fixture/root.tsconfig-fragment.json)"

echo "INFO: node $("${NODE}" --version)"
exec "${NODE}" "${TEST_MJS}" "${WORKER_JS}" "${LEAF}" "${ROOT}"
