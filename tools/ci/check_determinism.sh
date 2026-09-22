#!/usr/bin/env bash

set -euo pipefail

cd "${BUILD_WORKSPACE_DIRECTORY:-$(git rev-parse --show-toplevel)}"

bazel="${BAZEL:-bazel}"
if [ $# -ne 1 ]; then
  echo "usage: tools/ci/check_determinism.sh DIR" >&2
  exit 2
fi
dir="$1"
targets=(
  //tests/smoke:hello //ts/tools/tsaction //tools/launcher:ts_launcher
  //tools/lcov_merger //tools/copy_to_workspace
)
flags=(
  --config=determinism --platforms=//platforms:linux_amd64
  --disk_cache= --remote_cache= --remote_executor=
)

for base in a b; do
  if [ -L "$dir/$base" ]; then
    echo "check_determinism: $dir/$base must not be a symlink" >&2
    exit 1
  fi
  if [ -e "$dir/$base" ] &&
    { [ ! -d "$dir/$base" ] || [ -n "$(ls -A -- "$dir/$base")" ]; }; then
    echo "check_determinism: $dir/$base is not empty; the second build" \
      "would replay the first" >&2
    exit 1
  fi
done
shutdown() {
  for base in a b; do "$bazel" --output_base="$dir/$base" shutdown; done
}
trap shutdown EXIT

files() {
  "$bazel" --output_base="$1" cquery "${flags[@]}" --output=files \
    "config(set(${targets[*]}), target)" | LC_ALL=C sort
}

for base in a b; do
  "$bazel" --output_base="$dir/$base" build "${flags[@]}" "${targets[@]}"
done

files_a="$(files "$dir/a")"
files_b="$(files "$dir/b")"
if [ -z "$files_a" ]; then
  echo "check_determinism: no built files to compare" >&2
  exit 1
fi
if [ "$files_a" != "$files_b" ]; then
  echo "check_determinism: the two builds name different files (-a +b):" >&2
  diff <(echo "$files_a") <(echo "$files_b") >&2 || true
  exit 1
fi
root_a="$("$bazel" --output_base="$dir/a" info execution_root)"
root_b="$("$bazel" --output_base="$dir/b" info execution_root)"

status=0
while IFS= read -r f; do
  cmp -- "$root_a/$f" "$root_b/$f" || status=1
  sha256sum -- "$root_a/$f"
done <<<"$files_a"
if [ "$status" -ne 0 ]; then
  echo "check_determinism: the two builds differ; docs/CI_CD.md" \
    "§ Determinism Failures." >&2
  exit "$status"
fi
echo "check_determinism: $(echo "$files_a" | wc -l | tr -d ' ') files of" \
  "${targets[*]} are the same bytes from two empty output bases."
