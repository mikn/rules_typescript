#!/usr/bin/env bash
# test_existing_project.sh — Bootstrap test: existing project (no explicit return types).
#
# Simulates migrating an existing TypeScript codebase that does NOT yet have
# explicit return types on every exported function.  With the Gazelle directive
# `# gazelle:ts_isolated_declarations false` the generated ts_compile rules set
# isolated_declarations = False, allowing compilation to succeed without
# isolated-declaration-compatible annotations.
#
# No npm deps in this test — npm testing is covered by test_npm_deps.sh.
#
# Journey:
#   1. Create temp workspace with MODULE.bazel (local_path_override).
#   2. Write TS sources WITHOUT explicit return types.
#   3. Root BUILD.bazel carries `# gazelle:ts_isolated_declarations false`.
#   4. Run `bazel run //:gazelle`.
#   5. Run `bazel build //...` — should succeed.

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

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Create isolated temp workspace
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
cd "$TMP_DIR"

# Wrapper: run bazel with a dedicated --output_base.
bazel_run() {
    bazel --output_base="$OUTPUT_BASE" "$@"
}

# ---------------------------------------------------------------------------
# Write workspace files
# ---------------------------------------------------------------------------
cat > .bazelversion <<'EOF'
9.0.0
EOF

cat > WORKSPACE.bazel <<'EOF'
# Bzlmod only.
EOF

cat > .bazelrc <<'EOF'
# No @rules_rust flags — those are internal to rules_typescript.
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
test --test_output=errors
test --test_summary=terse
EOF

cat > MODULE.bazel <<EOF
"""Bootstrap test: existing project (no explicit return types, no npm deps)."""

module(
    name = "bootstrap_existing_project",
    version = "0.0.0",
    compatibility_level = 0,
)

bazel_dep(name = "rules_typescript", version = "0.0.0")
bazel_dep(name = "gazelle", version = "0.47.0")

local_path_override(
    module_name = "rules_typescript",
    path = "$RULES_TS_ROOT",
)

register_toolchains("@rules_typescript//ts/toolchain:all")
EOF

# Root BUILD.bazel — the key directive is ts_isolated_declarations false so
# that Gazelle emits isolated_declarations = False on every ts_compile rule.
cat > BUILD.bazel <<'EOF'
"""Root BUILD file for existing-project bootstrap test."""

load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_package_boundary every-dir
# gazelle:ts_isolated_declarations false

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
EOF

# ---------------------------------------------------------------------------
# Write TypeScript sources WITHOUT explicit return types
# ---------------------------------------------------------------------------
mkdir -p src/lib

# Functions intentionally lack explicit return-type annotations — this is
# the realistic "existing codebase" scenario.
cat > src/lib/utils.ts <<'EOF'
export function formatName(first: string, last: string) {
  return `${first} ${last}`;
}

export function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

export function range(start: number, end: number) {
  const result: number[] = [];
  for (let i = start; i < end; i++) {
    result.push(i);
  }
  return result;
}
EOF

# ---------------------------------------------------------------------------
# Step 3: run Gazelle
# ---------------------------------------------------------------------------
echo "INFO: running gazelle..."
if ! bazel_run run //:gazelle 2>&1; then
    fail "bazel run //:gazelle exited non-zero"
fi
pass "bazel run //:gazelle"

# Verify BUILD file was generated
if [[ ! -f "src/lib/BUILD.bazel" ]]; then
    fail "Gazelle did not generate src/lib/BUILD.bazel"
fi
pass "src/lib/BUILD.bazel generated"

# Verify the generated file uses isolated_declarations = False
if ! grep -q "isolated_declarations" src/lib/BUILD.bazel; then
    fail "src/lib/BUILD.bazel does not contain isolated_declarations attribute (expected False)"
fi
pass "src/lib/BUILD.bazel contains isolated_declarations attribute"

# ---------------------------------------------------------------------------
# Step 4: build — must succeed even without explicit return types
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero (isolated_declarations=false should allow missing return types)"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 5: verify output files
# ---------------------------------------------------------------------------
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"
for rel in "src/lib/utils.js" "src/lib/utils.d.ts"; do
    f="$BAZEL_BIN/$rel"
    if [[ ! -f "$f" ]]; then
        fail "expected output file not found: $f"
    fi
    pass "output file exists: $rel"
done

echo "ALL PASSED"
