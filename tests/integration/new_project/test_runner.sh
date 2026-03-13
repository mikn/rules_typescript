#!/usr/bin/env bash
# test_runner.sh — Integration test: create a fresh project, run Gazelle, build.
#
# The rules_bazel_integration_test framework provides:
#   BIT_BAZEL_BINARY  — absolute path to the Bazel binary to use
#   BIT_WORKSPACE_DIR — absolute path to the child workspace directory
#                       (in the runfiles tree, under _main/tests/integration/new_project)
#
# This test:
#   1. Derives RULES_TS_ROOT from BIT_WORKSPACE_DIR by walking up to _main/ and
#      resolving the MODULE.bazel symlink (which points at the real source checkout).
#   2. Copies the workspace to a writable scratch directory in /tmp.
#   3. Patches MODULE.bazel with the absolute path to rules_typescript root.
#   4. Runs `bazel run //:gazelle` to generate BUILD files.
#   5. Runs `bazel build //...` to compile and type-check.
#   6. Asserts expected output files exist.

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
# BIT_WORKSPACE_DIR is under the runfiles tree at a path ending with
# _main/tests/integration/new_project (where _main/ is the runfiles dir for the
# main Bazel repo). Strip the known suffix to get the _main/ runfiles root, then
# resolve the MODULE.bazel symlink there to find the actual source checkout.
_RUNFILES_MAIN="${BIT_WORKSPACE_DIR%/tests/integration/new_project}"
[[ "${_RUNFILES_MAIN}" != "${BIT_WORKSPACE_DIR}" ]] || {
    echo "FAIL: BIT_WORKSPACE_DIR does not end with expected suffix" >&2
    echo "      got: ${BIT_WORKSPACE_DIR}" >&2
    exit 1
}
_MODULE_IN_RUNFILES="${_RUNFILES_MAIN}/MODULE.bazel"

if [[ -L "${_MODULE_IN_RUNFILES}" ]]; then
    # Source file in runfiles is a symlink — resolve it to get the real checkout.
    RULES_TS_ROOT="$(dirname "$(_realpath "${_MODULE_IN_RUNFILES}")")"
elif [[ -f "${_MODULE_IN_RUNFILES}" ]]; then
    # Fallback for non-symlink runfiles mode (e.g. Windows).
    # The file is a regular copy; the source root cannot be derived from it.
    # Try the parent directory directly — it may be the real source.
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

# ── Create writable scratch workspace in /tmp ─────────────────────────────────
# Use /tmp to keep the nested workspace outside the outer Bazel execroot
# (avoids "repo contents cache is inside main repo" errors).
# Initialize vars and register trap BEFORE mktemp so cleanup always works.
SCRATCH_DIR=""
OUTPUT_BASE=""
cleanup() {
    [[ -n "${OUTPUT_BASE}" ]] && { chmod -R u+w "${OUTPUT_BASE}" 2>/dev/null || true; rm -rf "${OUTPUT_BASE}"; }
    [[ -n "${SCRATCH_DIR}" ]] && rm -rf "${SCRATCH_DIR}"
}
trap cleanup EXIT
SCRATCH_DIR="$(mktemp -d -t rules_ts_new_project.XXXXXX)"
OUTPUT_BASE="$(mktemp -d -t rules_ts_new_project_output.XXXXXX)"

# Copy workspace files. cp -rL dereferences symlinks from the runfiles tree.
cp -rL "${BIT_WORKSPACE_DIR}/." "${SCRATCH_DIR}/"
# Ensure dotfiles (.bazelrc, .bazelversion) are copied — they're matched by /. above
# on most systems, but be explicit for robustness.
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
    # Unset TEST_TMPDIR so the nested Bazel does not inherit the outer test's
    # temp dir (which sits inside the outer Bazel execroot, causing "repo
    # contents cache is inside main repo" errors).
    env -u TEST_TMPDIR "${BIT_BAZEL_BINARY}" --output_base="${OUTPUT_BASE}" "$@"
}

# ── Step 1: run Gazelle ───────────────────────────────────────────────────────
echo "INFO: running gazelle..."
bazel_cmd run //:gazelle || fail "bazel run //:gazelle exited non-zero"
pass "bazel run //:gazelle"

for dir in src/lib src/app; do
    [[ -f "${dir}/BUILD.bazel" ]] || fail "Gazelle did not generate ${dir}/BUILD.bazel"
    pass "${dir}/BUILD.bazel generated"
done

# ── Step 2: build (compile + type-check) ─────────────────────────────────────
echo "INFO: running bazel build //..."
bazel_cmd build //... || fail "bazel build //... exited non-zero"
pass "bazel build //..."

# ── Step 3: verify output files ──────────────────────────────────────────────
BAZEL_BIN="$(bazel_cmd info bazel-bin 2>/dev/null)"
for rel in "src/lib/math.js" "src/lib/math.d.ts" "src/app/index.js" "src/app/index.d.ts"; do
    f="${BAZEL_BIN}/${rel}"
    [[ -f "${f}" ]] || fail "expected output file not found: ${rel}"
    pass "output file exists: ${rel}"
done

echo "ALL PASSED"
