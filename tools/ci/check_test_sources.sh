#!/usr/bin/env bash
# check_test_sources.sh -- assert that every test source file in this workspace
# is named in the srcs of some test target.
#
# `bazel build //...`, `bazel test //...` and a byte-identical Gazelle rerun are
# all satisfied by a Gazelle run that DELETES a test target, which is how seven
# hand-written go_test targets once went missing. This check is not: the set of
# test files on disk is not something Gazelle writes, so it cannot be brought
# back into agreement by damaging the BUILD files.
#
# Read-only -- a loading-phase query and `git ls-files`. It never runs Gazelle
# and never writes to the source tree.
#
# Two limits it does not cover. `tests(//...)` counts manual-tagged targets, so
# a file claimed only by one (tests/vitest/environment:{edge,jsdom}_test) passes
# here while nothing executes it. And `git ls-files` cannot see an unstaged new
# file, so a local run reports green on a test that has no target yet.
#
#   tools/ci/check_test_sources.sh
#   BAZEL=bazelisk tools/ci/check_test_sources.sh

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

bazel="${BAZEL:-bazel}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# A directory holding its own MODULE.bazel/WORKSPACE.bazel is a separate
# workspace that `//...` does not descend into, so its test files are not this
# workspace's to claim; .bazelignore roots are invisible to `//...` for the same
# practical reason.
{
  git ls-files '*/MODULE.bazel' '*/WORKSPACE.bazel' | sed 's|/[^/]*$||'
  if [ -f .bazelignore ]; then
    sed -e 's/#.*//' -e 's:/*[[:space:]]*$::' .bazelignore
  fi
} | awk 'NF' | LC_ALL=C sort -u > "$work/out_of_scope"

git ls-files \
  '*_test.go' '*.test.ts' '*.test.tsx' '*.test.mts' \
  '*.spec.ts' '*.spec.tsx' '*.spec.mts' \
  | awk -v roots="$(cat "$work/out_of_scope")" '
      BEGIN { n = split(roots, root, "\n") }
      {
        for (i = 1; i <= n; i++)
          if (index($0, root[i] "/") == 1) next
        print
      }' \
  | while IFS= read -r file; do
      if [ -f "$file" ]; then printf '%s\n' "$file"; fi
    done \
  | LC_ALL=C sort -u > "$work/on_disk"

# `bazel query` prints partial results and exits 7 on a package-load error, so
# the status is checked before anything reads the output.
if ! "$bazel" query --noshow_progress \
  'filter("(_test\.go|\.(test|spec)\.(ts|tsx|mts))$", labels(srcs, tests(//...)))' \
  > "$work/labels"; then
  echo "check_test_sources: bazel query failed; see the error above." >&2
  exit 1
fi

sed -n 's|^//||p' "$work/labels" \
  | sed -e 's|:|/|' -e 's|^/||' \
  | LC_ALL=C sort -u > "$work/claimed"

if ! LC_ALL=C comm -23 "$work/on_disk" "$work/claimed" > "$work/orphans"; then
  echo "check_test_sources: comm failed." >&2
  exit 1
fi

if [ -s "$work/orphans" ]; then
  cat >&2 <<MESSAGE
check_test_sources: these test source files are not in the srcs of any test
target, so nothing runs them:

$(sed 's/^/  /' "$work/orphans")

Either add them to a test target, or delete them. A file losing its target is
usually a Gazelle run that dropped a hand-written rule -- check the BUILD file
in its package against git history before regenerating.
MESSAGE
  exit 1
fi

printf 'check_test_sources: %s test source files, all claimed by a test target.\n' \
  "$(wc -l < "$work/on_disk" | tr -d ' ')"
