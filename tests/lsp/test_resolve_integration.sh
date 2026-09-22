#!/usr/bin/env bash
# Preloading resolution data avoids racing the background worker.

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
RESOLVE_TEST_MJS="$(runfile tests/lsp/resolve_test.mjs)"
NODE_MODULES="$(runfile tests/lsp/node_modules)"
[[ -d "${NODE_MODULES}" ]] || fail "not a node_modules tree: ${NODE_MODULES}"

echo "INFO: node $("${NODE}" --version)"

# What the worker puts in the cache for a first-party package: its key is the
# package path and its value the .d.ts a build wrote into bazel-bin.
WORK_DIR="${TEST_TMPDIR:?TEST_TMPDIR is unset}/ws"
LIB_DTS="${WORK_DIR}/bazel-bin/src/lib/index.d.ts"
mkdir -p "$(dirname "${LIB_DTS}")" "${WORK_DIR}/app"
echo 'export declare function add(a: number, b: number): number;' > "${LIB_DTS}"

PRELOAD_MAP="$(L="${LIB_DTS}" "${NODE}" --eval \
  'process.stdout.write(JSON.stringify({ "src/lib": process.env.L }))')"
echo "INFO: preload_map = ${PRELOAD_MAP}"

NODE_PATH="${NODE_MODULES}" \
TSSERVER_HOOK_PRELOAD_MAP="${PRELOAD_MAP}" \
TSSERVER_HOOK_NO_WORKER=1 \
  "${NODE}" --require "${HOOK_JS}" "${RESOLVE_TEST_MJS}" "${LIB_DTS}" "${WORK_DIR}"
