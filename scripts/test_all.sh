#!/usr/bin/env bash
# test_all.sh — Run all tests and build examples for rules_typescript.
#
# Usage:
#   bash scripts/test_all.sh [--fast]
#
# Options:
#   --fast   Skip slow tests (type_error_detected_test which spawns nested Bazel)
#
# Requires:
#   - Must be run from the workspace root (where MODULE.bazel lives)
#   - bazel must be in PATH
#   - Bazel 9.0.0+

set -euo pipefail

# ── Helpers ───────────────────────────────────────────────────────────────────

WORKSPACE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE_ROOT"

if [ -t 1 ]; then
  GREEN='\033[0;32m'; RED='\033[0;31m'; CYAN='\033[0;36m'; YELLOW='\033[1;33m'; RESET='\033[0m'
else
  GREEN=''; RED=''; CYAN=''; YELLOW=''; RESET=''
fi

pass() { echo -e "${GREEN}PASS${RESET}  $*"; }
fail() { echo -e "${RED}FAIL${RESET}  $*"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${CYAN}INFO${RESET}  $*"; }
section() { echo; echo -e "${YELLOW}=== $* ===${RESET}"; }

FAILURES=0
FAST_MODE=0
for arg in "$@"; do
  [[ "$arg" == "--fast" ]] && FAST_MODE=1
done

# ── Part 1: Main repo tests ───────────────────────────────────────────────────

section "Main repo: bazel test //tests/..."

# Run the fast tests (exclude type_error_detected_test in fast mode since it
# spawns a nested Bazel server which takes ~30 seconds)
if [[ "$FAST_MODE" -eq 1 ]]; then
  info "Fast mode: excluding type_error_detected_test"
  EXTRA_FILTERS="--test_tag_filters=-exclusive"
else
  EXTRA_FILTERS=""
fi

if bazel test //tests/... $EXTRA_FILTERS 2>&1; then
  pass "bazel test //tests/..."
else
  fail "bazel test //tests/... — one or more tests failed"
fi

# ── Part 2: Build with validation (type-checking) ────────────────────────────

section "Main repo: bazel build //tests/... --output_groups=+_validation"

if bazel build //tests/... --output_groups=+_validation 2>&1; then
  pass "bazel build //tests/... with --output_groups=+_validation"
else
  fail "bazel build //tests/... with --output_groups=+_validation — type errors found"
fi

# ── Part 3: e2e workspace ─────────────────────────────────────────────────────

section "E2E workspace: e2e/basic"

if (cd e2e/basic && bazel build //... 2>&1); then
  pass "e2e/basic: bazel build //..."
else
  fail "e2e/basic: bazel build //..."
fi

# ── Part 4: examples/basic workspace ─────────────────────────────────────────

section "Examples workspace: examples/basic"

if (cd examples/basic && bazel build //... 2>&1); then
  pass "examples/basic: bazel build //..."
else
  fail "examples/basic: bazel build //..."
fi

# ── Summary ───────────────────────────────────────────────────────────────────

echo
echo "═══════════════════════════════════════════════════════════════"
echo "  rules_typescript — Full Test Suite Results"
echo "═══════════════════════════════════════════════════════════════"

if [[ "$FAILURES" -eq 0 ]]; then
  echo -e "${GREEN}All tests passed.${RESET}"
  exit 0
else
  echo -e "${RED}$FAILURES suite(s) FAILED.${RESET}"
  exit 1
fi
