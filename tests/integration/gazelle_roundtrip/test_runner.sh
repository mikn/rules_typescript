#!/usr/bin/env bash
# test_runner.sh — Integration test: Gazelle roundtrip.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/gazelle_roundtrip)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Runs `bazel run //:gazelle` to generate BUILD files (first pass).
#   5. Saves the generated BUILD files.
#   6. Deletes the generated BUILD files.
#   7. Runs `bazel run //:gazelle` again (second pass).
#   8. Diffs the two passes — output must be identical.
#   9. Runs `bazel build //...` — the regenerated BUILD files must produce a working build.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/gazelle_roundtrip}"
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
SCRATCH_DIR="$(mktemp -d -t rules_ts_gazelle_roundtrip.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_gazelle_roundtrip_output.XXXXXX)"
SNAPSHOT_DIR="$(mktemp -d -t rules_ts_gazelle_roundtrip_snap.XXXXXX)"

cleanup() {
    chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true
    rm -rf "${SCRATCH_DIR}" "${OUTPUT_BASE}" "${SNAPSHOT_DIR}"
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

# ── Step 1: first Gazelle pass ────────────────────────────────────────────────
echo "INFO: running gazelle (pass 1)..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle (pass 1) exited non-zero"
pass "gazelle pass 1 complete"

# Verify expected BUILD files were created.
for dir in src/lib src/app; do
    [[ -f "${dir}/BUILD.bazel" ]] || fail "Gazelle did not generate ${dir}/BUILD.bazel"
    pass "${dir}/BUILD.bazel generated (pass 1)"
done

# ── Step 2: snapshot the generated BUILD files ────────────────────────────────
echo "INFO: snapshotting generated BUILD files..."
for dir in src/lib src/app; do
    mkdir -p "${SNAPSHOT_DIR}/${dir}"
    cp "${SCRATCH_DIR}/${dir}/BUILD.bazel" "${SNAPSHOT_DIR}/${dir}/BUILD.bazel"
done
pass "BUILD files snapshotted"

# ── Step 3: delete the generated BUILD files ─────────────────────────────────
echo "INFO: deleting generated BUILD files..."
for dir in src/lib src/app; do
    rm "${SCRATCH_DIR}/${dir}/BUILD.bazel"
done
pass "BUILD files deleted"

# ── Step 4: second Gazelle pass ───────────────────────────────────────────────
echo "INFO: running gazelle (pass 2)..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle (pass 2) exited non-zero"
pass "gazelle pass 2 complete"

# ── Step 5: diff the two passes ───────────────────────────────────────────────
echo "INFO: diffing pass 1 vs pass 2..."
DIFF_FAILED=0
for dir in src/lib src/app; do
    if ! diff -u "${SNAPSHOT_DIR}/${dir}/BUILD.bazel" "${SCRATCH_DIR}/${dir}/BUILD.bazel"; then
        echo "FAIL: ${dir}/BUILD.bazel differs between pass 1 and pass 2" >&2
        DIFF_FAILED=1
    else
        pass "${dir}/BUILD.bazel is identical across both Gazelle runs"
    fi
done
[[ "${DIFF_FAILED}" -eq 0 ]] || fail "Gazelle output is not idempotent — see diffs above"

# ── Step 6: build with regenerated BUILD files ────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero (regenerated BUILD files should work)"
pass "bazel build //..."

# ── Step 7: verify output files ──────────────────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
for rel in "src/lib/math.js" "src/lib/math.d.ts" "src/app/index.js" "src/app/index.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected output file not found: ${rel}"
    pass "output file exists: ${rel}"
done

echo "ALL PASSED"
