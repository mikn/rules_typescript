#!/usr/bin/env bash
# test_runner.sh — Integration test: ts_dev_server builds correctly.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/dev_server)
#
# This test verifies that ts_dev_server:
#   1. Builds without error (the rule is an executable target).
#   2. The generated runner script exists in bazel-bin.
#   3. The generated vite.config.mjs exists in bazel-bin.
#
# We intentionally do NOT run `bazel run //:dev` because that would start an
# actual Vite dev server which requires network access and would block the test.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/dev_server}"
_MODULE_IN_RUNFILES="${_RUNFILES_MAIN}/MODULE.bazel"

if [[ -L "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(dirname "$(readlink -f "${_MODULE_IN_RUNFILES}")")"
elif [[ -f "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(readlink -f "$(dirname "${_MODULE_IN_RUNFILES}")")"
else
    echo "FAIL: cannot locate MODULE.bazel relative to BIT_WORKSPACE_DIR" >&2
    echo "      Tried: ${_MODULE_IN_RUNFILES}" >&2
    exit 1
fi

grep -q '"rules_typescript"' "${RULES_TS_ROOT}/MODULE.bazel" 2>/dev/null || {
    echo "FAIL: resolved RULES_TS_ROOT does not look like rules_typescript:" >&2
    echo "      ${RULES_TS_ROOT}" >&2
    exit 1
}
echo "INFO: rules_ts_root   = ${RULES_TS_ROOT}"

# ── Create writable scratch workspace in /tmp ─────────────────────────────────
SCRATCH_DIR="$(mktemp -d -t rules_ts_dev_server.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_dev_server_output.XXXXXX)"

cleanup() {
    chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true
    rm -rf "${SCRATCH_DIR}" "${OUTPUT_BASE}"
}
trap cleanup EXIT

cp -rL "${BIT_WORKSPACE_DIR}/." "${SCRATCH_DIR}/"
for f in "${BIT_WORKSPACE_DIR}"/.bazelrc "${BIT_WORKSPACE_DIR}"/.bazelversion; do
    [[ -e "${f}" ]] && cp -L "${f}" "${SCRATCH_DIR}/" || true
done

# ── Patch MODULE.bazel ────────────────────────────────────────────────────────
sed -i "s|{RULES_TS_ROOT}|${RULES_TS_ROOT}|g" "${SCRATCH_DIR}/MODULE.bazel"

# ── Helpers ───────────────────────────────────────────────────────────────────
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cd "${SCRATCH_DIR}"

bazel_cmd() {
    env -u TEST_TMPDIR "${BIT_BAZEL_BINARY}" --output_base="${OUTPUT_BASE}" "$@"
}

# ── Step 1: build the dev server target ──────────────────────────────────────
echo "INFO: running bazel build //:dev..."
bazel_cmd build //:dev || fail "bazel build //:dev exited non-zero"
pass "bazel build //:dev"

# ── Step 2: verify the runner script was generated ────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"

RUNNER="${BAZEL_BIN}/dev_runner.sh"
[[ -f "${RUNNER}" ]] || fail "dev server runner script not found: dev_runner.sh"
pass "dev_runner.sh exists"

# The runner script must be executable.
[[ -x "${RUNNER}" ]] || fail "dev_runner.sh is not executable"
pass "dev_runner.sh is executable"

# ── Step 3: verify the generated vite.config.mjs ─────────────────────────────
VITE_CONFIG="${BAZEL_BIN}/dev_dev/vite.config.mjs"
[[ -f "${VITE_CONFIG}" ]] || fail "vite.config.mjs not found: dev_dev/vite.config.mjs"
pass "dev_dev/vite.config.mjs exists"

# Verify the config contains key Vite configuration.
grep -q 'port: 5173' "${VITE_CONFIG}" || \
    fail "vite.config.mjs does not contain 'port: 5173'"
pass "vite.config.mjs contains port: 5173"

grep -q 'BUILD_WORKSPACE_DIRECTORY' "${VITE_CONFIG}" || \
    fail "vite.config.mjs does not reference BUILD_WORKSPACE_DIRECTORY"
pass "vite.config.mjs references BUILD_WORKSPACE_DIRECTORY"

# ── Step 4: also build //... to ensure the app compile target works ───────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero"
pass "bazel build //..."

echo "ALL PASSED"
