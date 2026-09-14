#!/usr/bin/env bash
# check_tools_lock.sh [--out DIR] -- the four tools release assets, built from
# this tree with the release job's command line, against the table in
# ts/private/tools_lock.bzl.
#
# For each platform of the release: `bazel build --platforms=//platforms:<key>`
# over the four go_binary targets, one asset packed by //tools/toolpack/cmd,
# its SRI printed. A platform whose SRI differs from TOOLS_INTEGRITY fails the
# check; the printed lines are what the PR bumping TOOLS_VERSION pastes into the
# table. With --out the assets are left in DIR for release.yml to attach.
#
#   tools/ci/check_tools_lock.sh
#   BAZEL=bazelisk tools/ci/check_tools_lock.sh --out dist

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

bazel="${BAZEL:-bazel}"
out=""
if [ "${1:-}" = "--out" ]; then
  out="$2"
  shift 2
fi
if [ -z "$out" ]; then
  out="$(mktemp -d)"
  trap 'rm -rf "$out"' EXIT
fi
mkdir -p "$out"

lock=ts/private/tools_lock.bzl
version="$(sed -n 's/^TOOLS_VERSION = "\([0-9]*\)"$/\1/p' "$lock")"
if [ -z "$version" ]; then
  echo "check_tools_lock: no TOOLS_VERSION in $lock" >&2
  exit 1
fi
platforms="$(sed -n 's/^TSGO_PLATFORMS = \[\(.*\)\]$/\1/p' \
  ts/private/toolchain.bzl | tr -d '" ' | tr ',' ' ')"
targets="//ts/tools/tsaction //tools/launcher:ts_launcher //tools/lcov_merger"
targets="$targets //tools/copy_to_workspace"

"$bazel" build //tools/toolpack/cmd >/dev/null
pack="$("$bazel" cquery --output=files "config(//tools/toolpack/cmd, target)" \
  2>/dev/null)"
root="$("$bazel" info execution_root 2>/dev/null)"

status=0
echo "TOOLS_INTEGRITY = {"
for platform in $platforms; do
  "$bazel" build --platforms=//platforms:"$platform" $targets >/dev/null
  files="$("$bazel" cquery --platforms=//platforms:"$platform" --output=files \
    "config(set($targets), target)" 2>/dev/null)"
  args=()
  for f in $files; do
    args+=("$(basename "$f")=$root/$f")
  done
  asset="$out/rules_typescript-tools-$version-$platform.tar.gz"
  sri="$("$root/$pack" -version "$version" -platform "$platform" \
    -out "$asset" "${args[@]}")"
  echo "    \"$platform\": \"$sri\","
  want="$(sed -n "s/^    \"$platform\": \"\([^\"]*\)\",$/\1/p" "$lock")"
  if [ "$want" != "$sri" ]; then
    echo "check_tools_lock: $lock has $platform at \"$want\"," \
      "the tree builds $sri" >&2
    status=1
  fi
done
echo "}"

if [ "$status" -ne 0 ]; then
  cat >&2 <<MESSAGE
check_tools_lock: the table in $lock is not this tree's build. A change to
the tools bumps TOOLS_VERSION and pastes the table printed above; the owner
pushes tools-v$version from the PR head before merging.
MESSAGE
  exit "$status"
fi
echo "check_tools_lock: the four assets of tools-v$version match $lock ($out)."
