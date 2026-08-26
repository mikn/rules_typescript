#!/usr/bin/env bash
# add_package_wrapper_test.sh — the command line //:add_package_* hands pnpm,
# and the two refusals that stand between a missing hub and a package.json
# written at the workspace root.
#
# pnpm itself is a stub: `pnpm add` needs a registry, and the point of the test
# is the argv, not the resolution.

set -uo pipefail

fail() {
  echo "FAIL: $*" >&2
  exit 1
}
pass() { echo "PASS: $*"; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
if [[ -d "${RUNFILES}/${TEST_WORKSPACE}" ]]; then
  RUNFILES="${RUNFILES}/${TEST_WORKSPACE}"
fi

WRAPPER="${RUNFILES}/tools/add_package_wrapper.sh"
[[ -f "${WRAPPER}" ]] || fail "missing runfile: ${WRAPPER}"

# The wrapper resolves PNPM_BIN through rlocation, so this is a runfiles path.
FAKE_PNPM="${TEST_WORKSPACE}/tests/pnpm/fake_pnpm.sh"
[[ -x "${TEST_SRCDIR}/${FAKE_PNPM}" ]] || fail "stub pnpm is not executable: ${TEST_SRCDIR}/${FAKE_PNPM}"

# A workspace with a hub in it and, like this repository, nothing resembling a
# package at its root.
WS="${TEST_TMPDIR}/workspace"
rm -rf "${WS}"
mkdir -p "${WS}/tests/hub"
echo '{"name":"hub"}' >"${WS}/tests/hub/package.json"

# One whose hub is the root, which is the shape of a single-hub workspace.
ROOT_WS="${TEST_TMPDIR}/root_workspace"
rm -rf "${ROOT_WS}"
mkdir -p "${ROOT_WS}"
echo '{"name":"root"}' >"${ROOT_WS}/package.json"

run_wrapper() {
  local workspace="$1" hub="$2"
  shift 2
  if [[ "${hub}" == "<unset>" ]]; then
    env -u PNPM_HUB_DIR \
      BUILD_WORKSPACE_DIRECTORY="${workspace}" \
      PNPM_BIN="${FAKE_PNPM}" \
      bash "${WRAPPER}" "$@" 2>&1
  else
    env BUILD_WORKSPACE_DIRECTORY="${workspace}" \
      PNPM_BIN="${FAKE_PNPM}" \
      PNPM_HUB_DIR="${hub}" \
      bash "${WRAPPER}" "$@" 2>&1
  fi
}

# ── the hub the target names is the hub pnpm edits ────────────────────────────
if ! out="$(run_wrapper "${WS}" "tests/hub" zod)"; then
  fail "wrapper exited non-zero for a hub that exists: ${out}"
fi
[[ "${out}" == *"argv=add zod --lockfile-only --dir tests/hub"* ]] ||
  fail "pnpm was not pointed at the hub: ${out}"
[[ "${out}" == *"cwd=${WS}"* ]] || fail "pnpm did not start in the workspace root: ${out}"
pass "pnpm add is given --dir <hub>"

if ! out="$(run_wrapper "${WS}" "tests/hub" -D typescript)"; then
  fail "wrapper exited non-zero: ${out}"
fi
[[ "${out}" == *"argv=add -D typescript --lockfile-only --dir tests/hub"* ]] ||
  fail "user flags did not survive ahead of the appended ones: ${out}"
pass "user arguments keep their place"

if ! out="$(run_wrapper "${ROOT_WS}" "." zod)"; then
  fail "wrapper exited non-zero for a root hub: ${out}"
fi
[[ "${out}" == *"argv=add zod --lockfile-only --dir ."* ]] ||
  fail "a root hub is still passed explicitly: ${out}"
pass "a workspace-root hub is named too"

# ── the refusals ──────────────────────────────────────────────────────────────
if out="$(run_wrapper "${WS}" "<unset>" zod)"; then
  fail "wrapper ran pnpm with no hub: ${out}"
fi
[[ "${out}" == *"PNPM_HUB_DIR"* ]] || fail "the error does not name what is missing: ${out}"
pass "no hub is refused"

# The reported bug: this is the workspace root, and pnpm would create the
# package.json and pnpm-lock.yaml it did not find.
if out="$(run_wrapper "${WS}" "." zod)"; then
  fail "wrapper ran pnpm against a directory with no package.json: ${out}"
fi
[[ "${out}" == *"./package.json"* ]] || fail "the error does not name the directory: ${out}"
pass "a hub with no package.json is refused"
