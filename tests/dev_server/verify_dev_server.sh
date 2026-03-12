#!/usr/bin/env bash
# Verification script for the ts_dev_server rule.
#
# This script checks that the generated runner script and vite.config.mjs
# are well-formed without actually starting the dev server (which would
# block the test).
#
# Checks performed:
#   1. The runner script exists and is executable.
#   2. The runner script contains the expected shebang and key fragments.
#   3. The vite.config.mjs exists and contains valid JavaScript structure.
set -euo pipefail

# Locate test inputs from runfiles.
RUNFILES="${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}"
WORKSPACE="${TEST_WORKSPACE:-_main}"
RUNNER="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_runner.sh"
CONFIG="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_dev/vite.config.mjs"

# ── Check 1: runner script exists and is executable ─────────────────────────
if [[ ! -f "${RUNNER}" ]]; then
  echo "FAIL: runner script not found: ${RUNNER}"
  exit 1
fi

if [[ ! -x "${RUNNER}" ]]; then
  echo "FAIL: runner script is not executable: ${RUNNER}"
  exit 1
fi

echo "PASS: runner script exists and is executable."

# ── Check 2: runner script contains expected content ────────────────────────
RUNNER_CONTENT="$(cat "${RUNNER}")"

if ! echo "${RUNNER_CONTENT}" | grep -q '#!/usr/bin/env bash'; then
  echo "FAIL: runner script missing bash shebang."
  exit 1
fi

if ! echo "${RUNNER_CONTENT}" | grep -qE 'vite\.js|VITE_BIN.*dev|dev.*VITE_BIN'; then
  echo "FAIL: runner script does not reference vite or invoke dev mode."
  exit 1
fi

if ! echo "${RUNNER_CONTENT}" | grep -q 'vite.config.mjs'; then
  echo "FAIL: runner script does not reference vite.config.mjs."
  exit 1
fi

if ! echo "${RUNNER_CONTENT}" | grep -q 'BAZEL_BIN_DIR'; then
  echo "FAIL: runner script does not set BAZEL_BIN_DIR."
  exit 1
fi

if ! echo "${RUNNER_CONTENT}" | grep -q 'BUILD_WORKSPACE_DIRECTORY'; then
  echo "FAIL: runner script does not reference BUILD_WORKSPACE_DIRECTORY."
  exit 1
fi

echo "PASS: runner script content looks correct."

# ── Check 3: vite.config.mjs exists and is valid ────────────────────────────
if [[ ! -f "${CONFIG}" ]]; then
  echo "FAIL: vite.config.mjs not found: ${CONFIG}"
  exit 1
fi

CONFIG_CONTENT="$(cat "${CONFIG}")"

if ! echo "${CONFIG_CONTENT}" | grep -q 'export default'; then
  echo "FAIL: vite.config.mjs missing 'export default'."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'server:'; then
  echo "FAIL: vite.config.mjs missing 'server:' section."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'port:'; then
  echo "FAIL: vite.config.mjs missing 'port:' setting."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'BAZEL_BIN_DIR'; then
  echo "FAIL: vite.config.mjs does not reference BAZEL_BIN_DIR."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'BUILD_WORKSPACE_DIRECTORY'; then
  echo "FAIL: vite.config.mjs does not reference BUILD_WORKSPACE_DIRECTORY."
  exit 1
fi

echo "PASS: vite.config.mjs content looks correct."

echo ""
echo "All ts_dev_server checks passed."
