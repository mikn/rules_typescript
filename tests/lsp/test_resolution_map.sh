#!/usr/bin/env bash
# test_resolution_map.sh — the tsserver-hook worker over this repo's own
# generated data, end to end.
#
#   bazel test //tests/lsp:test_resolution_map --test_output=all
#
# `bazel run //:refresh_tsconfig` is a copier over a manifest the ide_tsconfig
# rule wrote at analysis time, so the same copier can stage that manifest into a
# scratch workspace here -- no bazel, no network, nothing from the host. What
# this pins is the seam between the two halves of the IDE integration: the
# Starlark that writes .bazel/tsserver-hook-data.json and installs the npm
# declarations, and the JavaScript that reads them back. The map itself is
# asserted over in resolution_map_test.mjs; the steps here are the staging it
# needs, each a prerequisite of the next.
#
# Exit code: 0 = all assertions passed, non-zero = failure.

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

pass() { echo "PASS: $*"; }
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
WORKER_JS="$(runfile tools/tsserver-hook-worker.js)"
TEST_MJS="$(runfile tests/lsp/resolution_map_test.mjs)"
REFRESH="$(runfile refresh_tsconfig)"
echo "INFO: node $("${NODE}" --version)"

# ── The hook script loads on its own ──────────────────────────────────────────
TSSERVER_HOOK_NO_WORKER=1 "${NODE}" --require "${HOOK_JS}" --eval "process.exit(0)"
pass "hook loads without errors"

# ── Stage what refresh_tsconfig installs, into a scratch workspace ────────────
# The copier reads its manifest from the runfiles tree this test already has, so
# pointing BUILD_WORKSPACE_DIRECTORY at TEST_TMPDIR is the whole of the setup.
WORKSPACE_ROOT="${TEST_TMPDIR}/workspace"
mkdir -p "${WORKSPACE_ROOT}"

BUILD_WORKSPACE_DIRECTORY="${WORKSPACE_ROOT}" \
    COPY_TO_WORKSPACE_MANIFEST="${TEST_WORKSPACE:-_main}/refresh_tsconfig.manifest.json" \
    "${REFRESH}" > "${TEST_TMPDIR}/copied.txt"
echo "INFO: staged $(wc -l < "${TEST_TMPDIR}/copied.txt") files into ${WORKSPACE_ROOT}"

[[ -f "${WORKSPACE_ROOT}/.bazel/tsserver-hook-data.json" ]] || \
    fail "refresh_tsconfig did not install .bazel/tsserver-hook-data.json"
pass "refresh_tsconfig installed the hook data"

# ── The worker's map of it ────────────────────────────────────────────────────
exec "${NODE}" "${TEST_MJS}" "${WORKER_JS}" "${WORKSPACE_ROOT}"
