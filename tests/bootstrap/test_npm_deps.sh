#!/usr/bin/env bash
# test_npm_deps.sh — Bootstrap test: npm dependencies resolve correctly.
#
# Verifies the end-to-end npm dependency flow:
#   1. Create a workspace that copies tests/npm/pnpm-lock.yaml from the
#      rules_typescript checkout (which includes zod 3.24.2 and vitest 3.0.9).
#   2. Write TypeScript that imports from zod with explicit return types.
#   3. Run Gazelle to generate BUILD files.
#   4. Build — npm deps must resolve, zod types must compile.
#   5. Run tests that use zod at runtime via vitest.

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

NPM_LOCKFILE="$RULES_TS_ROOT/tests/npm/pnpm-lock.yaml"
if [[ ! -f "$NPM_LOCKFILE" ]]; then
    echo "FAIL: pnpm-lock.yaml not found at $NPM_LOCKFILE" >&2
    exit 1
fi

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
# Copy the lockfile from tests/npm/ — this includes zod 3.24.2 + vitest 3.0.9
# ---------------------------------------------------------------------------
cp "$NPM_LOCKFILE" pnpm-lock.yaml

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
"""Bootstrap test: npm dependency resolution."""

module(
    name = "bootstrap_npm_deps",
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

npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
EOF

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
# Write TypeScript that uses zod (isolated-declarations-compatible)
# ---------------------------------------------------------------------------
mkdir -p src/models

cat > src/models/user.ts <<'EOF'
import { z } from "zod";

// Explicit interface — no inference from schema, fully isolated-declarations
// compatible (exported type has no dependency on unexported const).
export interface User {
  id: number;
  name: string;
  email: string;
}

// Internal schema — NOT exported, so isolated-declarations does not require
// an explicit type annotation on this const.
const userSchema = z.object({
  id: z.number(),
  name: z.string(),
  email: z.string().email(),
});

export function parseUser(input: unknown): User {
  return userSchema.parse(input) as User;
}

export function isValidEmail(email: string): boolean {
  return z.string().email().safeParse(email).success;
}
EOF

cat > src/models/user.test.ts <<'EOF'
import { describe, expect, it } from "vitest";
import { isValidEmail, parseUser } from "./user";

describe("parseUser", () => {
  it("parses a valid user object", () => {
    const result = parseUser({ id: 1, name: "Alice", email: "alice@example.com" });
    expect(result.id).toBe(1);
    expect(result.name).toBe("Alice");
  });

  it("throws on invalid input", () => {
    expect(() => parseUser({ id: "not-a-number", name: "Bob", email: "bad" })).toThrow();
  });
});

describe("isValidEmail", () => {
  it("accepts a valid email", () => {
    expect(isValidEmail("alice@example.com")).toBe(true);
  });

  it("rejects an invalid email", () => {
    expect(isValidEmail("not-an-email")).toBe(false);
  });
});
EOF

# ---------------------------------------------------------------------------
# Step 3: run Gazelle
# ---------------------------------------------------------------------------
echo "INFO: running gazelle..."
if ! bazel_run run //:gazelle 2>&1; then
    fail "bazel run //:gazelle exited non-zero"
fi
pass "bazel run //:gazelle"

if [[ ! -f "src/models/BUILD.bazel" ]]; then
    fail "Gazelle did not generate src/models/BUILD.bazel"
fi
pass "src/models/BUILD.bazel generated"

# Verify Gazelle wired the zod dep into the BUILD file
if ! grep -q "@npm//:zod" src/models/BUILD.bazel; then
    fail "src/models/BUILD.bazel does not reference @npm//:zod"
fi
pass "src/models/BUILD.bazel references @npm//:zod"

# ---------------------------------------------------------------------------
# Step 4: build — npm deps must resolve and zod types must compile
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero (npm deps should resolve)"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 5: verify output files
# ---------------------------------------------------------------------------
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"
for rel in "src/models/user.js" "src/models/user.d.ts"; do
    f="$BAZEL_BIN/$rel"
    if [[ ! -f "$f" ]]; then
        fail "expected output file not found: $f"
    fi
    pass "output file exists: $rel"
done

# ---------------------------------------------------------------------------
# Step 6: tests pass (zod works at runtime)
# ---------------------------------------------------------------------------
echo "INFO: running bazel test //..."
if ! bazel_run test //... 2>&1; then
    fail "bazel test //... exited non-zero"
fi
pass "bazel test //..."

echo "ALL PASSED"
