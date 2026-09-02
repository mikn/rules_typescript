#!/usr/bin/env bash
# check_integration_shards.sh -- assert the integration-test legs still partition
# the suite: every nested-Bazel test runs on exactly one of them.
#
# The legs are `--test_tag_filters` in //.bazelrc, one matrix entry apiece in
# .github/workflows/ci.yml, and one shard tag apiece in
# //tests/integration/tags.bzl. Three files, one invariant, and a green run
# proves nothing about it: a test no leg selects is not a failure anywhere -- it
# is a test that silently stopped running.
#
# The `core` leg is the complement of the other filters, so ADDING a test cannot
# drop it and needs no edit here. What this checks is the other direction:
# removing or renaming a leg while its `-shard-*` exclusion stays behind, a shard
# tag with no leg, or a hand-written tag that bypassed nested_bazel_tags().
#
# Read-only -- three loading-phase queries and some grep.
#
#   tools/ci/check_integration_shards.sh
#   BAZEL=bazelisk tools/ci/check_integration_shards.sh

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

bazel="${BAZEL:-bazel}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
  printf 'check_integration_shards: %s\n' "$1" >&2
  status=1
}
status=0

# ── The three declarations ────────────────────────────────────────────────────

# //.bazelrc: `test:ci-integration-<leg> --test_tag_filters=<terms>`. The leg
# whose terms are all negative is the complement; the rest name one tag each.
sed -n 's/^test:ci-integration-\([a-z0-9-]*\) --test_tag_filters=\(.*\)$/\1 \2/p' \
  .bazelrc > "$work/legs"

awk '$2 !~ /^-/ { print $1 }' "$work/legs" | LC_ALL=C sort > "$work/rc_shards"
awk '$2 ~ /^-/ { print $1 }' "$work/legs" | LC_ALL=C sort > "$work/rc_complement"

if [ "$(wc -l < "$work/rc_complement")" -ne 1 ]; then
  fail "//.bazelrc must define exactly one complement leg (all-negative
  --test_tag_filters); found $(wc -l < "$work/rc_complement"). Without it a test
  carrying no shard tag runs nowhere."
fi

# tests/integration/tags.bzl: the SHARDS list.
sed -n '/^SHARDS = \[/,/^\]/p' tests/integration/tags.bzl \
  | sed -n 's/^ *"\([a-z0-9-]*\)",$/\1/p' | LC_ALL=C sort > "$work/bzl_shards"

# .github/workflows/ci.yml: the matrix. One `shard:` list in the file, so a
# second job growing one is a parse this cannot silently read the wrong half of.
sed -n 's/^ *shard: \[\(.*\)\]$/\1/p' .github/workflows/ci.yml > "$work/yml_raw"
if [ "$(wc -l < "$work/yml_raw")" -ne 1 ]; then
  fail "expected exactly one 'shard: [...]' matrix in .github/workflows/ci.yml,
  found $(wc -l < "$work/yml_raw"). Teach this check which one is the
  integration lane's before adding another."
fi
tr ',' '\n' < "$work/yml_raw" | tr -d ' ' | awk 'NF' \
  | LC_ALL=C sort > "$work/yml_legs"

LC_ALL=C sort -u "$work/rc_shards" "$work/rc_complement" > "$work/rc_legs"

if ! LC_ALL=C diff -u "$work/bzl_shards" "$work/rc_shards" > "$work/d1"; then
  fail "SHARDS in tests/integration/tags.bzl and the positive
  test:ci-integration-* filters in //.bazelrc disagree (-tags.bzl +.bazelrc):
$(sed 's/^/  /' "$work/d1")"
fi

if ! LC_ALL=C diff -u "$work/rc_legs" "$work/yml_legs" > "$work/d2"; then
  fail "the //.bazelrc legs and the integration-tests matrix in
  .github/workflows/ci.yml disagree (-.bazelrc +ci.yml). A leg with no matrix
  entry runs nowhere:
