#!/usr/bin/env bash
set -euo pipefail
binary="$TEST_SRCDIR/$1"
nm -D --defined-only "$binary" | awk '{print $NF}' > "$TEST_TMPDIR/exports"
for symbol in napi_get_version napi_create_object uv_async_send; do
    grep -Fx "$symbol" "$TEST_TMPDIR/exports"
done
"$binary" --version
