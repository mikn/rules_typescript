#!/usr/bin/env bash
# test_dev_server.sh — Bootstrap test: ts_dev_server builds successfully.
#
# Verifies that a ts_dev_server target builds without errors. We cannot test
# that the server actually serves requests (it would block forever), but we
# can verify:
#   1. The build succeeds (rule analysis + action execution).
#   2. The generated vite.config.mjs exists in bazel-bin.
#   3. The generated runner script exists and contains expected content.
#   4. The vite.config.mjs contains expected server configuration (port, etc.).
#
# Note: ts_dev_server requires npm deps (vite) for node_modules wiring.
# We use the same pnpm-lock.yaml as test_npm_deps to get vite 6.4.1.
#
# No Gazelle is used in this test — everything is hand-written. This avoids
# the Gazelle auto-generated ts_dev_server conflict that arises when a file
# named main.ts triggers both gazelle-generated and hand-written dev targets.

set -euo pipefail

# ---------------------------------------------------------------------------
# Avoid TEST_TMPDIR interference with nested Bazel.
# ---------------------------------------------------------------------------
unset TEST_TMPDIR

# ---------------------------------------------------------------------------
# Locate the rules_typescript checkout.
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

bazel_run() {
    bazel --output_base="$OUTPUT_BASE" "$@"
}

# ---------------------------------------------------------------------------
# Copy the lockfile from tests/npm/ — includes vite 6.4.1
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
# Bootstrap test workspace — no @rules_rust flags (internal to rules_typescript)
build --incompatible_strict_action_env
build --nolegacy_external_runfiles
build --output_groups=+_validation
test --test_output=errors
test --test_summary=terse
EOF

cat > MODULE.bazel <<EOF
"""Bootstrap test: ts_dev_server builds successfully."""

module(
    name = "bootstrap_dev_server",
    version = "0.0.0",
    compatibility_level = 0,
)

bazel_dep(name = "rules_typescript", version = "0.0.0")

local_path_override(
    module_name = "rules_typescript",
    path = "$RULES_TS_ROOT",
)

register_toolchains("@rules_typescript//ts/toolchain:all")

npm = use_extension("@rules_typescript//npm:extensions.bzl", "npm")
npm.translate_lock(pnpm_lock = "//:pnpm-lock.yaml")
use_repo(npm, "npm")
EOF

# Root BUILD.bazel: hand-written ts_compile + node_modules + ts_dev_server.
# No Gazelle is needed — we write the BUILD targets directly to avoid the
# conflict where main.ts would trigger Gazelle to also generate a ts_dev_server
# that references a non-existent :node_modules in the sub-package.
# The TypeScript source is placed directly at the root package level to avoid
# cross-package path issues with oxc output paths.
cat > BUILD.bazel <<'EOF'
"""Root BUILD file — ts_dev_server bootstrap test."""

load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_dev_server")

# Compile the application TypeScript source.
# Source lives at the root package level so oxc outputs correctly to root.
ts_compile(
    name = "app",
    srcs = ["app.ts"],
)

# node_modules tree for the dev server — must include vite.
node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

# Dev server target. `bazel build //:dev` generates the runner script and
# vite.config.mjs. `bazel run //:dev` would start the server (not done here).
ts_dev_server(
    name = "dev",
    entry_point = ":app",
    node_modules = ":node_modules",
    port = 5173,
)
EOF

# ---------------------------------------------------------------------------
# Write TypeScript source at the root package level.
# Using a non-app-entry-point name (not main.ts, app.ts, etc.) to avoid
# any future Gazelle invocation generating a ts_dev_server that conflicts
# with the hand-written one above.
# ---------------------------------------------------------------------------
cat > app.ts <<'EOF'
/**
 * Application module for the dev server test.
 * Uses explicit return types for isolated declarations compatibility.
 */
export function greet(name: string): string {
  return `Hello, ${name}!`;
}

export function computeMessage(items: string[]): string {
  return items.map((item: string) => greet(item)).join(", ");
}
EOF

