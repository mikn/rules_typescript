#!/usr/bin/env bash
#
# Determinism verification script
#
# Builds the same target twice with different output bases and compares outputs.
# This ensures that the build is bit-for-bit reproducible.
#
# Usage:
#   bash scripts/verify_determinism.sh
#
# Note: Uses --output_base to avoid bazel clean (per project conventions)

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Create temporary directories for output bases
TMPDIR=${TMPDIR:-/tmp}
OUTPUT_BASE_1="$TMPDIR/bazel_det_1_$$"
OUTPUT_BASE_2="$TMPDIR/bazel_det_2_$$"

# Cleanup function
cleanup() {
  if [[ -d "$OUTPUT_BASE_1" ]]; then
    rm -rf "$OUTPUT_BASE_1"
  fi
  if [[ -d "$OUTPUT_BASE_2" ]]; then
    rm -rf "$OUTPUT_BASE_2"
  fi
}

trap cleanup EXIT

cd "$REPO_ROOT"

echo -e "${YELLOW}=== Determinism Check ===${NC}"
echo "Building //tests/smoke:hello twice with different output bases..."
echo

# Target to test
TARGET="//tests/smoke:hello"

# Build 1
echo -e "${YELLOW}[1/3] First build (output_base=$OUTPUT_BASE_1)...${NC}"
if ! bazel --output_base="$OUTPUT_BASE_1" build "$TARGET" >/dev/null 2>&1; then
  echo -e "${RED}✗ First build failed${NC}"
  exit 1
fi
echo -e "${GREEN}✓ First build succeeded${NC}"

# Find the output file (hello.js)
OUTPUT_FILE_1=$(find "$OUTPUT_BASE_1" -name "hello.js" -type f 2>/dev/null | head -1)
if [[ -z "$OUTPUT_FILE_1" ]]; then
  echo -e "${RED}✗ Could not find hello.js from first build${NC}"
  exit 1
fi
echo "Output: $OUTPUT_FILE_1"
echo

# Build 2
echo -e "${YELLOW}[2/3] Second build (output_base=$OUTPUT_BASE_2)...${NC}"
if ! bazel --output_base="$OUTPUT_BASE_2" build "$TARGET" >/dev/null 2>&1; then
  echo -e "${RED}✗ Second build failed${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Second build succeeded${NC}"

# Find the output file
OUTPUT_FILE_2=$(find "$OUTPUT_BASE_2" -name "hello.js" -type f 2>/dev/null | head -1)
if [[ -z "$OUTPUT_FILE_2" ]]; then
  echo -e "${RED}✗ Could not find hello.js from second build${NC}"
  exit 1
fi
echo "Output: $OUTPUT_FILE_2"
echo

# Compare
echo -e "${YELLOW}[3/3] Comparing outputs...${NC}"
if ! diff -u "$OUTPUT_FILE_1" "$OUTPUT_FILE_2"; then
  echo -e "${RED}✗ Outputs differ - build is not deterministic${NC}"
  echo
  echo "First build size:  $(wc -c < "$OUTPUT_FILE_1") bytes"
  echo "Second build size: $(wc -c < "$OUTPUT_FILE_2") bytes"
  exit 1
fi
echo -e "${GREEN}✓ Outputs are identical${NC}"
echo

# Verify hashes
HASH_1=$(sha256sum "$OUTPUT_FILE_1" | awk '{print $1}')
HASH_2=$(sha256sum "$OUTPUT_FILE_2" | awk '{print $1}')

echo -e "${YELLOW}=== Determinism Verified ===${NC}"
echo "Target: $TARGET"
echo "File size: $(wc -c < "$OUTPUT_FILE_1") bytes"
echo "SHA256: $HASH_1"
echo -e "${GREEN}✓ Build is deterministic (bit-for-bit reproducible)${NC}"
