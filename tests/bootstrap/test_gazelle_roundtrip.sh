#!/usr/bin/env bash
# test_gazelle_roundtrip.sh — Bootstrap test: Gazelle regenerates BUILD files
#                              identically for e2e/basic.
#
# Journey:
#   1. Create a copy of e2e/basic in a temp directory.
#   2. Delete all src/*/BUILD.bazel files.
#   3. Run `bazel run //:gazelle` to regenerate them.
#   4. Build everything — should succeed.
#   5. Run tests — should pass.
#   6. Diff generated vs original BUILD files — should be identical (or only
#      differ in whitespace / comment ordering that Gazelle normalises).

set -euo pipefail

# ---------------------------------------------------------------------------
# Avoid TEST_TMPDIR interference with nested Bazel.
# ---------------------------------------------------------------------------
unset TEST_TMPDIR

# ---------------------------------------------------------------------------
# Locate the rules_typescript checkout
# ---------------------------------------------------------------------------
RULES_TS_ROOT=""

if [[ -n "${RULES_TYPESCRIPT_ROOT:-}" ]]; then
    RULES_TS_ROOT="$RULES_TYPESCRIPT_ROOT"
fi

if [[ -z "$RULES_TS_ROOT" && -f "$(pwd)/MODULE.bazel" ]]; then
    candidate="$(pwd)"
    if grep -q '"rules_typescript"' "$candidate/MODULE.bazel" 2>/dev/null; then
        RULES_TS_ROOT="$candidate"
    fi
fi

if [[ -z "$RULES_TS_ROOT" ]]; then
    dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    while [[ "$dir" != "/" ]]; do
        if [[ -f "$dir/MODULE.bazel" ]] && grep -q '"rules_typescript"' "$dir/MODULE.bazel" 2>/dev/null; then
            RULES_TS_ROOT="$dir"
            break
        fi
        dir="$(dirname "$dir")"
    done
fi

if [[ -z "$RULES_TS_ROOT" ]]; then
    echo "FAIL: cannot locate rules_typescript checkout" >&2
    echo "      Set RULES_TYPESCRIPT_ROOT or run from within the repo." >&2
    exit 1
fi

echo "INFO: rules_typescript root = $RULES_TS_ROOT"

E2E_BASIC="$RULES_TS_ROOT/e2e/basic"
if [[ ! -d "$E2E_BASIC" ]]; then
    echo "FAIL: e2e/basic not found at $E2E_BASIC" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Create isolated copy of e2e/basic
# ---------------------------------------------------------------------------
TMP_DIR="$(mktemp -d)"
OUTPUT_BASE="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$OUTPUT_BASE" 2>/dev/null || true
    rm -rf "$TMP_DIR" "$OUTPUT_BASE"
}
trap cleanup EXIT

echo "INFO: workspace    = $TMP_DIR"
echo "INFO: output_base  = $OUTPUT_BASE"

# Copy only the non-generated, non-symlink workspace contents.
# We explicitly exclude bazel-* symlinks, .git, and generated lock files.
rsync -a \
    --exclude='bazel-*' \
    --exclude='.git' \
    --exclude='MODULE.bazel.lock' \
    "$E2E_BASIC/" "$TMP_DIR/"

cd "$TMP_DIR"

# Wrapper: run bazel with a dedicated --output_base.
bazel_run() {
    bazel --output_base="$OUTPUT_BASE" "$@"
}

# ---------------------------------------------------------------------------
# Rewrite MODULE.bazel to use an absolute local_path_override
# (the copied MODULE.bazel uses a relative "../.." path which would no longer
# be correct in the temp directory).
# ---------------------------------------------------------------------------
cat > MODULE.bazel <<EOF
"""Gazelle roundtrip test workspace (copy of e2e/basic)."""

module(
    name = "e2e_basic",
    version = "0.0.0",
)

bazel_dep(name = "rules_typescript", version = "0.0.0")

register_toolchains("@rules_typescript//ts/toolchain:all")

local_path_override(
    module_name = "rules_typescript",
    path = "$RULES_TS_ROOT",
)

bazel_dep(name = "gazelle", version = "0.47.0")

npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
use_repo(npm, "npm")
EOF

# ---------------------------------------------------------------------------
# Save a snapshot of the original BUILD files before deletion
# ---------------------------------------------------------------------------
declare -a ORIGINAL_BUILD_FILES=()
while IFS= read -r f; do
    ORIGINAL_BUILD_FILES+=("$f")
done < <(find src -name "BUILD.bazel" | sort)

if [[ ${#ORIGINAL_BUILD_FILES[@]} -eq 0 ]]; then
    fail "no src/*/BUILD.bazel files found in e2e/basic copy — check rsync"
fi

echo "INFO: found ${#ORIGINAL_BUILD_FILES[@]} BUILD files to snapshot:"
for f in "${ORIGINAL_BUILD_FILES[@]}"; do
    echo "  $f"
done

# Save originals
declare -A ORIGINAL_CONTENTS
for f in "${ORIGINAL_BUILD_FILES[@]}"; do
    ORIGINAL_CONTENTS["$f"]="$(cat "$f")"
done

# ---------------------------------------------------------------------------
# Step 2: delete all src/*/BUILD.bazel files
# ---------------------------------------------------------------------------
for f in "${ORIGINAL_BUILD_FILES[@]}"; do
    rm -f "$f"
    pass "deleted $f"
done

# ---------------------------------------------------------------------------
# Step 3: run Gazelle to regenerate
# ---------------------------------------------------------------------------
echo "INFO: running gazelle..."
if ! bazel_run run //:gazelle 2>&1; then
    fail "bazel run //:gazelle exited non-zero"
fi
pass "bazel run //:gazelle"

# Verify all BUILD files were regenerated
for f in "${ORIGINAL_BUILD_FILES[@]}"; do
    if [[ ! -f "$f" ]]; then
        fail "Gazelle did not regenerate $f"
    fi
    pass "regenerated: $f"
done

# ---------------------------------------------------------------------------
# Step 4: build
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero after Gazelle regeneration"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 5: tests pass
# ---------------------------------------------------------------------------
echo "INFO: running bazel test //..."
if ! bazel_run test //... 2>&1; then
    fail "bazel test //... exited non-zero after Gazelle regeneration"
fi
pass "bazel test //..."

# ---------------------------------------------------------------------------
# Step 6: diff generated vs original
# ---------------------------------------------------------------------------
DIFF_FAILURES=0
for f in "${ORIGINAL_BUILD_FILES[@]}"; do
    original="${ORIGINAL_CONTENTS[$f]}"
    generated="$(cat "$f")"

    # Normalise: strip trailing whitespace, collapse multiple blank lines.
    # Gazelle may reorder or reformat slightly; we focus on semantic equality.
    norm_orig="$(printf '%s' "$original" | sed 's/[[:space:]]*$//' | cat -s)"
    norm_gen="$(printf '%s' "$generated" | sed 's/[[:space:]]*$//' | cat -s)"

    if [[ "$norm_orig" != "$norm_gen" ]]; then
        echo "DIFF: $f differs after roundtrip:"
        diff <(printf '%s\n' "$norm_orig") <(printf '%s\n' "$norm_gen") || true
        DIFF_FAILURES=$((DIFF_FAILURES + 1))
    else
        pass "roundtrip identical: $f"
    fi
done

if [[ $DIFF_FAILURES -gt 0 ]]; then
    fail "$DIFF_FAILURES BUILD file(s) differ after Gazelle roundtrip"
fi

echo "ALL PASSED"