# ---------------------------------------------------------------------------
# Step 1: build the dev server target.
# `bazel build //:dev` runs the rule's analysis + action (generates the config
# and runner script) but does NOT start the server.
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //:dev..."
if ! bazel_run build //:dev 2>&1; then
    fail "bazel build //:dev exited non-zero"
fi
pass "bazel build //:dev"

# ---------------------------------------------------------------------------
# Step 2: verify generated outputs in bazel-bin
# ---------------------------------------------------------------------------
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"

# ts_dev_server generates:
#   <name>_dev/vite.config.mjs — the generated Vite dev server config
#   <name>_runner.sh           — the runner script
VITE_CONFIG="$BAZEL_BIN/dev_dev/vite.config.mjs"
RUNNER_SCRIPT="$BAZEL_BIN/dev_runner.sh"

if [[ ! -f "$VITE_CONFIG" ]]; then
    fail "expected vite.config.mjs not found: $VITE_CONFIG"
fi
pass "vite.config.mjs exists: dev_dev/vite.config.mjs"

if [[ ! -f "$RUNNER_SCRIPT" ]]; then
    fail "expected runner script not found: $RUNNER_SCRIPT"
fi
pass "runner script exists: dev_runner.sh"

# ---------------------------------------------------------------------------
# Step 3: verify vite.config.mjs content
# ---------------------------------------------------------------------------

# The generated config should reference the configured port (5173).
if ! grep -q "5173" "$VITE_CONFIG"; then
    fail "vite.config.mjs does not contain port 5173"
fi
pass "vite.config.mjs contains port 5173"

# The config should include server configuration block.
if ! grep -q "server:" "$VITE_CONFIG"; then
    fail "vite.config.mjs does not contain 'server:' block"
fi
pass "vite.config.mjs contains server configuration"

# The config should reference BUILD_WORKSPACE_DIRECTORY for the workspace root.
if ! grep -q "BUILD_WORKSPACE_DIRECTORY" "$VITE_CONFIG"; then
    fail "vite.config.mjs does not reference BUILD_WORKSPACE_DIRECTORY"
fi
pass "vite.config.mjs references BUILD_WORKSPACE_DIRECTORY"

# The config should reference BAZEL_BIN_DIR for bazel-bin file serving.
if ! grep -q "BAZEL_BIN_DIR" "$VITE_CONFIG"; then
    fail "vite.config.mjs does not reference BAZEL_BIN_DIR"
fi
pass "vite.config.mjs references BAZEL_BIN_DIR"

# ---------------------------------------------------------------------------
# Step 4: verify runner script content
# ---------------------------------------------------------------------------

# The runner script should be executable.
if [[ ! -x "$RUNNER_SCRIPT" ]]; then
    fail "runner script is not executable: $RUNNER_SCRIPT"
fi
pass "runner script is executable"

# The runner should reference the Vite binary path.
if ! grep -q "vite" "$RUNNER_SCRIPT"; then
    fail "runner script does not reference vite"
fi
pass "runner script references vite"

# The runner should set BAZEL_BIN_DIR.
if ! grep -q "BAZEL_BIN_DIR" "$RUNNER_SCRIPT"; then
    fail "runner script does not set BAZEL_BIN_DIR"
fi
pass "runner script sets BAZEL_BIN_DIR"

# ---------------------------------------------------------------------------
# Step 5: also verify the TypeScript source compiled correctly
# ---------------------------------------------------------------------------
APP_JS="$BAZEL_BIN/app.js"
APP_DTS="$BAZEL_BIN/app.d.ts"

if [[ ! -f "$APP_JS" ]]; then
    fail "expected compiled app.js not found: $APP_JS"
fi
pass "compiled app.js exists"

if [[ ! -f "$APP_DTS" ]]; then
    fail "expected app.d.ts not found: $APP_DTS"
fi
pass "app.d.ts exists"

echo "ALL PASSED"
