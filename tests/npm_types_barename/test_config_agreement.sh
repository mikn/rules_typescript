#!/usr/bin/env bash
# test_config_agreement.sh — hermetic shim for config_agreement_test.mjs.
#
#   bazel test //tests/npm_types_barename:test_config_agreement --test_output=all
#
# Both subjects are generated files, so all this does is find them and node in
# the runfiles and hand over. Every assertion lives in the .mjs.

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

runfile() {
  local resolved
  resolved="$(rlocation "${TEST_WORKSPACE:-_main}/$1")" || resolved=""
  [[ -n "${resolved}" && -e "${resolved}" ]] || fail "missing runfile: $1"
  printf '%s\n' "${resolved}"
}

NODE="$(runfile ts/toolchain/node_resolved/node)"
TEST_MJS="$(runfile tests/npm_types_barename/config_agreement_test.mjs)"
BUILD_TSCONFIG="$(runfile tests/npm_types_barename/own_import.tsconfig.json)"
EDITOR_TSCONFIG="$(runfile tests/npm_types_barename/own_import_ide_tsconfig.json)"

exec "${NODE}" "${TEST_MJS}" "${BUILD_TSCONFIG}" "${EDITOR_TSCONFIG}"
