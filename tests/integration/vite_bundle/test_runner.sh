#!/usr/bin/env bash
# test_runner.sh — Integration test: Vite production bundle from scratch.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/vite_bundle)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Copies pnpm-lock.yaml from tests/npm/ in the rules_typescript checkout.
#   5. Runs `bazel run //:gazelle` to generate BUILD files for src/.
#   6. Runs `bazel build //:bundle` to invoke Vite and produce the bundle.
#   7. Asserts the bundle output directory exists.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/vite_bundle}"
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

NPM_LOCKFILE="${RULES_TS_ROOT}/tests/npm/pnpm-lock.yaml"
if [[ ! -f "${NPM_LOCKFILE}" ]]; then
    echo "FAIL: pnpm-lock.yaml not found at ${NPM_LOCKFILE}" >&2
    exit 1
fi

# ── Create writable scratch workspace in /tmp ─────────────────────────────────
SCRATCH_DIR="$(mktemp -d -t rules_ts_vite_bundle.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_vite_bundle_output.XXXXXX)"

cleanup() {
    chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true
    rm -rf "${SCRATCH_DIR}" "${OUTPUT_BASE}"
}
trap cleanup EXIT

cp -rL "${BIT_WORKSPACE_DIR}/." "${SCRATCH_DIR}/"
for f in "${BIT_WORKSPACE_DIR}"/.bazelrc "${BIT_WORKSPACE_DIR}"/.bazelversion; do
    [[ -e "${f}" ]] && cp -L "${f}" "${SCRATCH_DIR}/" || true
done

# ── Copy pnpm-lock.yaml ───────────────────────────────────────────────────────
cp "${NPM_LOCKFILE}" "${SCRATCH_DIR}/pnpm-lock.yaml"

# ── Patch MODULE.bazel ────────────────────────────────────────────────────────
sed -i "s|{RULES_TS_ROOT}|${RULES_TS_ROOT}|g" "${SCRATCH_DIR}/MODULE.bazel"

# ── Helpers ───────────────────────────────────────────────────────────────────
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cd "${SCRATCH_DIR}"

bazel_cmd() {
    env -u TEST_TMPDIR "${BIT_BAZEL_BINARY}" --output_base="${OUTPUT_BASE}" "$@"
}

# ── Step 1: run Gazelle to generate src/ BUILD files ─────────────────────────
echo "INFO: running gazelle..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle exited non-zero"
pass "bazel run //:gazelle"

for dir in src/lib src/app; do
    [[ -f "${dir}/BUILD.bazel" ]] || fail "Gazelle did not generate ${dir}/BUILD.bazel"
    pass "${dir}/BUILD.bazel generated"
done

# ── Step 2: build the Vite bundle ─────────────────────────────────────────────
echo "INFO: running bazel build //:bundle..."
bazel_cmd build //:bundle || fail "bazel build //:bundle exited non-zero"
pass "bazel build //:bundle"

# ── Step 3: verify the bundle output directory exists ─────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"

BUNDLE_DIR="${BAZEL_BIN}/bundle_bundle"
[[ -d "${BUNDLE_DIR}" ]] || fail "Vite bundle output directory not found: bundle_bundle/"
pass "bundle_bundle/ directory exists"

# Vite lib mode produces a .es.js file named after the bundle target.
BUNDLE_JS="${BUNDLE_DIR}/bundle.es.js"
[[ -f "${BUNDLE_JS}" ]] || fail "Vite bundle JS not found: bundle_bundle/bundle.es.js"
pass "bundle_bundle/bundle.es.js exists"

echo "ALL PASSED"
