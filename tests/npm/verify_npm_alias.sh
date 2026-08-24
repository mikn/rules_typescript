#!/usr/bin/env bash
# Verifies that an npm alias specifier becomes a real directory in node_modules.
#
# `nano-alias: npm:nanoid@3.3.11` means `import "nano-alias"` must find
# node_modules/nano-alias. That only exists if the alias gets its own target with
# package_name set to the alias: package_name is what the tree builder writes on
# disk, so a Bazel alias pointing at nanoid's own target writes node_modules/nanoid.
#
# The two routes an alias arrives by are checked separately, each with a fixture
# entry that only the one route reaches:
#   ms-alias    declared by the workspace root and by nothing else, so the label
#               exists only if importers are read for aliases as well as links
#   nano-alias  declared by zod and by nothing else, so it exists only if the
#               dependency edge carries the name zod imports nanoid under
set -euo pipefail

if [[ -n "${RUNFILES_DIR:-}" ]]; then
  : # already set by Bazel
elif [[ -n "${TEST_SRCDIR:-}" ]]; then
  RUNFILES_DIR="$TEST_SRCDIR"
else
  echo "ERROR: RUNFILES_DIR not set" >&2
  exit 1
fi

cd "${RUNFILES_DIR}"

check_tree() {
  local tree_name="$1"
  local alias_name="$2"
  local via="$3"
  local tree
  tree="$(find -L . -type d -name "$tree_name" -print -quit 2>/dev/null || true)"
  if [[ -z "$tree" ]]; then
    echo "ERROR: node_modules tree '$tree_name' is not in runfiles" >&2
    find . -maxdepth 3 -type d >&2
    exit 1
  fi
  if [[ ! -f "${tree}/${alias_name}/package.json" ]]; then
    echo "ERROR: ${tree} has no ${alias_name}/ directory (alias reached via ${via})" >&2
    ls -la "$tree" >&2
    exit 1
  fi
  echo "  ${tree}/${alias_name}/package.json  (via ${via})"
}

echo "Checking that the npm alias is installed under its alias name..."
check_tree "importer_alias_node_modules" "ms-alias" "the root importer"
check_tree "package_alias_node_modules" "nano-alias" "zod's dependency edge"

echo "SUCCESS: npm alias specifiers install as node_modules/<alias>"
