#!/usr/bin/env bash
# Starlark can load a name or fail to, but cannot enumerate a module's exports,
# so ts/defs.bzl is read and its top-level assignments and defs compared.

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
if [[ -d "${RUNFILES}/_main" ]]; then
  RUNFILES="${RUNFILES}/_main"
fi

DEFS="${RUNFILES}/ts/defs.bzl"
[[ -f "${DEFS}" ]] || fail "missing runfile: ${DEFS}"

want="${TEST_TMPDIR}/want"
got="${TEST_TMPDIR}/got"

cat > "${want}" <<'NAMES'
AssetInfo
BundlerInfo
CssInfo
CssModuleInfo
JsInfo
TsDeclarationInfo
TsLintInfo
TsModuleInfo
asset_library
css_library
css_module
json_library
refresh_workspace_files
ts_add_package
ts_binary
ts_codegen
ts_compile
ts_config
ts_dev_server
ts_lint
ts_pnpm
ts_refresh_tsconfig
ts_test
NAMES

{
  sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\) = .*/\1/p' "${DEFS}"
  sed -n 's/^def \([A-Za-z_][A-Za-z0-9_]*\)(.*/\1/p' "${DEFS}"
} | LC_ALL=C sort > "${got}"

if ! LC_ALL=C diff -u "${want}" "${got}" > "${TEST_TMPDIR}/diff"; then
  echo "ts/defs.bzl exports differ from the pinned surface (-want +got):" >&2
  cat "${TEST_TMPDIR}/diff" >&2
  exit 1
fi
echo "ts/defs.bzl exports exactly $(wc -l < "${want}" | tr -d ' ') names"
