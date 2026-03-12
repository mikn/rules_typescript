#!/usr/bin/env bash
# verify_incremental.sh — Phase 6: Incremental build verification
#
# Tests that Bazel's content-addressed caching correctly skips downstream
# rebuilds when a change does NOT affect the .d.ts compilation boundary,
# and correctly rebuilds downstream packages when the .d.ts does change.
#
# Usage:
#   cd /path/to/rules_typescript
#   bash tests/synthetic/verify_incremental.sh
#
# Requirements:
#   - Must be run from the workspace root (where MODULE.bazel lives)
#   - Python 3 must be in PATH (used by generate.py)
#   - bazel must be in PATH
#
# The diamond dependency graph used (20 packages):
#
#           pkg_00 (base)
#          /    |    |   \
#      pkg_01 02 03 04 (mid_a)
#          |    |    |   |   \
#      pkg_05 06 07 08 09 (mid_b)
#       /  \  /  \  /  \  /  \  / \
#      pkg_10 11 12 13 14 15 16 17 18 19 (leaf)
#
# Test 1: Implementation-only change (pkg_10/utils.ts, no .d.ts change)
#   Expected: ONLY pkg_10 recompiles (2 actions: OxcCompile + TsgoCheck)
#
# Test 2: API change (pkg_05/types.ts, changes .d.ts)
#   Expected: pkg_05 + its 4 direct leaf dependents recompile
#             (pkg_10, pkg_14, pkg_15, pkg_19) → 5 packages × 2 = 10 actions

set -euo pipefail

# ─── helpers ──────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SYNTHETIC_DIR="$SCRIPT_DIR"

# Colour codes (disabled when not a terminal)
if [ -t 1 ]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; RESET='\033[0m'
else
  RED=''; GREEN=''; CYAN=''; YELLOW=''; RESET=''
fi

