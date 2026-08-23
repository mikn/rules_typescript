#!/usr/bin/env bash
# Verifies that a tarball whose top-level directory is not predictable from the
# package name is still extracted correctly.
#
# @types/express-serve-static-core@4.19.6 packs its files under
# "express-serve-static-core v4.19/", so npm_translate_lock must read the prefix
# out of the archive; predicting "express-serve-static-core" fails the fetch.
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

PKG_DIR=""
for candidate in $(find . -maxdepth 3 -name "types_express-serve-static-core__4_19_6" -type d 2>/dev/null); do
  PKG_DIR="$candidate"
  break
done

if [[ -z "$PKG_DIR" ]]; then
  echo "ERROR: could not find types_express-serve-static-core__4_19_6 in runfiles" >&2
  ls -la . >&2
  exit 1
fi

if [[ ! -f "${PKG_DIR}/index.d.ts" || ! -f "${PKG_DIR}/package.json" ]]; then
  echo "ERROR: ${PKG_DIR} does not hold the package contents at its root:" >&2
  find "${PKG_DIR}" -maxdepth 2 >&2
  exit 1
fi

if [[ -d "${PKG_DIR}/express-serve-static-core v4.19" ]]; then
  echo "ERROR: tarball prefix was not stripped from ${PKG_DIR}" >&2
  exit 1
fi

echo "SUCCESS: 'express-serve-static-core v4.19/' prefix detected and stripped"
