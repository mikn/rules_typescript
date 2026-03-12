#!/usr/bin/env bash
# test_new_project.sh — Bootstrap test: create a fresh project, run Gazelle, build.
#
# Simulates the full new-user journey WITHOUT npm deps so the test works in
# isolation (no lockfile integrity issues, no network needed for packages):
#   1. Create a temp directory with a minimal MODULE.bazel (local_path_override
#      pointing at the rules_typescript checkout).
#   2. Write TypeScript sources with explicit return types (isolated declarations).
#   3. Run `bazel run //:gazelle` to generate BUILD files.
#   4. Run `bazel build //...` to compile and type-check.
#   5. Assert all commands exit 0 and expected output files exist.
#
# npm dependency testing is covered by test_npm_deps.sh.

set -euo pipefail

# ---------------------------------------------------------------------------
# Avoid TEST_TMPDIR interference with nested Bazel.
# When run under `bazel test`, $TEST_TMPDIR is set and the nested Bazel would
# inherit it as its own output base, causing conflicts.
# ---------------------------------------------------------------------------
unset TEST_TMPDIR

# ---------------------------------------------------------------------------
# Locate the rules_typescript checkout.
# Under `bazel test` with tags = ["local"] the env var BUILD_WORKSPACE_DIRECTORY
# is not set (that is only for `bazel run`).  We use TEST_SRCDIR instead.
# ---------------------------------------------------------------------------
RULES_TS_ROOT=""

# Prefer an explicit env override (useful for manual invocation).
if [[ -n "${RULES_TYPESCRIPT_ROOT:-}" ]]; then
    RULES_TS_ROOT="$RULES_TYPESCRIPT_ROOT"
fi

# Under `bazel test` with tags = ["local"], Bazel does not chdir into the
# sandbox for local tests, so the repo root is the working directory.
if [[ -z "$RULES_TS_ROOT" && -f "$(pwd)/MODULE.bazel" ]]; then
    candidate="$(pwd)"
    if grep -q '"rules_typescript"' "$candidate/MODULE.bazel" 2>/dev/null; then
        RULES_TS_ROOT="$candidate"
    fi
fi

# Fall back: walk up from the script location until we find MODULE.bazel with
# the right module name.
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
    # Bazel makes output files read-only; chmod before rm so the trap succeeds.
    chmod -R u+w "$OUTPUT_BASE" 2>/dev/null || true
    rm -rf "$TMP_DIR" "$OUTPUT_BASE"
}
trap cleanup EXIT

echo "INFO: workspace    = $TMP_DIR"
echo "INFO: output_base  = $OUTPUT_BASE"
cd "$TMP_DIR"

# Wrapper: run bazel with a dedicated --output_base so it does not collide with
# the outer Bazel process or any other nested Bazel in parallel.
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
# Bzlmod (MODULE.bazel) is used exclusively; this file prevents Bazel from
# walking up to a parent workspace.
EOF

cat > .bazelrc <<'EOF'
# Bootstrap test workspace — no @rules_rust flags (internal to rules_typescript)

# Correctness
build --incompatible_strict_action_env
build --nolegacy_external_runfiles

# Always type-check on build
build --output_groups=+_validation

# Test output
test --test_output=errors
test --test_summary=terse
EOF

# Use the absolute path so local_path_override works from any cwd.
cat > MODULE.bazel <<EOF
"""Bootstrap test: new project from scratch (no npm deps)."""

module(
    name = "bootstrap_new_project",
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

# Root BUILD.bazel with Gazelle target
cat > BUILD.bazel <<'EOF'
"""Root BUILD file."""

load("@gazelle//:def.bzl", "gazelle")

# gazelle:ts_package_boundary every-dir

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_ts",
)
EOF

# ---------------------------------------------------------------------------
# Write TypeScript sources (explicit return types — isolated declarations)
# ---------------------------------------------------------------------------
mkdir -p src/lib src/app

cat > src/lib/math.ts <<'EOF'
/**
 * Pure arithmetic helpers with explicit return types (isolated declarations).
 */

export function add(a: number, b: number): number {
  return a + b;
}

export function multiply(a: number, b: number): number {
  return a * b;
}

export function subtract(a: number, b: number): number {
  return a - b;
}

export function divide(a: number, b: number): number {
  if (b === 0) {
    throw new RangeError("Division by zero");
  }
  return a / b;
}
EOF

cat > src/lib/index.ts <<'EOF'
export { add, divide, multiply, subtract } from "./math";
EOF

cat > src/app/index.ts <<'EOF'
import { add, multiply } from "../lib";

export function main(): string {
  const sum: number = add(1, 2);
  const product: number = multiply(3, 4);
  return `Result: ${sum} and ${product}`;
}
EOF

# ---------------------------------------------------------------------------
# Step 3: run Gazelle to generate BUILD files
# ---------------------------------------------------------------------------
echo "INFO: running gazelle..."
if ! bazel_run run //:gazelle 2>&1; then
    fail "bazel run //:gazelle exited non-zero"
fi
pass "bazel run //:gazelle"

# Verify Gazelle generated BUILD files in the src directories
for dir in src/lib src/app; do
    if [[ ! -f "$dir/BUILD.bazel" ]]; then
        fail "Gazelle did not generate $dir/BUILD.bazel"
    fi
    pass "$dir/BUILD.bazel generated"
done

# ---------------------------------------------------------------------------
# Step 4: build (compile + type-check)
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 5: verify output files exist in bazel-bin
# ---------------------------------------------------------------------------
# Gazelle names the ts_compile target after the directory basename.
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"

for rel in "src/lib/math.js" "src/lib/math.d.ts" "src/app/index.js" "src/app/index.d.ts"; do
    f="$BAZEL_BIN/$rel"
    if [[ ! -f "$f" ]]; then
        fail "expected output file not found: $f"
    fi
    pass "output file exists: $rel"
done

echo "ALL PASSED"
