#!/usr/bin/env bash
# check_tools_sources.sh -- the sources a consumer's tools binaries were built
# from are the tree's: `git diff --quiet tools-v<N> HEAD` over the tools'
# directories, with N from ts/private/tools_lock.bzl. docs/CI_CD.md § Tools.

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

lock=ts/private/tools_lock.bzl
version="$(sed -n 's/^TOOLS_VERSION = "\([0-9]*\)"$/\1/p' "$lock")"
tag="tools-v$version"
paths=(
  ts/tools tools/launcher tools/lcov_merger tools/copy_to_workspace
  tools/toolpack
)

if ! git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null; then
  git fetch --quiet --depth=1 origin "refs/tags/$tag:refs/tags/$tag" \
    2>/dev/null || {
    cat >&2 <<MESSAGE
check_tools_sources: no tag $tag. ts/private/tools_lock.bzl names it, so the
owner pushes it from the commit whose table that is (bazel run //tools/release
-- tools $version --push) before this tree merges.
MESSAGE
    exit 1
  }
fi

if ! git diff --quiet "$tag" HEAD -- "${paths[@]}"; then
  cat >&2 <<MESSAGE
check_tools_sources: the tools' sources differ from $tag:

$(git diff --stat "$tag" HEAD -- "${paths[@]}" | sed 's/^/  /')

A consumer downloads $tag's binaries; bump TOOLS_VERSION in
ts/private/tools_lock.bzl, fill the table from tools/ci/check_tools_lock.sh,
and the owner pushes the new tag from the PR head before merging.
MESSAGE
  exit 1
fi
printf 'check_tools_sources: %s is the tree at %s.\n' "$tag" "${paths[*]}"
