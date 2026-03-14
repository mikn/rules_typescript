#!/usr/bin/env bash
# test_resolution_map.sh — Verify the tsserver-hook worker builds a correct
# resolution map from the rules_typescript workspace.
#
# Usage (direct):
#   bash tests/lsp/test_resolution_map.sh
#
# Usage (via Bazel):
#   bazel test //tests/lsp:test_resolution_map --test_output=all
#
# What it tests:
#   1. The hook script loads without errors.
#   2. The worker builds a resolution map that contains npm packages (zod,
#      vitest) pointing at real .d.ts files in the Bazel output base.
#   3. At least one resolution entry points at an existing file on disk.
#
# Prerequisites for the npm-package checks to pass:
#   bazel build @npm//...   (or bazel build //...) must have run at least
#   once to populate the @npm external repo in the Bazel output base.
#
# Exit code: 0 = all assertions passed, non-zero = failure.

set -euo pipefail

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

# ── Locate files ───────────────────────────────────────────────────────────────
# When run via `bazel test`, the runfiles tree is set up and the DATA deps
# (//tools:tsserver-hook.js, //tools:tsserver-hook-worker.js) are accessible
# via the standard runfiles layout under $RUNFILES_DIR or $TEST_SRCDIR.
#
# When run directly (bash tests/lsp/test_resolution_map.sh from workspace root),
# we use relative paths.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Portable realpath (macOS lacks readlink -f).
_realpath() {
  python3 -c "import os,sys; print(os.path.realpath(sys.argv[1]))" "$1"
}

# Detect Bazel test environment.
if [[ -n "${TEST_SRCDIR:-}" ]]; then
  # Bazel test: tools/ is under the _main runfiles repo.
  _RUNFILES_MAIN="${TEST_SRCDIR}/_main"
  [[ -d "${_RUNFILES_MAIN}" ]] || _RUNFILES_MAIN="${TEST_SRCDIR}"
  TOOLS_DIR="${_RUNFILES_MAIN}/tools"

  # Derive the real workspace root from the MODULE.bazel symlink.
  # The test has `data = ["//:MODULE.bazel"]` which causes MODULE.bazel to
  # appear as a symlink in the runfiles tree pointing at the real source file.
  _MODULE_SYMLINK="${_RUNFILES_MAIN}/MODULE.bazel"
  if [[ -L "${_MODULE_SYMLINK}" ]]; then
    WORKSPACE_ROOT="$(dirname "$(_realpath "${_MODULE_SYMLINK}")")"
  elif [[ -f "${_MODULE_SYMLINK}" ]]; then
    WORKSPACE_ROOT="$(_realpath "$(dirname "${_MODULE_SYMLINK}")")"
  else
    # Fallback: assume workspace root is the real rules_typescript checkout,
    # which we derive by resolving the SCRIPT_DIR symlink chain.
    WORKSPACE_ROOT="$(dirname "$(dirname "$(_realpath "${SCRIPT_DIR}")")")"
  fi
else
  # Direct invocation: tools/ is two directories up from this script.
  WORKSPACE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  TOOLS_DIR="${WORKSPACE_ROOT}/tools"
fi

[[ -f "${TOOLS_DIR}/tsserver-hook.js" ]] || \
    fail "tsserver-hook.js not found at ${TOOLS_DIR}/tsserver-hook.js"
[[ -f "${TOOLS_DIR}/tsserver-hook-worker.js" ]] || \
    fail "tsserver-hook-worker.js not found at ${TOOLS_DIR}/tsserver-hook-worker.js"

# Derive the Bazel output base so the worker can locate the @npm external repo
# without needing to run `bazel info output_base` (which is blocked by the
# outer `bazel test` server lock).
#
# Strategy:
#   1. If running inside a Bazel test, derive the output base from the
#      runfiles directory path (which is always under
#      <output_base>/execroot/_main/bazel-out/.../bin/...).
#   2. Fall back to `bazel info output_base` for direct invocations.
BAZEL_OUTPUT_BASE=""

