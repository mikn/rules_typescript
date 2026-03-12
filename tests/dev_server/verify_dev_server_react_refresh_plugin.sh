#!/usr/bin/env bash
# Verification script for ts_dev_server with react_refresh = True AND
# vite-plugin-bazel wired in (dev_with_react_refresh_and_plugin target).
#
# This is a dedicated script for the combined variant so that its specific
# config file is validated directly, without fallback logic that could mask
# failures by finding the simpler react_refresh-only variant's config.
#
# Checks:
#   1. The vite.config.mjs for dev_with_react_refresh_and_plugin exists in runfiles.
#   2. The config loads @vitejs/plugin-react via dynamic import (not static).
#   3. The config calls react() in the plugins array.
#   4. The config loads vite-plugin-bazel via VITE_PLUGIN_PATH (dynamic import).
#   5. Both plugins appear in the exported config object.
#   6. Standard vite config structure (export default, server:) is present.
set -euo pipefail

# Locate test inputs from runfiles.
RUNFILES="${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}"
WORKSPACE="${TEST_WORKSPACE:-_main}"
CONFIG="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_with_react_refresh_and_plugin_dev/vite.config.mjs"
RUNNER="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_with_react_refresh_and_plugin_runner.sh"
PLUGIN="${RUNFILES}/${WORKSPACE}/vite/vite_plugin_bazel.mjs"

# ── Check 1: config file exists ─────────────────────────────────────────────
if [[ ! -f "${CONFIG}" ]]; then
  echo "FAIL: vite.config.mjs not found: ${CONFIG}"
  exit 1
fi

echo "PASS: found combined react_refresh+plugin config: ${CONFIG}"

CONFIG_CONTENT="$(cat "${CONFIG}")"

# ── Check 2: uses dynamic import for @vitejs/plugin-react ───────────────────
# The config must NOT use a static bare-specifier import (which would fail in
# runfiles where there is no node_modules/ directory for ESM resolution).
if echo "${CONFIG_CONTENT}" | grep -q "^import react from '@vitejs/plugin-react'"; then
  echo "FAIL: vite.config.mjs uses a static bare-specifier import for @vitejs/plugin-react."
  echo "      This will throw ERR_MODULE_NOT_FOUND when run from runfiles."
  echo "      Use a dynamic import() with NODE_MODULES_PATH instead."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q "plugin-react"; then
  echo "FAIL: vite.config.mjs does not reference @vitejs/plugin-react at all."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q "await import"; then
  echo "FAIL: vite.config.mjs does not use dynamic import() for @vitejs/plugin-react."
  exit 1
fi

echo "PASS: vite.config.mjs loads @vitejs/plugin-react via dynamic import."

# ── Check 3: calls react() ──────────────────────────────────────────────────
if ! echo "${CONFIG_CONTENT}" | grep -q "plugins.push(react())"; then
  echo "FAIL: vite.config.mjs does not call react() plugin."
  exit 1
fi

echo "PASS: vite.config.mjs calls react() in the plugins array."

# ── Check 4: loads vite-plugin-bazel via VITE_PLUGIN_PATH ───────────────────
if ! echo "${CONFIG_CONTENT}" | grep -q 'VITE_PLUGIN_PATH'; then
  echo "FAIL: vite.config.mjs does not reference VITE_PLUGIN_PATH."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'bazelPlugin'; then
  echo "FAIL: vite.config.mjs does not reference bazelPlugin."
  exit 1
fi

echo "PASS: vite.config.mjs loads vite-plugin-bazel via VITE_PLUGIN_PATH."

# ── Check 5: plugins array is included in the exported config ───────────────
if ! echo "${CONFIG_CONTENT}" | grep -q "plugins,"; then
  echo "FAIL: vite.config.mjs does not include plugins in the exported config."
  exit 1
fi

echo "PASS: vite.config.mjs includes plugins in the exported config."

# ── Check 6: standard vite config structure still present ───────────────────
if ! echo "${CONFIG_CONTENT}" | grep -q 'export default'; then
  echo "FAIL: vite.config.mjs missing 'export default'."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'server:'; then
  echo "FAIL: vite.config.mjs missing 'server:' section."
  exit 1
fi

echo "PASS: standard vite config structure is present."

# ── Check 7: runner sets VITE_PLUGIN_PATH ───────────────────────────────────
if [[ ! -f "${RUNNER}" ]]; then
  echo "FAIL: runner script not found: ${RUNNER}"
  exit 1
fi

if ! grep -q 'VITE_PLUGIN_PATH' "${RUNNER}"; then
  echo "FAIL: runner script does not set VITE_PLUGIN_PATH."
  exit 1
fi

echo "PASS: runner script sets VITE_PLUGIN_PATH."

# ── Check 8: compiled plugin is in runfiles ──────────────────────────────────
if [[ ! -f "${PLUGIN}" ]]; then
  echo "FAIL: compiled plugin not found in runfiles: ${PLUGIN}"
  exit 1
fi

if ! grep -q 'bazelPlugin' "${PLUGIN}"; then
  echo "FAIL: plugin .mjs does not export bazelPlugin."
  exit 1
fi

echo "PASS: compiled plugin is in runfiles and exports bazelPlugin."

echo ""
echo "All ts_dev_server (react_refresh + plugin) checks passed."
