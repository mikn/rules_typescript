#!/usr/bin/env bash
# Verification script for ts_dev_server with react_refresh = True.
#
# Validates the dev_with_react_refresh target specifically.
#
# Checks:
#   1. The vite.config.mjs for dev_with_react_refresh exists in runfiles.
#   2. The config loads @vitejs/plugin-react via dynamic import (not static).
#   3. The config calls react() in the plugins array.
#   4. The plugins array is included in the exported config object.
#   5. Standard vite config structure (export default, server:) is present.
set -euo pipefail

# Locate test inputs from runfiles.
RUNFILES="${RUNFILES_DIR:-${BASH_SOURCE[0]}.runfiles}"
WORKSPACE="${TEST_WORKSPACE:-_main}"
CONFIG="${RUNFILES}/${WORKSPACE}/tests/dev_server/dev_with_react_refresh_dev/vite.config.mjs"

# ── Check 1: config file exists ─────────────────────────────────────────────
if [[ ! -f "${CONFIG}" ]]; then
  echo "FAIL: vite.config.mjs not found: ${CONFIG}"
  exit 1
fi

echo "PASS: found react_refresh config: ${CONFIG}"

CONFIG_CONTENT="$(cat "${CONFIG}")"

# ── Check 2: uses dynamic import for @vitejs/plugin-react ───────────────────
# The config must NOT use a static bare-specifier import (which would fail in
# runfiles where there is no node_modules/ directory for ESM resolution).
# It must instead use a dynamic import() with the NODE_MODULES_PATH-derived path.
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

# ── Check 4: plugins array is included in the exported config ───────────────
if ! echo "${CONFIG_CONTENT}" | grep -q "plugins,"; then
  echo "FAIL: vite.config.mjs does not include plugins in the exported config."
  exit 1
fi

echo "PASS: vite.config.mjs includes plugins in the exported config."

# ── Check 5: standard vite config structure still present ───────────────────
if ! echo "${CONFIG_CONTENT}" | grep -q 'export default'; then
  echo "FAIL: vite.config.mjs missing 'export default'."
  exit 1
fi

if ! echo "${CONFIG_CONTENT}" | grep -q 'server:'; then
  echo "FAIL: vite.config.mjs missing 'server:' section."
  exit 1
fi

echo "PASS: standard vite config structure is present."

echo ""
echo "All ts_dev_server (react_refresh) checks passed."
