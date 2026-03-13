#!/usr/bin/env bash
# test_runner.sh — Integration test: npm dependency resolution.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/npm_deps)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Copies pnpm-lock.yaml from tests/npm/ in the rules_typescript checkout.
#   5. Runs `bazel run //:gazelle` to generate BUILD files.
#   6. Verifies Gazelle wired the zod dep.
#   7. Runs `bazel build //...` — npm deps must resolve and zod types must compile.
#   8. Runs `bazel test //...` — vitest tests must pass.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Portable realpath (macOS lacks readlink -f) ──────────────────────────────
_realpath() {
    python3 -c "import os,sys; print(os.path.realpath(sys.argv[1]))" "$1"
}

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/npm_deps}"
[[ "${_RUNFILES_MAIN}" != "${BIT_WORKSPACE_DIR}" ]] || {
    echo "FAIL: BIT_WORKSPACE_DIR does not end with expected suffix" >&2
    echo "      got: ${BIT_WORKSPACE_DIR}" >&2
    exit 1
}
_MODULE_IN_RUNFILES="${_RUNFILES_MAIN}/MODULE.bazel"

if [[ -L "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(dirname "$(_realpath "${_MODULE_IN_RUNFILES}")")"
elif [[ -f "${_MODULE_IN_RUNFILES}" ]]; then
    RULES_TS_ROOT="$(_realpath "$(dirname "${_MODULE_IN_RUNFILES}")")"
else
    echo "FAIL: cannot locate MODULE.bazel relative to BIT_WORKSPACE_DIR" >&2
    echo "      Tried: ${_MODULE_IN_RUNFILES}" >&2
    exit 1
fi

# Sanity check — the resolved root must actually be a rules_typescript checkout.
# Check for a directory that only exists in the real root (not in child workspaces).
[[ -d "${RULES_TS_ROOT}/oxc_cli" ]] || {
    echo "FAIL: RULES_TS_ROOT is wrong — oxc_cli dir not found:" >&2
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
# Initialize vars and register trap BEFORE mktemp so cleanup always works.
SCRATCH_DIR=""
OUTPUT_BASE=""
cleanup() {
    [[ -n "${OUTPUT_BASE}" ]] && { chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true; rm -rf "${OUTPUT_BASE}"; }
    [[ -n "${SCRATCH_DIR}" ]] && rm -rf "${SCRATCH_DIR}"
}
trap cleanup EXIT
SCRATCH_DIR="$(mktemp -d -t rules_ts_npm_deps.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_npm_deps_output.XXXXXX)"

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

# ── Step 1: run Gazelle ───────────────────────────────────────────────────────
echo "INFO: running gazelle..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle exited non-zero"
pass "bazel run //:gazelle"

[[ -f "src/models/BUILD.bazel" ]] || fail "Gazelle did not generate src/models/BUILD.bazel"
pass "src/models/BUILD.bazel generated"

grep -q "@npm//:zod" src/models/BUILD.bazel || fail "src/models/BUILD.bazel does not reference @npm//:zod"
pass "src/models/BUILD.bazel references @npm//:zod"

# ── Step 2: build ─────────────────────────────────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero (npm deps should resolve)"
pass "bazel build //..."

# ── Step 3: verify output files ──────────────────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
for rel in "src/models/user.js" "src/models/user.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected output file not found: ${rel}"
    pass "output file exists: ${rel}"
done

# ── Step 4: run tests ─────────────────────────────────────────────────────────
echo "INFO: running bazel test //..."
bazel_cmd test //... || fail "bazel test //... exited non-zero"
pass "bazel test //..."

echo "ALL PASSED"
