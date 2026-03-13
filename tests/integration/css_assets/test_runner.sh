#!/usr/bin/env bash
# test_runner.sh — Integration test: CSS modules, SVG assets, and JSON imports.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/css_assets)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR.
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Runs `bazel run //:gazelle` to generate BUILD files.
#   5. Verifies Gazelle generated css_module, asset_library, and json_library targets.
#   6. Runs `bazel build //...` — CSS module .d.ts must be generated and compilation
#      must succeed with the typed CSS module import.
#   7. Asserts the CSS module .d.ts output exists.

set -euo pipefail

# ── Validate framework env vars ───────────────────────────────────────────────
[[ -n "${BIT_BAZEL_BINARY:-}" ]]  || { echo "FAIL: BIT_BAZEL_BINARY not set" >&2; exit 1; }
[[ -n "${BIT_WORKSPACE_DIR:-}" ]] || { echo "FAIL: BIT_WORKSPACE_DIR not set" >&2; exit 1; }

echo "INFO: bazel           = ${BIT_BAZEL_BINARY}"
echo "INFO: workspace_dir   = ${BIT_WORKSPACE_DIR}"

# ── Derive RULES_TS_ROOT ──────────────────────────────────────────────────────
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/css_assets}"
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
SCRATCH_DIR="$(mktemp -d -t rules_ts_css_assets.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_css_assets_output.XXXXXX)"

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

[[ -f "src/components/BUILD.bazel" ]] || fail "Gazelle did not generate src/components/BUILD.bazel"
pass "src/components/BUILD.bazel generated"
cat src/components/BUILD.bazel

# ── Step 2: verify Gazelle generated expected rule types ──────────────────────
grep -q 'css_module' src/components/BUILD.bazel || \
    fail "src/components/BUILD.bazel does not contain css_module rule"
pass "src/components/BUILD.bazel has css_module target"

grep -q 'asset_library' src/components/BUILD.bazel || \
    fail "src/components/BUILD.bazel does not contain asset_library rule (for logo.svg)"
pass "src/components/BUILD.bazel has asset_library target"

grep -q 'json_library' src/components/BUILD.bazel || \
    fail "src/components/BUILD.bazel does not contain json_library rule (for config.json)"
pass "src/components/BUILD.bazel has json_library target"

# ── Step 3: build (compile + type-check) ─────────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero"
pass "bazel build //..."

# ── Step 4: verify CSS module .d.ts output ───────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"

CSS_DTS="${BAZEL_BIN}/src/components/button.module.css.d.ts"
[[ -f "${CSS_DTS}" ]] || fail "CSS module .d.ts not found: src/components/button.module.css.d.ts"
pass "CSS module .d.ts exists: src/components/button.module.css.d.ts"

# Verify the .d.ts contains expected class names from the CSS.
grep -q 'container' "${CSS_DTS}" || fail "CSS module .d.ts missing class 'container'"
grep -q 'button' "${CSS_DTS}" || fail "CSS module .d.ts missing class 'button'"
pass "CSS module .d.ts contains expected class names"

# ── Step 5: verify TypeScript compilation output ─────────────────────────────
BUTTON_JS="${BAZEL_BIN}/src/components/Button.js"
[[ -f "${BUTTON_JS}" ]] || fail "Button.js not found (ts_compile did not run)"
pass "src/components/Button.js exists"

echo "ALL PASSED"
