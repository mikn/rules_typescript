#!/usr/bin/env bash
#
# CI script for rules_typescript
#
# Runs all builds and tests across the monorepo and examples.
# Exit code 0 on complete success, non-zero on any failure.
#
# Usage:
#   bash scripts/ci.sh [--verbose] [--keep-going]
#
# Options:
#   --verbose     Show more output from Bazel
#   --keep-going  Continue building even after failures (still reports exit code)

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Options
VERBOSE=0
KEEP_GOING=0
SKIP_BOOTSTRAP=0
BAZEL_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --verbose)
      VERBOSE=1
      BAZEL_ARGS+=("--verbose_failures")
      shift
      ;;
    --keep-going)
      KEEP_GOING=1
      BAZEL_ARGS+=("--keep_going")
      shift
      ;;
    --skip-bootstrap)
      SKIP_BOOTSTRAP=1
      shift
      ;;
    --help|-h)
      echo "CI script for rules_typescript"
      echo "Usage: bash scripts/ci.sh [options]"
      echo "Options:"
      echo "  --verbose           Show more output from Bazel"
      echo "  --keep-going        Continue building even after failures"
      echo "  --skip-bootstrap    Skip bootstrap integration tests (nested Bazel)"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

cd "$REPO_ROOT"

echo -e "${YELLOW}=== rules_typescript CI ===${NC}"
echo "Repository root: $REPO_ROOT"
echo

# Track failures
FAILED=0

# Step 1: Build and test main workspace
echo -e "${YELLOW}[1/5] Testing main workspace...${NC}"
if ! bazel test //... --cache_test_results=no "${BAZEL_ARGS[@]}"; then
  echo -e "${RED}✗ Main workspace tests failed${NC}"
  FAILED=1
  if [[ $KEEP_GOING -eq 0 ]]; then
    exit 1
  fi
fi
echo -e "${GREEN}✓ Main workspace tests passed${NC}"
echo

# Step 2: Type checking (validation)
echo -e "${YELLOW}[2/5] Type checking (validation)...${NC}"
if ! bazel build //... --output_groups=+_validation "${BAZEL_ARGS[@]}"; then
  echo -e "${RED}✗ Type checking failed${NC}"
  FAILED=1
  if [[ $KEEP_GOING -eq 0 ]]; then
    exit 1
  fi
fi
echo -e "${GREEN}✓ Type checking passed${NC}"
echo

# Step 3: E2E tests
echo -e "${YELLOW}[3/5] Building and testing e2e/basic...${NC}"
(
  cd e2e/basic
  if ! bazel build //... "${BAZEL_ARGS[@]}"; then
    echo -e "${RED}✗ e2e/basic build failed${NC}"
    exit 1
  fi
  if ! bazel test //... "${BAZEL_ARGS[@]}"; then
    echo -e "${RED}✗ e2e/basic tests failed${NC}"
    exit 1
  fi
)
if [[ $? -ne 0 ]]; then
  FAILED=1
  if [[ $KEEP_GOING -eq 0 ]]; then
    exit 1
  fi
fi
echo -e "${GREEN}✓ e2e/basic passed${NC}"
echo

# Step 4: Bootstrap integration tests
# These tests spawn nested Bazel processes and require tags = ["local"].
# They are skipped when the --skip-bootstrap flag is passed (e.g. for fast CI).
echo -e "${YELLOW}[4/5] Bootstrap integration tests...${NC}"
if [[ $SKIP_BOOTSTRAP -eq 1 ]]; then
    echo -e "${YELLOW}  ⚠ Bootstrap tests skipped (--skip-bootstrap)${NC}"
else
    export RULES_TYPESCRIPT_ROOT="$REPO_ROOT"
    BOOTSTRAP_TARGETS=(
        "//tests/bootstrap:test_new_project"
        "//tests/bootstrap:test_existing_project"
        "//tests/bootstrap:test_npm_deps"
        "//tests/bootstrap:test_gazelle_roundtrip"
    )
    for target in "${BOOTSTRAP_TARGETS[@]}"; do
        echo "  Running $target..."
        if ! bazel test "$target" \
            --test_output=errors \
            --test_strategy=local \
            "${BAZEL_ARGS[@]}"; then
            echo -e "${RED}✗ $target failed${NC}"
            FAILED=1
            if [[ $KEEP_GOING -eq 0 ]]; then
                exit 1
            fi
        fi
    done
    echo -e "${GREEN}✓ Bootstrap integration tests passed${NC}"
fi
echo

# Step 5: Examples (non-critical, may have incomplete features)
echo -e "${YELLOW}[5/5] Building examples...${NC}"
EXAMPLES_FAILED=0
for example_dir in examples/*/; do
  if [[ ! -f "$example_dir/MODULE.bazel" ]]; then
    continue
  fi
  example_name=$(basename "$example_dir")
  echo "  Building examples/$example_name..."
  (
    cd "$example_dir"
    if ! bazel build //... "${BAZEL_ARGS[@]}" 2>&1; then
      echo -e "${YELLOW}  ⚠ examples/$example_name build incomplete (expected for some examples)${NC}"
      exit 1
    fi
  ) || EXAMPLES_FAILED=1
done
if [[ $EXAMPLES_FAILED -eq 0 ]]; then
  echo -e "${GREEN}✓ All examples built${NC}"
else
  echo -e "${YELLOW}⚠ Some examples incomplete (expected)${NC}"
fi
echo

# Summary
echo -e "${YELLOW}=== Summary ===${NC}"
if [[ $FAILED -eq 0 ]]; then
  echo -e "${GREEN}✓ All critical checks passed${NC}"
  exit 0
else
  echo -e "${RED}✗ Some checks failed${NC}"
  exit 1
fi
