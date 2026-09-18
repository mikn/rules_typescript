#!/usr/bin/env bash

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

tools/vendor_crates.sh >/dev/null

changed="$(git status --porcelain -- oxc_cli/crates)"
if [ -n "$changed" ]; then
  cat >&2 <<MESSAGE
check_vendored_crates: oxc_cli/crates is not the rendering of oxc_cli/Cargo.toml
and Cargo.lock:

$(printf '%s\n' "$changed" | sed 's/^/  /')

Commit what tools/vendor_crates.sh rendered, then \`bazel mod tidy\`
(CONTRIBUTING.md § Vendoring the crates).
MESSAGE
  exit 1
fi
echo "check_vendored_crates: oxc_cli/crates is Cargo.lock's rendering."
