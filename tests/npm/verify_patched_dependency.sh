#!/usr/bin/env bash
# Verifies that a pnpm patch from patchedDependencies actually reaches the files
# Bazel hands to a build.
#
# The registry tarball for nanoid@3.3.11 is byte-identical whether or not the
# patch is applied -- `packages:` records the integrity of the unpatched publish
# -- so nothing but the file content can tell the two apart. The fixture patch
# (tests/npm/patches/nanoid@3.3.11.patch) rewrites "sideEffects": false into an
# array, mirroring the real patch in the consumer monorepo whose absence lets
# Rollup tree-shake a worker to nothing.
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

PKG_JSON="$(find -L . -path "*nanoid__3_3_11/package.json" -print -quit 2>/dev/null || true)"

if [[ -z "$PKG_JSON" ]]; then
  echo "ERROR: nanoid's package.json is not in runfiles" >&2
  find . -maxdepth 3 >&2
  exit 1
fi

SIDE_EFFECTS=$(python3 -c "import json,sys; print(json.dumps(json.load(open(sys.argv[1])).get('sideEffects')))" "$PKG_JSON")

if [[ "$SIDE_EFFECTS" != '["./index.js", "./index.cjs"]' ]]; then
  echo "ERROR: $PKG_JSON was not patched." >&2
  echo "  sideEffects is $SIDE_EFFECTS, expected [\"./index.js\", \"./index.cjs\"]" >&2
  echo "  The published nanoid@3.3.11 has \"sideEffects\": false; the patch in" >&2
  echo "  tests/npm/patches/nanoid@3.3.11.patch replaces it with the array." >&2
  exit 1
fi

echo "SUCCESS: patchedDependencies applied to $PKG_JSON"
