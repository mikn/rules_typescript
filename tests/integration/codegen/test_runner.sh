#!/usr/bin/env bash
# test_runner.sh — Integration test: ts_codegen → ts_compile pipeline.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/codegen)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Runs `bazel build //...` which:
#      a. Runs generate.sh as a Bazel action (ts_codegen) to produce generated/record.ts.
#      b. Compiles the generated TypeScript (ts_compile).
#   5. Verifies the generated TypeScript source and compiled outputs exist.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/codegen}"
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
SCRATCH_DIR="$(mktemp -d -t rules_ts_codegen.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_codegen_output.XXXXXX)"

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

# ── Step 1: build everything ──────────────────────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero"
pass "bazel build //..."

# ── Step 2: verify generated TypeScript source ────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"

# ts_codegen output: the generated .ts file.
GENERATED_TS="${BAZEL_BIN}/generated/record.ts"
[[ -f "${GENERATED_TS}" ]] || fail "generated TypeScript not found: generated/record.ts"
pass "generated/record.ts exists"

# Verify generator actually wrote TypeScript content.
grep -q 'GeneratedRecord' "${GENERATED_TS}" || \
    fail "generated/record.ts does not contain expected interface 'GeneratedRecord'"
pass "generated/record.ts contains GeneratedRecord interface"

# ── Step 3: verify compiled outputs ──────────────────────────────────────────
for rel in "generated/record.js" "generated/record.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected compiled output not found: ${rel}"
    pass "output file exists: ${rel}"
done

echo "ALL PASSED"
