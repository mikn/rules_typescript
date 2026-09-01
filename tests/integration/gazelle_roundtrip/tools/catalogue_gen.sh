#!/usr/bin/env bash
# catalogue_gen.sh <space-separated srcs> <out.ts>
#
# Emits the basename of every src it was given, so the generated file shows
# which inputs the action actually received.

set -euo pipefail

OUT="$2"
{
    echo "// GENERATED FILE -- do not edit manually."
    echo "export const catalogues: readonly string[] = ["
    for src in $1; do
        printf '  "%s",\n' "$(basename "${src}")"
    done
    echo "];"
} > "${OUT}"