pass() { echo -e "${GREEN}PASS${RESET}  $*"; }
fail() { echo -e "${RED}FAIL${RESET}  $*"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${CYAN}INFO${RESET}  $*"; }

FAILURES=0

# extract_linux_sandbox <log_file> — count "N linux-sandbox" from bazel output
extract_linux_sandbox() {
  local log="$1"
  # Match e.g. "3 linux-sandbox" in "INFO: 4 processes: 1 internal, 3 linux-sandbox."
  grep -oP '\d+(?= linux-sandbox)' "$log" | head -1 || echo "0"
}

extract_total_actions() {
  local log="$1"
  grep -oP '\d+(?= total actions)' "$log" | head -1 || echo "0"
}

# ─── step 0: ensure packages are generated ────────────────────────────────────

info "Regenerating synthetic packages..."
cd "$WORKSPACE_ROOT"
python3 "$SYNTHETIC_DIR/generate.py" --out-dir "$SYNTHETIC_DIR" >/dev/null
info "Packages generated."

# ─── step 1: warm build ───────────────────────────────────────────────────────

info "Running warm build to populate Bazel cache..."
WARM_START=$(date +%s%N)
bazel build //tests/synthetic/... --output_groups=+_validation 2>&1 | tee /tmp/warm_build.log
WARM_END=$(date +%s%N)
WARM_WALL_MS=$(( (WARM_END - WARM_START) / 1000000 ))
WARM_ACTIONS=$(extract_total_actions /tmp/warm_build.log)
info "Warm build completed in ${WARM_WALL_MS}ms  (${WARM_ACTIONS} total actions)"

# No-op check.
info "Verifying no-op rebuild..."
bazel build //tests/synthetic/... --output_groups=+_validation 2>&1 | tee /tmp/noop_build.log >/dev/null
NOOP_ACTIONS=$(extract_total_actions /tmp/noop_build.log)
if [ "${NOOP_ACTIONS:-1}" -le 1 ]; then
  pass "No-op rebuild: ${NOOP_ACTIONS} total actions (expected ≤1)"
else
  fail "No-op rebuild should be ≤1 actions, got: $NOOP_ACTIONS"
fi

# ─── test 1: implementation-only change ───────────────────────────────────────

echo
info "=== Test 1: Implementation-only change (pkg_10/utils.ts) ==="

UTILS_FILE="$SYNTHETIC_DIR/pkg_10/utils.ts"

# Capture .d.ts before the change.
DTS_BEFORE=$(cat "$WORKSPACE_ROOT/bazel-bin/tests/synthetic/pkg_10/utils.d.ts" 2>/dev/null || echo "")

# Patch: change "raw * 10," to "raw * 10 + 0," — same semantics, different source.
# This is a targeted single-line replacement that cannot accidentally match imports.
python3 - <<PYEOF
path = "$UTILS_FILE"
with open(path) as f:
    lines = f.readlines()
for i, line in enumerate(lines):
    if "value: raw * 10," in line:
        lines[i] = line.replace("raw * 10,", "raw * 10 + 0, // implementation-only tweak")
        break
with open(path, "w") as f:
    f.writelines(lines)
print("  Patched:", path)
PYEOF

info "Incremental rebuild after implementation-only change..."
T1_START=$(date +%s%N)
bazel build //tests/synthetic/... --output_groups=+_validation 2>&1 | tee /tmp/test1_build.log
T1_END=$(date +%s%N)
T1_WALL_MS=$(( (T1_END - T1_START) / 1000000 ))
T1_TOTAL=$(extract_total_actions /tmp/test1_build.log)
T1_LINUX=$(extract_linux_sandbox /tmp/test1_build.log)
info "Rebuild completed in ${T1_WALL_MS}ms  (${T1_TOTAL} total, ${T1_LINUX} linux-sandbox actions)"

if [ "$T1_LINUX" -eq 2 ]; then
  pass "Test 1a: Exactly 2 linux-sandbox actions (OxcCompile + TsgoCheck for pkg_10 only)"
else
  fail "Test 1a: Expected 2 linux-sandbox actions, got $T1_LINUX"
fi

DTS_AFTER=$(cat "$WORKSPACE_ROOT/bazel-bin/tests/synthetic/pkg_10/utils.d.ts" 2>/dev/null || echo "")
if [ "$DTS_BEFORE" = "$DTS_AFTER" ]; then
  pass "Test 1b: utils.d.ts unchanged — .d.ts boundary correctly preserved implementation detail"
else
  fail "Test 1b: utils.d.ts changed after implementation-only edit"
fi

# Restore pkg_10/utils.ts.
python3 - <<PYEOF
path = "$UTILS_FILE"
with open(path) as f:
    lines = f.readlines()
for i, line in enumerate(lines):
    if "raw * 10 + 0," in line:
        lines[i] = line.replace("raw * 10 + 0, // implementation-only tweak", "raw * 10,")
        break
with open(path, "w") as f:
    f.writelines(lines)
print("  Restored:", path)
PYEOF

# Re-warm after restore so test 2 starts from a clean state.
bazel build //tests/synthetic/... --output_groups=+_validation >/dev/null 2>&1

# ─── test 2: API change (changes .d.ts) ───────────────────────────────────────

echo
info "=== Test 2: API change (pkg_05/types.ts — changes public interface) ==="
info "pkg_05 is depended on by: pkg_10, pkg_14, pkg_15, pkg_19 (4 leaf packages)"

TYPES_FILE="$SYNTHETIC_DIR/pkg_05/types.ts"
DTS_05_BEFORE=$(cat "$WORKSPACE_ROOT/bazel-bin/tests/synthetic/pkg_05/types.d.ts" 2>/dev/null || echo "")

# Patch: insert a new optional field inside the Value05 interface.
# We target the specific line "  base01: Value01;" and add after it.
python3 - <<PYEOF
path = "$TYPES_FILE"
with open(path) as f:
    lines = f.readlines()
for i, line in enumerate(lines):
    if "base01: Value01;" in line:
        lines.insert(i + 1, "  version?: number; // API change: new optional field\n")
        break
with open(path, "w") as f:
    f.writelines(lines)
print("  Patched:", path)
PYEOF

info "Incremental rebuild after API change to pkg_05..."
T2_START=$(date +%s%N)
bazel build //tests/synthetic/... --output_groups=+_validation 2>&1 | tee /tmp/test2_build.log
T2_END=$(date +%s%N)
T2_WALL_MS=$(( (T2_END - T2_START) / 1000000 ))
T2_TOTAL=$(extract_total_actions /tmp/test2_build.log)
T2_LINUX=$(extract_linux_sandbox /tmp/test2_build.log)
info "Rebuild completed in ${T2_WALL_MS}ms  (${T2_TOTAL} total, ${T2_LINUX} linux-sandbox actions)"

# pkg_05 (1) + pkg_10, pkg_14, pkg_15, pkg_19 (4) = 5 packages × 2 actions = 10
if [ "$T2_LINUX" -eq 10 ]; then
  pass "Test 2a: Exactly 10 linux-sandbox actions (5 packages × 2: pkg_05 + 4 dependents)"
else
  fail "Test 2a: Expected 10 linux-sandbox actions (5 pkgs × 2), got $T2_LINUX"
fi

DTS_05_AFTER=$(cat "$WORKSPACE_ROOT/bazel-bin/tests/synthetic/pkg_05/types.d.ts" 2>/dev/null || echo "")
if [ "$DTS_05_BEFORE" != "$DTS_05_AFTER" ]; then
  pass "Test 2b: pkg_05/types.d.ts changed as expected after API change"
else
  fail "Test 2b: pkg_05/types.d.ts should have changed but did not"
fi

# Verify sibling mid_b packages NOT in pkg_05's cone did NOT recompile.
# Action count of 10 already implies this; add an explicit diagnostic note.
info "Sibling mid_b packages (pkg_06..09) have no dependency on pkg_05;"
info "their actions are absent from the 10-action count, confirming cache hit."

# Restore pkg_05/types.ts.
python3 - <<PYEOF
path = "$TYPES_FILE"
with open(path) as f:
    lines = f.readlines()
lines = [l for l in lines if "version?: number;" not in l]
with open(path, "w") as f:
    f.writelines(lines)
print("  Restored:", path)
PYEOF

# Re-warm.
bazel build //tests/synthetic/... --output_groups=+_validation >/dev/null 2>&1

# ─── test 3: no-op after all restores ─────────────────────────────────────────

echo
info "=== Test 3: No-op rebuild after all restores ==="
bazel build //tests/synthetic/... --output_groups=+_validation 2>&1 | tee /tmp/noop2_build.log >/dev/null
NOOP2_ACTIONS=$(extract_total_actions /tmp/noop2_build.log)
if [ "${NOOP2_ACTIONS:-1}" -le 1 ]; then
  pass "Test 3: No-op rebuild: ${NOOP2_ACTIONS} total actions"
else
  fail "Test 3: No-op rebuild should be ≤1 actions, got: $NOOP2_ACTIONS"
fi

# ─── existing tests still pass ────────────────────────────────────────────────

echo
info "=== Regression check: //tests/... still builds ==="
bazel build //tests/... --output_groups=+_validation 2>&1 | tee /tmp/regression_build.log | tail -5
REG_STATUS=$?
if [ "$REG_STATUS" -eq 0 ]; then
  pass "Regression: //tests/... builds successfully"
else
  fail "Regression: //tests/... build failed"
fi

# ─── summary ──────────────────────────────────────────────────────────────────

echo
echo "═══════════════════════════════════════════════════════════════"
echo "  Phase 6 Incremental Build Verification — Results"
echo "═══════════════════════════════════════════════════════════════"
echo "  Warm build:          ${WARM_WALL_MS}ms  (${WARM_ACTIONS} total actions)"
echo "  Test 1 (impl-only):  ${T1_WALL_MS}ms  (${T1_LINUX} linux-sandbox actions, expected 2)"
echo "  Test 2 (API change): ${T2_WALL_MS}ms  (${T2_LINUX} linux-sandbox actions, expected 10)"
echo "═══════════════════════════════════════════════════════════════"

if [ "$FAILURES" -eq 0 ]; then
  echo -e "${GREEN}All tests passed.${RESET}"
  exit 0
else
  echo -e "${RED}$FAILURES test(s) FAILED.${RESET}"
  exit 1
fi
