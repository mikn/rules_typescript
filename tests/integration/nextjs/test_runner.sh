#!/usr/bin/env bash
# test_runner.sh — Integration test: next_build produces a .next/ directory.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/nextjs)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Copies pnpm-lock.yaml from examples/nextjs-app/ in the rules_typescript
#      checkout (Next.js + React lockfile).
#   5. Runs `bazel build //:app` to invoke next build.
#   6. Asserts the .next/ output directory exists with expected contents.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Portable realpath ─────────────────────────────────────────────────────────
_realpath() {
    python3 -c "import os,sys; print(os.path.realpath(sys.argv[1]))" "$1"
}

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/nextjs}"
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

[[ -d "${RULES_TS_ROOT}/oxc_cli" ]] || {
    echo "FAIL: RULES_TS_ROOT is wrong — oxc_cli dir not found:" >&2
    echo "      ${RULES_TS_ROOT}" >&2
    exit 1
}
echo "INFO: rules_ts_root   = ${RULES_TS_ROOT}"

# The pnpm-lock.yaml for Next.js lives in examples/nextjs-app/.
NPM_LOCKFILE="${RULES_TS_ROOT}/examples/nextjs-app/pnpm-lock.yaml"
if [[ ! -f "${NPM_LOCKFILE}" ]]; then
    echo "FAIL: pnpm-lock.yaml not found at ${NPM_LOCKFILE}" >&2
    exit 1
fi

# ── Create writable scratch workspace in /tmp ─────────────────────────────────
SCRATCH_DIR=""
OUTPUT_BASE=""
cleanup() {
    [[ -n "${OUTPUT_BASE}" ]] && { chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true; rm -rf "${OUTPUT_BASE}"; }
    [[ -n "${SCRATCH_DIR}" ]] && rm -rf "${SCRATCH_DIR}"
}
trap cleanup EXIT
SCRATCH_DIR="$(mktemp -d -t rules_ts_nextjs.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_nextjs_output.XXXXXX)"

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

# ── Step 1: build the Next.js app ─────────────────────────────────────────────
echo "INFO: running bazel build //:app..."
bazel_cmd build //:app || fail "bazel build //:app exited non-zero"
pass "bazel build //:app"

# ── Step 2: verify the .next output directory exists ─────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"

NEXT_OUT="${BAZEL_BIN}/app_next_out"
[[ -d "${NEXT_OUT}" ]] || fail "next_build output directory not found: app_next_out/"
pass "app_next_out/ directory exists"

# Next.js always produces these directories in the output.
for subdir in "server" "static"; do
    [[ -d "${NEXT_OUT}/${subdir}" ]] || fail ".next/${subdir}/ not found in output"
    pass ".next/${subdir}/ exists"
done

# BUILD_ID is written by Next.js to identify the build.
[[ -f "${NEXT_OUT}/BUILD_ID" ]] || fail ".next/BUILD_ID not found"
pass ".next/BUILD_ID exists"

# ── Step 3: verify the cache directory was stripped ───────────────────────────
# .next/cache/ must not be present — it is non-hermetic and would pollute the
# Bazel remote cache with machine-specific data.
[[ ! -d "${NEXT_OUT}/cache" ]] || fail ".next/cache/ must be excluded from output (non-hermetic)"
pass ".next/cache/ correctly excluded from output"

# The staging directory must not leak into the output.
[[ ! -d "${NEXT_OUT}/_staging" ]] || fail "_staging/ must be cleaned up from output"
pass "_staging/ correctly absent from output"

# ── Step 4: verify compiled route output ─────────────────────────────────────
# At least one compiled page JS file must exist under server/
COMPILED_PAGES="$(find "${NEXT_OUT}/server" -name "*.js" 2>/dev/null | grep -c "page" || true)"
[[ "${COMPILED_PAGES}" -gt 0 ]] || fail "no compiled page JS found under .next/server/"
pass "compiled page JS files found (${COMPILED_PAGES})"

# The greeting function from src/lib/greeting.ts must appear in the server output.
# This proves that staging_srcs are correctly picked up and compiled by Next.js.
if grep -r --include="*.js" -q "Hello" "${NEXT_OUT}/server/" 2>/dev/null; then
    pass "greeting string found in server output (staging_srcs compiled correctly)"
else
    fail "greeting string not found in server output — staging_srcs may not have been staged"
fi

echo "ALL PASSED"