if [[ -n "${TEST_SRCDIR:-}" ]]; then
  # Inside Bazel test: TEST_SRCDIR is something like
  #   <output_base>/execroot/_main/bazel-out/.../bin/<target>.runfiles
  # Extract output_base by stripping everything from /execroot/ onward.
  if [[ "${TEST_SRCDIR}" == */execroot/* ]]; then
    BAZEL_OUTPUT_BASE="${TEST_SRCDIR%%/execroot/*}"
  fi
fi

if [[ -z "${BAZEL_OUTPUT_BASE}" ]]; then
  # Direct invocation or fallback: ask Bazel.
  BAZEL_OUTPUT_BASE="$(bazel info output_base 2>/dev/null || true)"
fi

echo "INFO: workspace_root    = ${WORKSPACE_ROOT}"
echo "INFO: tools_dir         = ${TOOLS_DIR}"
echo "INFO: bazel_output_base = ${BAZEL_OUTPUT_BASE:-<not found>}"

pass "hook files exist"

# ── Prerequisite: Node.js available ───────────────────────────────────────────
command -v node >/dev/null 2>&1 || fail "node not found on PATH"
NODE_VERSION=$(node --version)
echo "INFO: node ${NODE_VERSION}"

# ── Test 1: Hook script loads without errors ──────────────────────────────────
echo "INFO: testing that hook loads without errors..."
node --require "${TOOLS_DIR}/tsserver-hook.js" --eval "process.exit(0)"
pass "hook loads without errors"

# ── Test 2: Run worker and capture resolution map ─────────────────────────────
echo "INFO: running worker to build resolution map (may take up to 60 s)..."

RESOLUTION_MAP_FILE="$(mktemp -t resolution_map.XXXXXX.json)"
WORKER_SCRIPT="$(mktemp -t tsserver_hook_test.XXXXXX.js)"
cleanup() {
  rm -f "${RESOLUTION_MAP_FILE}" "${WORKER_SCRIPT}"
}
trap cleanup EXIT

# Write the worker-runner as a temp file to avoid bash/JS interpolation issues.
cat > "${WORKER_SCRIPT}" << WORKER_EOF
'use strict';
const { Worker } = require('worker_threads');
const path = require('path');
const fs = require('fs');

const workerPath = path.resolve(process.argv[2]);
const workspaceRoot = process.argv[3];
const outputFile = process.argv[4];
const outputBase = process.argv[5] || '';  // optional, passed when Bazel lock is held

const workerData = { workspaceRoot };
if (outputBase) workerData.outputBase = outputBase;

const w = new Worker(workerPath, { workerData });

const timeout = setTimeout(() => {
    w.terminate();
    process.stderr.write('FAIL: worker did not send resolution map within 60 s\\n');
    process.exit(1);
}, 60000);

w.on('message', (msg) => {
    clearTimeout(timeout);
    w.terminate();
    if (msg.type !== 'resolution-map') {
        process.stderr.write('FAIL: unexpected message type: ' + msg.type + '\\n');
        process.exit(1);
    }
    fs.writeFileSync(outputFile, JSON.stringify(msg.data, null, 2));
    process.exit(0);
});

w.on('error', (err) => {
    clearTimeout(timeout);
    process.stderr.write('FAIL: worker error: ' + err.message + '\\n');
    process.exit(1);
});
WORKER_EOF

node "${WORKER_SCRIPT}" \
    "${TOOLS_DIR}/tsserver-hook-worker.js" \
    "${WORKSPACE_ROOT}" \
    "${RESOLUTION_MAP_FILE}" \
    "${BAZEL_OUTPUT_BASE:-}"

[[ -s "${RESOLUTION_MAP_FILE}" ]] || fail "resolution map file is empty"
pass "worker produced a resolution map"

echo "INFO: resolution map written to ${RESOLUTION_MAP_FILE}"

# ── Test 3: Check npm packages are present ─────────────────────────────────────
echo "INFO: checking npm packages in resolution map..."

python3 - "${RESOLUTION_MAP_FILE}" << 'PYEOF'
import json
import os
import sys

map_path = sys.argv[1]

with open(map_path) as f:
    data = json.load(f)

errors = []

# zod and vitest are in the rules_typescript test lockfile.
# They must be present after `bazel build @npm//...`.
REQUIRED_PACKAGES = ["zod", "vitest"]

for pkg in REQUIRED_PACKAGES:
    if pkg not in data:
        errors.append("'{}' not in resolution map".format(pkg))
    else:
        dts_path = data[pkg]
        if not os.path.exists(dts_path):
            errors.append("'{}' path does not exist: {!r}".format(pkg, dts_path))
        elif not (dts_path.endswith('.d.ts') or
                  dts_path.endswith('.d.mts') or
                  dts_path.endswith('.d.cts')):
            errors.append("'{}' path is not a .d.ts file: {!r}".format(pkg, dts_path))
        else:
            print("PASS: '{}' -> {!r}".format(pkg, dts_path))

module_entries = [k for k in data.keys() if not k.startswith('__alias__')]
alias_entries = [k for k in data.keys() if k.startswith('__alias__')]
print("INFO: {} module entries, {} alias entries".format(
    len(module_entries), len(alias_entries)))

if errors:
    for e in errors:
        print("FAIL: {}".format(e), file=sys.stderr)
    sys.exit(1)

sys.exit(0)
PYEOF
pass "npm packages present in resolution map"

# ── Test 4: At least one module points at a real .d.ts ────────────────────────
echo "INFO: verifying .d.ts paths exist on disk..."

python3 - "${RESOLUTION_MAP_FILE}" << 'PYEOF'
import json
import os
import sys

with open(sys.argv[1]) as f:
    data = json.load(f)

valid = 0
invalid = 0
for k, v in data.items():
    if k.startswith('__alias__'):
        continue
    if os.path.exists(v):
        valid += 1
    else:
        invalid += 1

print("INFO: {} paths exist, {} paths missing (may be unbuilt packages)".format(
    valid, invalid))

if valid == 0:
    print("FAIL: no resolution map entries point to existing files", file=sys.stderr)
    sys.exit(1)

sys.exit(0)
PYEOF
pass "at least one resolution entry points to an existing file"

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "ALL PASSED"
