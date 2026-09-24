#!/usr/bin/env bash
set -euo pipefail
root="${TEST_SRCDIR}/${TEST_WORKSPACE}"
exec "${root}/ts/toolchain/node_resolved/node" \
  "${root}/tests/workers_nested/test/runtime_entries_test.mjs" \
  "${root}/ts/private/wrangler_test_config.mjs" \
  "${root}/tests/workers/node_modules"
