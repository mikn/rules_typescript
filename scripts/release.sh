#!/usr/bin/env bash
#
# Release script for rules_typescript
#
# Automates the release process:
# 1. Updates MODULE.bazel version
# 2. Creates a git tag
# 3. Builds the tarball
# 4. Computes SHA256 hash
# 5. Updates .bcr/source.json with hash
#
# Usage:
#   bash scripts/release.sh <version>
#
# Example:
#   bash scripts/release.sh 0.2.0

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

if [[ $# -ne 1 ]]; then
  echo "Usage: bash scripts/release.sh <version>"
  echo "Example: bash scripts/release.sh 0.2.0"
  exit 1
fi

VERSION="$1"
TAG="v${VERSION}"

# Validate version format (basic check)
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9._]+)?$ ]]; then
  echo -e "${RED}✗ Invalid version format: $VERSION${NC}"
  echo "Expected format: X.Y.Z or X.Y.Z-prerelease (e.g., 0.2.0 or 0.2.0-rc.1)"
  exit 1
fi

cd "$REPO_ROOT"

echo -e "${YELLOW}=== rules_typescript Release v$VERSION ===${NC}"
echo

# Check if tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo -e "${RED}✗ Tag $TAG already exists${NC}"
  exit 1
fi

# Check if working tree is clean
if ! git diff-index --quiet HEAD --; then
  echo -e "${RED}✗ Working tree has uncommitted changes${NC}"
  exit 1
fi

# Step 1: Update MODULE.bazel version
echo -e "${YELLOW}[1/5] Updating MODULE.bazel version...${NC}"
if ! grep -q "version = \"" MODULE.bazel; then
  echo -e "${RED}✗ Could not find version field in MODULE.bazel${NC}"
  exit 1
fi

# Use sed to update the version (works on both macOS and Linux)
if [[ "$OSTYPE" == "darwin"* ]]; then
  sed -i '' "s/version = \"[^\"]*\"/version = \"$VERSION\"/" MODULE.bazel
else
  sed -i "s/version = \"[^\"]*\"/version = \"$VERSION\"/" MODULE.bazel
fi
echo -e "${GREEN}✓ Updated MODULE.bazel to version $VERSION${NC}"
echo

# Step 2: Commit and create tag
echo -e "${YELLOW}[2/5] Creating git tag...${NC}"
git add MODULE.bazel
git commit -m "Release v$VERSION"
git tag "$TAG" -m "rules_typescript $VERSION"
echo -e "${GREEN}✓ Created tag $TAG${NC}"
echo

# Step 3: Build tarball
echo -e "${YELLOW}[3/5] Building tarball...${NC}"
TARBALL_NAME="rules_typescript-${TAG}.tar.gz"
TARBALL_PATH="/tmp/$TARBALL_NAME"

# Create tarball excluding bazel-*, .git, .bazel* files
tar --exclude='bazel-*' \
    --exclude='.git' \
    --exclude='.bazel*' \
    --exclude='.claude' \
    --exclude='*.log' \
    --exclude='node_modules' \
    -czf "$TARBALL_PATH" \
    --transform "s,^,$TARBALL_NAME%.tar.gz/," \
    $(find . -maxdepth 1 -type f -o -type d -name "ts" -o -type d -name "npm" -o -type d -name "gazelle" -o -type d -name "vite" -o -type d -name "oxc_cli" -o -type d -name "eslint-plugin" -o -type d -name "tools" -o -type d -name ".bcr" -o -type f -name "*.bazel" -o -type f -name "*.md" | grep -v "^\.git")

if [[ ! -f "$TARBALL_PATH" ]]; then
  echo -e "${RED}✗ Failed to create tarball${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Created tarball: $TARBALL_PATH${NC}"
ls -lh "$TARBALL_PATH"
echo

# Step 4: Compute SHA256
echo -e "${YELLOW}[4/5] Computing SHA256 hash...${NC}"
SHA256=$(sha256sum "$TARBALL_PATH" | awk '{print $1}')
echo "SHA256: $SHA256"
echo -e "${GREEN}✓ Computed hash${NC}"
echo

# Step 5: Update .bcr/source.json
echo -e "${YELLOW}[5/5] Updating .bcr/source.json...${NC}"
if [[ ! -f .bcr/source.json ]]; then
  echo -e "${RED}✗ .bcr/source.json not found${NC}"
  exit 1
fi

# Update the integrity field in source.json using jq or a simple replacement
# For robustness, use a Python one-liner if jq is not available
if command -v jq &> /dev/null; then
  jq ".integrity = \"sha256-$(echo -n "$SHA256" | xxd -r -p | base64)\"" \
    .bcr/source.json > .bcr/source.json.tmp
  mv .bcr/source.json.tmp .bcr/source.json
else
  # Fallback: simple string replacement (works for the basic template)
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s/\"integrity\": \"\"/\"integrity\": \"sha256-$(echo -n "$SHA256" | xxd -r -p | base64)\"/" .bcr/source.json
  else
    sed -i "s/\"integrity\": \"\"/\"integrity\": \"sha256-$(echo -n "$SHA256" | xxd -r -p | base64)\"/" .bcr/source.json
  fi
fi

echo "Updated .bcr/source.json:"
cat .bcr/source.json
echo -e "${GREEN}✓ Updated source.json${NC}"
echo

# Step 6: Final summary
echo -e "${YELLOW}=== Release Complete ===${NC}"
echo -e "${GREEN}✓ Version: $VERSION${NC}"
echo -e "${GREEN}✓ Tag: $TAG${NC}"
echo -e "${GREEN}✓ Tarball: $TARBALL_NAME${NC}"
echo -e "${GREEN}✓ SHA256: $SHA256${NC}"
echo
echo "Next steps:"
echo "1. Push the tag: git push origin $TAG"
echo "2. Create a GitHub release: https://github.com/nicholasgasior/rules_typescript/releases/new?tag=$TAG"
echo "3. Attach tarball: $TARBALL_PATH"
echo "4. Submit to BCR: https://github.com/bazelbuild/bazel-central-registry/pulls"
echo "   - Include .bcr/metadata.json and .bcr/source.json in the PR"
