#!/usr/bin/env bash
# Verifies that multi-version npm packages generate versioned Bazel targets.
#
# The pnpm-lock.yaml used for these tests includes @vitest/pretty-format at
# two different versions (3.0.9 and 3.2.4), which exercises the multi-version
# label generation logic in npm_translate_lock.
#
# Expected labels generated:
#   @npm//:vitest_pretty-format_3_0_9   -- versioned target for 3.0.9
#   @npm//:vitest_pretty-format_3_2_4   -- versioned target for 3.2.4
#   @npm//:vitest_pretty-format         -- alias → highest version (3.2.4)
set -euo pipefail

if [[ -n "${RUNFILES_DIR:-}" ]]; then
  : # already set by Bazel
elif [[ -n "${TEST_SRCDIR:-}" ]]; then
  RUNFILES_DIR="$TEST_SRCDIR"
else
  echo "ERROR: RUNFILES_DIR not set" >&2
  exit 1
fi

cd "${RUNFILES_DIR}"

# Helper: find the extracted package directory for a given versioned label.
# The directory is named after the label (with __ separator for version).
find_pkg_dir() {
  local label_dir="$1"
  for prefix in "+npm+npm" "npm+npm" "external/+npm+npm" "_main/external/+npm+npm"; do
    if [[ -d "${prefix}/${label_dir}" ]]; then
      echo "${prefix}/${label_dir}"
      return 0
    fi
  done
  return 1
}

echo "Checking versioned label directories for @vitest/pretty-format..."

DIR_309=""
DIR_324=""

for candidate in $(find . -maxdepth 3 -name "vitest_pretty-format__3_0_9" -type d 2>/dev/null); do
  DIR_309="$candidate"
  break
done

for candidate in $(find . -maxdepth 3 -name "vitest_pretty-format__3_2_4" -type d 2>/dev/null); do
  DIR_324="$candidate"
  break
done

if [[ -z "$DIR_309" ]]; then
  echo "ERROR: could not find vitest_pretty-format__3_0_9 directory in runfiles" >&2
  echo "Runfiles root contents:" >&2
  ls -la . >&2
  exit 1
fi

if [[ -z "$DIR_324" ]]; then
  echo "ERROR: could not find vitest_pretty-format__3_2_4 directory in runfiles" >&2
  exit 1
fi

echo "Found 3.0.9 at: $DIR_309"
echo "Found 3.2.4 at: $DIR_324"

# Verify that the package.json inside each directory contains the expected version.
PKG_JSON_309="${DIR_309}/package.json"
PKG_JSON_324="${DIR_324}/package.json"

if [[ ! -f "$PKG_JSON_309" ]]; then
  echo "ERROR: package.json not found at $PKG_JSON_309" >&2
  exit 1
fi

if [[ ! -f "$PKG_JSON_324" ]]; then
  echo "ERROR: package.json not found at $PKG_JSON_324" >&2
  exit 1
fi

VERSION_309=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['version'])" "$PKG_JSON_309")
VERSION_324=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['version'])" "$PKG_JSON_324")

if [[ "$VERSION_309" != "3.0.9" ]]; then
  echo "ERROR: expected version 3.0.9 in $PKG_JSON_309, got $VERSION_309" >&2
  exit 1
fi

if [[ "$VERSION_324" != "3.2.4" ]]; then
  echo "ERROR: expected version 3.2.4 in $PKG_JSON_324, got $VERSION_324" >&2
  exit 1
fi

echo "SUCCESS: multi-version npm packages generate correct versioned labels"
echo "  @npm//:vitest_pretty-format_3_0_9 → version $VERSION_309"
echo "  @npm//:vitest_pretty-format_3_2_4 → version $VERSION_324"
echo "  @npm//:vitest_pretty-format       → alias to highest (3.2.4)"