$(sed 's/^/  /' "$work/d2")"
fi

# The complement leg must exclude every shard, or the shards it forgot run twice
# and the ones it invents run nowhere.
awk '$2 ~ /^-/ { print $2 }' "$work/legs" | tr ',' '\n' \
  | sed -n 's/^-shard-\([a-z0-9-]*\)$/\1/p' | LC_ALL=C sort > "$work/rc_excluded"

if ! LC_ALL=C diff -u "$work/rc_shards" "$work/rc_excluded" > "$work/d3"; then
  fail "the complement leg's --test_tag_filters does not exclude exactly the
  other legs' tags (-shards +excluded):
$(sed 's/^/  /' "$work/d3")"
fi

# ── What the legs actually select ─────────────────────────────────────────────

suite='tests(//tests/integration/...)'

query() {
  if ! "$bazel" query --noshow_progress "$1" > "$2" < /dev/null; then
    echo "check_integration_shards: bazel query failed; see the error above." >&2
    exit 1
  fi
  LC_ALL=C sort -u -o "$2" "$2"
}

query "$suite" "$work/all"

# `attr` matches the stringified attribute, so the tags arrive as `[a, b]` --
# hence the delimiter classes rather than an anchored `^shard-x$`.
: > "$work/selected"
while read -r shard; do
  query "attr(\"tags\", \"[\\[ ]shard-$shard[,\\]]\", $suite)" "$work/leg.$shard"
  if [ ! -s "$work/leg.$shard" ]; then
    fail "leg '$shard' selects no test at all. Either it is dead and should go,
  or its tag is misspelled somewhere."
  fi
  cat "$work/leg.$shard" >> "$work/selected"
done < "$work/rc_shards"

if [ -s "$work/selected" ]; then
  LC_ALL=C sort "$work/selected" | LC_ALL=C uniq -d > "$work/twice"
  if [ -s "$work/twice" ]; then
    fail "these tests carry more than one shard tag, so more than one leg runs
  them:
$(sed 's/^/  /' "$work/twice")"
  fi
fi

# Anything selected by no leg is what `core` runs. Nothing can escape both -- the
# complement is a complement -- so this is a report, not a check, and the check
# is that no tag outside the declared set reached a test.
LC_ALL=C sort -u "$work/selected" > "$work/selected_uniq"
LC_ALL=C comm -23 "$work/all" "$work/selected_uniq" > "$work/core"

"$bazel" query --noshow_progress --output=build "$suite" < /dev/null \
  | grep -oE '"shard-[a-z0-9-]+"' | tr -d '"' \
  | sed 's/^shard-//' | LC_ALL=C sort -u > "$work/tags_seen"

if ! LC_ALL=C comm -13 "$work/rc_shards" "$work/tags_seen" > "$work/undeclared"; then
  echo "check_integration_shards: comm failed." >&2
  exit 1
fi

if [ -s "$work/undeclared" ]; then
  fail "these shard tags are on a test but have no leg, so the tests carrying
  them run only because the complement leg still picks them up:
$(sed 's/^/  /' "$work/undeclared")"
fi

if [ "$status" -ne 0 ]; then
  exit "$status"
fi

# Every test under //tests/integration, not every nested-Bazel one: the harness's
# own unit test lives there and carries none of nested_bazel_tags(), so it lands
# on the complement leg like anything else unsharded.
printf 'check_integration_shards: %s tests in //tests/integration over %s legs, each on exactly one:\n' \
  "$(wc -l < "$work/all" | tr -d ' ')" "$(wc -l < "$work/rc_legs" | tr -d ' ')"
while read -r shard; do
  printf '  %-24s %s\n' "$shard" "$(wc -l < "$work/leg.$shard" | tr -d ' ')"
done < "$work/rc_shards"
printf '  %-24s %s\n' "$(cat "$work/rc_complement") (complement)" \
  "$(wc -l < "$work/core" | tr -d ' ')"
