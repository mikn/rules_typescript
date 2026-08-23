#!/usr/bin/env bash
# Verifies that @npm//:nested-shared resolves to //packages/nested-shared.
#
# The alias comes from an importer-relative link: entry in pnpm-lock.yaml
# (importer tests/npm/nested → link:../../../packages/nested-shared), so its
# files can only land in runfiles when the link path is resolved against the
# importer rather than treated as root-relative.
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

DECL="$(find -L . -path "*/packages/nested-shared/index.d.ts" -print -quit 2>/dev/null || true)"

if [[ -z "$DECL" ]]; then
  echo "ERROR: no declarations from //packages/nested-shared in runfiles" >&2
  find . -maxdepth 4 >&2
  exit 1
fi

echo "SUCCESS: @npm//:nested-shared → //packages/nested-shared:nested-shared"
