#!/usr/bin/env bash
# test_runner.sh — Integration test: existing project with no type annotations.
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
#   5. Verifies Gazelle emitted NO declarations attribute (the tsgo default).
#   6. Builds //src/lib — functions without explicit return types must compile.
#   7. Asserts expected output files exist.
#   8. Asserts the emitted .d.ts carry inferred return types, not widened ones.
#   9. Builds //src/broken — a real type error must fail a PLAIN bazel build,
#      with no --output_groups=+_validation, and must emit no .d.ts.

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
SCRATCH_DIR="$(mktemp -d -p "${TEST_TMPDIR:-${XDG_CACHE_HOME:-$HOME/.cache}}" -t rules_ts_existing_project.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -p "${TEST_TMPDIR:-${XDG_CACHE_HOME:-$HOME/.cache}}" -t rules_ts_existing_project_output.XXXXXX)"

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

# ── Step 2: verify Gazelle left the emitter at its default ───────────────────
if grep -q 'declarations' src/lib/BUILD.bazel; then
    echo "--- src/lib/BUILD.bazel ---" >&2
    cat src/lib/BUILD.bazel >&2
    fail "Gazelle emitted a declarations attribute; the tsgo default needs none"
fi
pass "src/lib/BUILD.bazel has no declarations attribute (tsgo default)"

# ── Step 3: build (compile + type-check) ─────────────────────────────────────
echo "INFO: running bazel build //src/lib:all"
bazel_cmd build //src/lib:all || fail "bazel build //src/lib:all exited non-zero (functions without return types should still build)"
pass "bazel build //src/lib:all"

# ── Step 4: verify output files ──────────────────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
for rel in "src/lib/math.js" "src/lib/math.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected output file not found: ${rel}"
    pass "output file exists: ${rel}"
done

# ── Step 5: the declarations must carry the inferred types ───────────────────
# This is the reason tsgo owns declaration emit. A syntactic emitter cannot
# infer these and would widen them, and nothing would report it until some
# consumer failed against `{}` or `unknown`.
DTS="${BAZEL_BIN}/src/lib/math.d.ts"
echo "--- src/lib/math.d.ts ---"
cat "${DTS}"
echo "------------------------"
for fn in add multiply subtract divide; do
    grep -qE "declare function ${fn}\\(a: number, b: number\\): number" "${DTS}" || \
        fail "${fn}() lost its inferred 'number' return type in math.d.ts"
    pass "${fn}(): number inferred in math.d.ts"
done
if grep -qE ':[[:space:]]*(unknown|\\{\\})[[:space:]]*;' "${DTS}"; then
    fail "math.d.ts widened an export to 'unknown' or '{}'"
fi
pass "math.d.ts contains no widened exports"

# ── Step 6: a real type error must fail a plain build ────────────────────────
# Note the absence of --output_groups=+_validation. Because the .d.ts are real
# outputs of the tsgo action, a type error is a build failure by construction.
echo "INFO: running bazel build //src/broken:all (expect FAILURE)"
BUILD_LOG="$(mktemp)"
if bazel_cmd build //src/broken:all >"${BUILD_LOG}" 2>&1; then
    echo "--- build output ---" >&2; cat "${BUILD_LOG}" >&2
    fail "//src/broken:all built successfully; a type error must fail the build"
fi
pass "//src/broken:all failed the build without --output_groups=+_validation"

grep -qE "TS[0-9]{4}|not assignable" "${BUILD_LOG}" || {
    echo "--- build output ---" >&2; cat "${BUILD_LOG}" >&2
    fail "build failed, but not with a type diagnostic"
}
pass "failure names the type error"

[[ ! -f "${BAZEL_BIN}/src/broken/type_error.d.ts" ]] || \
    fail "a failing target still produced src/broken/type_error.d.ts"
pass "no .d.ts was produced for the failing target"

echo "ALL PASSED"
