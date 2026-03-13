#!/usr/bin/env bash
# test_runner.sh — Integration test: existing project with isolated_declarations disabled.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/existing_project)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Runs `bazel run //:gazelle` to generate BUILD files.
#   5. Verifies Gazelle emitted isolated_declarations = False on generated targets.
#   6. Runs `bazel build //...` — functions without explicit return types must compile.
#   7. Asserts expected output files exist.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/existing_project}"
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
SCRATCH_DIR="$(mktemp -d -t rules_ts_existing_project.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_existing_project_output.XXXXXX)"

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

# ── Step 1: run Gazelle ───────────────────────────────────────────────────────
echo "INFO: running gazelle..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle exited non-zero"
pass "bazel run //:gazelle"

[[ -f "src/lib/BUILD.bazel" ]] || fail "Gazelle did not generate src/lib/BUILD.bazel"
pass "src/lib/BUILD.bazel generated"

# ── Step 2: verify Gazelle emitted isolated_declarations = False ──────────────
grep -q 'isolated_declarations.*=.*False\|isolated_declarations.*false' src/lib/BUILD.bazel || \
    fail "src/lib/BUILD.bazel does not contain isolated_declarations = False (directive not respected)"
pass "src/lib/BUILD.bazel has isolated_declarations = False"

# ── Step 3: build (compile + type-check) ─────────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero (functions without return types should still build)"
pass "bazel build //..."

# ── Step 4: verify output files ──────────────────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
for rel in "src/lib/math.js" "src/lib/math.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected output file not found: ${rel}"
    pass "output file exists: ${rel}"
done

echo "ALL PASSED"
