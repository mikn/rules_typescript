#!/usr/bin/env bash
# test_runner.sh — Integration test: declarations = "oxc" refuses un-annotated exports.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace in the runfiles tree
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory.
#   3. Patches MODULE.bazel with the rules_typescript root.
#   4. Runs Gazelle and asserts it emitted declarations = "oxc".
#   5. Builds the annotated target — must SUCCEED.
#   6. Builds the un-annotated target — must FAIL, and the failure must name
#      isolated declarations rather than being some unrelated breakage.

set -euo pipefail

[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel         = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir = ${BIT_WORKSPACE_DIR}"

_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/oxc_declarations}"
_MODULE_IN_RUNFILES="${_RUNFILES_MAIN}/MODULE.bazel"
if [[ -L "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(dirname "$(readlink -f "${_MODULE_IN_RUNFILES}")")"
elif [[ -f "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(readlink -f "$(dirname "${_MODULE_IN_RUNFILES}")")"
else
    echo "FAIL: cannot locate MODULE.bazel relative to BIT_WORKSPACE_DIR" >&2
    exit 1
fi
grep -q '"rules_typescript"' "${RULES_TS_ROOT}/MODULE.bazel" 2>/dev/null || {
    echo "FAIL: resolved RULES_TS_ROOT does not look like rules_typescript: ${RULES_TS_ROOT}" >&2
    exit 1
}
echo "INFO: rules_ts_root = ${RULES_TS_ROOT}"

SCRATCH_DIR="$(mktemp -d -t rules_ts_oxc_decls.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_oxc_decls_output.XXXXXX)"
cleanup() {
    chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true
    rm -rf "${SCRATCH_DIR}" "${OUTPUT_BASE}"
}
trap cleanup EXIT

cp -rL "${BIT_WORKSPACE_DIR}/." "${SCRATCH_DIR}/"
for f in "${BIT_WORKSPACE_DIR}"/.bazelrc "${BIT_WORKSPACE_DIR}"/.bazelversion; do
    [[ -e "${f}" ]] && cp -L "${f}" "${SCRATCH_DIR}/" || true
done
sed -i "s|{RULES_TS_ROOT}|${RULES_TS_ROOT}|g" "${SCRATCH_DIR}/MODULE.bazel"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cd "${SCRATCH_DIR}"
bazel_cmd() { env -u TEST_TMPDIR "${BIT_BAZEL_BINARY}" --output_base="${OUTPUT_BASE}" "$@"; }

# ── Step 1: Gazelle ──────────────────────────────────────────────────────────
bazel_cmd run //:gazelle || fail "bazel run //:gazelle exited non-zero"
pass "bazel run //:gazelle"

[[ -f "src/lib/BUILD.bazel" ]] || fail "Gazelle did not generate src/lib/BUILD.bazel"
[[ -f "src/bad/BUILD.bazel" ]] || fail "Gazelle did not generate src/bad/BUILD.bazel"

# ── Step 2: the directive must reach the generated rules ─────────────────────
for f in src/lib/BUILD.bazel src/bad/BUILD.bazel; do
    grep -q 'declarations = "oxc"' "${f}" || {
        echo "--- ${f} ---" >&2; cat "${f}" >&2
        fail "${f} is missing declarations = \"oxc\" (ts_declarations directive not respected)"
    }
    pass "${f} has declarations = \"oxc\""
done

# ── Step 3: annotated sources build ──────────────────────────────────────────
bazel_cmd build //src/lib:all || fail "annotated target failed to build under declarations = \"oxc\""
pass "annotated target builds under declarations = \"oxc\""

BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
DTS="${BAZEL_BIN}/src/lib/annotated.d.ts"
[[ -f "${DTS}" ]] || fail "oxc did not emit src/lib/annotated.d.ts"
grep -q "RegExp" "${DTS}" || fail "annotated.d.ts lost the RegExp type"
pass "oxc emitted annotated.d.ts with its declared types"

# ── Step 4: un-annotated sources must FAIL, not widen ────────────────────────
# No --output_groups=+_validation: oxc itself must refuse.
BUILD_LOG="$(mktemp)"
if bazel_cmd build //src/bad:all >"${BUILD_LOG}" 2>&1; then
    echo "--- build output ---" >&2; cat "${BUILD_LOG}" >&2
    BAD_DTS="${BAZEL_BIN}/src/bad/inferred.d.ts"
    if [[ -f "${BAD_DTS}" ]]; then
        echo "--- emitted src/bad/inferred.d.ts ---" >&2; cat "${BAD_DTS}" >&2
    fi
    fail "un-annotated exports built under declarations = \"oxc\"; they must be rejected, never widened"
fi
pass "un-annotated exports were rejected under declarations = \"oxc\""

grep -qiE "isolated declarations|TS901[0-9]|TS90[0-9][0-9]" "${BUILD_LOG}" || {
    echo "--- build output ---" >&2; cat "${BUILD_LOG}" >&2
    fail "build failed, but not with an isolated-declarations diagnostic"
}
pass "failure names the isolated-declarations problem"

echo "ALL PASSED"
