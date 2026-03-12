#!/usr/bin/env bash
# Verifies that ts_npm_publish produces the expected files.
set -euo pipefail

# Bazel sets TEST_SRCDIR to the runfiles root.
# The runfiles workspace is under TEST_SRCDIR/_main for the main repo.
RUNFILES_DIR="${TEST_SRCDIR}/_main"

# The staging directory name is <rule_name>_pkg (declared with declare_directory).
PKG_DIR="${RUNFILES_DIR}/tests/npm_publish/math_pkg_pkg"
# The tarball is <rule_name>_pkg.tar.
TARBALL="${RUNFILES_DIR}/tests/npm_publish/math_pkg_pkg.tar"

if [[ ! -d "$PKG_DIR" ]]; then
  echo "ERROR: Staging directory not found: $PKG_DIR" >&2
  echo "Contents of runfiles dir:" >&2
  ls -la "${RUNFILES_DIR}/tests/npm_publish/" >&2
  exit 1
fi

if [[ ! -f "$TARBALL" ]]; then
  echo "ERROR: Tarball not found: $TARBALL" >&2
  exit 1
fi

echo "Checking staging directory: $PKG_DIR"

# Check required files exist in the staging directory.
for required in package.json math.js math.js.map math.d.ts; do
  if [[ ! -f "$PKG_DIR/$required" ]]; then
    echo "ERROR: Expected $required in staging directory" >&2
    ls -la "$PKG_DIR" >&2
    exit 1
  fi
done

# Check that version was stamped correctly.
version=$(python3 -c "import json; d=json.load(open('$PKG_DIR/package.json')); print(d['version'])")
if [[ "$version" != "1.0.0" ]]; then
  echo "ERROR: Expected version 1.0.0 in package.json, got: $version" >&2
  exit 1
fi

echo "Checking tarball: $TARBALL"

# Check tarball contains the expected files with package/ prefix.
tar_contents=$(tar tf "$TARBALL")
for required in package/package.json package/math.js package/math.js.map package/math.d.ts; do
  if ! echo "$tar_contents" | grep -q "^${required}$"; then
    echo "ERROR: Expected $required in tarball" >&2
    echo "Tarball contents:" >&2
    echo "$tar_contents" >&2
    exit 1
  fi
done

echo "PASS: ts_npm_publish produced correct outputs"
