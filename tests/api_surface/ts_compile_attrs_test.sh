#!/usr/bin/env bash
# A rule cannot enumerate its own attributes from Starlark, so the `attrs` dict
# of the ts_compile declaration is read out of the source and its public keys
# compared against the three the rule has.

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

RUNFILES="${TEST_SRCDIR:-}"
[[ -n "${RUNFILES}" ]] || fail "TEST_SRCDIR is unset; run this through bazel test"
if [[ -d "${RUNFILES}/_main" ]]; then
  RUNFILES="${RUNFILES}/_main"
fi

RULE="${RUNFILES}/ts/private/ts_compile.bzl"
[[ -f "${RULE}" ]] || fail "missing runfile: ${RULE}"

want="${TEST_TMPDIR}/want"
got="${TEST_TMPDIR}/got"

printf '%s\n' deps srcs tsconfig > "${want}"

# The attrs dict runs from `ts_compile = rule(` to the `toolchains` key; a
# public attribute is an 8-space-indented quoted key at that depth.
sed -n '/^ts_compile = rule($/,/^    toolchains = \[$/p' "${RULE}" \
  | sed -n 's/^        "\([a-z][a-z_]*\)": attr\..*/\1/p' \
  | LC_ALL=C sort > "${got}"

if ! LC_ALL=C diff -u "${want}" "${got}" > "${TEST_TMPDIR}/diff"; then
  echo "ts_compile's public attributes differ from the pinned three (-want +got):" >&2
  cat "${TEST_TMPDIR}/diff" >&2
  exit 1
fi
echo "ts_compile has exactly $(wc -l < "${want}" | tr -d ' ') public attributes"
