#!/usr/bin/env bash
# Verification script for ts_dev_server with the vite-plugin-bazel enabled.
#
# Checks:
#   1. The runner script references VITE_PLUGIN_PATH.
#   2. The vite.config.mjs imports the plugin dynamically.
#   3. The compiled plugin file is included in runfiles.
set -euo pipefail

# Locate test inputs from runfiles.
RUNFILES="${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}"
WORKSPACE="${TEST_WORKSPACE:-_main}"
RUNNER="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_with_plugin_runner.sh"
CONFIG="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_with_plugin_dev/vite.config.mjs"
PLUGIN="${RUNFILES}/${WORKSPACE}/vite/vite_plugin_bazel.mjs"

# ── Check 1: runner script references VITE_PLUGIN_PATH ─────────────────────
if [[ ! -f "${RUNNER}" ]]; then
  echo "FAIL: runner script not found: ${RUNNER}"
  exit 1
fi

RUNNER_CONTENT="$(cat "${RUNNER}")"

if ! echo "${RUNNER_CONTENT}" | grep -q 'VITE_PLUGIN_PATH'; then
  echo "FAIL: runner script does not set VITE_PLUGIN_PATH."
  exit 1
fi

echo "PASS: runner script references VITE_PLUGIN_PATH."

# ── Check 2: vite.config.mjs imports the plugin ─────────────────────────────
if [[ ! -f "${CONFIG}" ]]; then
  echo "FAIL: vite.config.mjs not found: ${CONFIG}"
  exit 1
fi

CONFIG_CONTENT="$(cat "${CONFIG}")"

if ! echo "${CONFIG_CONTENT}" | grep -q 'VITE_PLUGIN_PATH'; then
  echo "FAIL: vite.config.mjs does not reference VITE_PLUGIN_PATH."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'bazelPlugin'; then
  echo "FAIL: vite.config.mjs does not reference bazelPlugin."
  exit 1
fi

echo "PASS: vite.config.mjs imports the plugin."

# ── Check 3: compiled plugin file is in runfiles ─────────────────────────────
if [[ ! -f "${PLUGIN}" ]]; then
  echo "FAIL: compiled plugin not found in runfiles: ${PLUGIN}"
  exit 1
fi

PLUGIN_CONTENT="$(cat "${PLUGIN}")"

if ! echo "${PLUGIN_CONTENT}" | grep -q 'bazelPlugin'; then
  echo "FAIL: plugin .mjs does not export bazelPlugin."
  exit 1
fi

echo "PASS: compiled plugin is in runfiles and exports bazelPlugin."

echo ""
echo "All ts_dev_server (plugin) checks passed."
