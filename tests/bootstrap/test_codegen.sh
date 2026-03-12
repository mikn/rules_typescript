#!/usr/bin/env bash
# test_codegen.sh — Bootstrap test: ts_codegen rule from scratch.
#
# Verifies the ts_codegen rule in isolation (no npm, no Gazelle required):
#   1. Create a workspace with rules_typescript (no npm deps needed).
#   2. Write a simple generator sh_binary that reads an input schema file
#      and writes a TypeScript source file.
#   3. Write a src/gen/ package with:
#        - sh_binary for the generator
#        - ts_codegen that runs the generator producing generated/constants.ts
#        - ts_compile that compiles the generated output (srcs = [":codegen"])
#   4. Write a src/app/ package with a ts_compile that deps on src/gen.
#   5. Run `bazel build //...` — verify generation + compilation succeeds.
#   6. Check the final .js and .d.ts outputs exist in bazel-bin.
#
# This test exercises ts_codegen independently of Gazelle and npm, confirming
# the rule's core mechanism: run a generator → produce .ts → compile .ts.

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
"""Bootstrap test: ts_codegen rule from scratch."""

module(
    name = "bootstrap_codegen",
    version = "0.0.0",
    compatibility_level = 0,
)

bazel_dep(name = "rules_typescript", version = "0.0.0")
# rules_shell is needed for sh_binary in BUILD files.
# It is a transitive dep of rules_typescript but must be declared
# explicitly in the root module to use @rules_shell load() statements.
bazel_dep(name = "rules_shell", version = "0.6.1")

local_path_override(
    module_name = "rules_typescript",
    path = "$RULES_TS_ROOT",
)

register_toolchains("@rules_typescript//ts/toolchain:all")
EOF

# Minimal root BUILD.bazel (no rules needed at root for this test).
cat > BUILD.bazel <<'EOF'
"""Root BUILD file."""
EOF

# ---------------------------------------------------------------------------
# Write the generator script.
# This shell script reads a simple "schema" file (key=value pairs) and emits
# a TypeScript source file with typed exported constants.
# ---------------------------------------------------------------------------
mkdir -p src/gen

cat > src/gen/schema_gen.sh <<'EOF'
#!/usr/bin/env bash
# schema_gen.sh — Reads a schema file (key=value pairs) and emits TypeScript.
#
# Usage: schema_gen.sh <schema_file> <output.ts>
#
# Schema file format (one definition per line):
#   KEY=value
# Each definition becomes an exported typed constant in the output.

set -euo pipefail

SCHEMA_FILE="$1"
OUTPUT_FILE="$2"

# Create the output directory if it doesn't exist.
mkdir -p "$(dirname "$OUTPUT_FILE")"

{
  echo "// AUTO-GENERATED — do not edit. Generated by schema_gen.sh."
  echo ""
  while IFS='=' read -r key value; do
    # Skip blank lines and comment lines.
    [[ -z "$key" || "$key" == \#* ]] && continue
    echo "export const ${key}: string = \"${value}\";"
  done < "$SCHEMA_FILE"
  echo ""
  echo "export function getAllKeys(): string[] {"
  echo "  return ["
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    echo "    \"${key}\","
  done < "$SCHEMA_FILE"
  echo "  ];"
  echo "}"
} > "$OUTPUT_FILE"

echo "INFO: schema_gen wrote $OUTPUT_FILE" >&2
EOF

chmod +x src/gen/schema_gen.sh

# ---------------------------------------------------------------------------
# Write the input schema file.
# ---------------------------------------------------------------------------
cat > src/gen/app.schema <<'EOF'
APP_NAME=CodegenTestApp
VERSION=2.0.0
ENVIRONMENT=production
EOF

# ---------------------------------------------------------------------------
# Write src/gen/BUILD.bazel with sh_binary, ts_codegen, and ts_compile.
# Pattern: ts_codegen produces a .ts file → ts_compile compiles it.
# The ts_compile target is what downstream consumers depend on.
# ---------------------------------------------------------------------------
cat > src/gen/BUILD.bazel <<'EOF'
"""src/gen BUILD file — ts_codegen + ts_compile for generated constants."""

load("@rules_shell//shell:sh_binary.bzl", "sh_binary")
load("@rules_typescript//ts:defs.bzl", "ts_codegen", "ts_compile")

# The generator binary reads a schema file and emits TypeScript source code.
sh_binary(
    name = "schema_gen",
    srcs = ["schema_gen.sh"],
)

# ts_codegen runs schema_gen as a Bazel build action.
# The generator reads app.schema and declares constants.ts as the output.
# {srcs} expands to the input schema file path at action execution time.
# {out} expands to the declared output file path at action execution time.
ts_codegen(
    name = "constants_gen",
    srcs = ["app.schema"],
    outs = ["constants.ts"],
    generator = ":schema_gen",
    args = [
        "{srcs}",
        "{out}",
    ],
)

# ts_compile compiles the generated TypeScript source.
# srcs accepts ts_codegen output directly via label reference.
# The output (.js + .d.ts) is what downstream consumers depend on.
ts_compile(
    name = "constants",
    srcs = [":constants_gen"],
    isolated_declarations = False,
    enable_check = False,
    visibility = ["//visibility:public"],
)
EOF

# ---------------------------------------------------------------------------
# Write src/app/ — a consumer that depends on the generated constants.
# ---------------------------------------------------------------------------
mkdir -p src/app

# The import path uses "../gen/constants" which ts_compile resolves via
# the .d.ts boundary from the :constants lib target.
cat > src/app/index.ts <<'EOF'
import { APP_NAME, VERSION, getAllKeys } from "../gen/constants";

export function describe(): string {
  return `${APP_NAME} v${VERSION}`;
}

export function listKeys(): string[] {
  return getAllKeys();
}
EOF

cat > src/app/BUILD.bazel <<'EOF'
"""src/app BUILD file — ts_compile depending on generated constants."""

load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "app",
    srcs = ["index.ts"],
    deps = ["//src/gen:constants"],
    isolated_declarations = False,
    visibility = ["//visibility:public"],
)
EOF

# ---------------------------------------------------------------------------
# Step 1: build everything
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 2: verify the generated TypeScript source exists in bazel-bin
# ---------------------------------------------------------------------------
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"

# ts_codegen declares output "constants.ts" in the src/gen package.
GENERATED_TS="$BAZEL_BIN/src/gen/constants.ts"
if [[ ! -f "$GENERATED_TS" ]]; then
    fail "expected generated TypeScript not found: $GENERATED_TS"
fi
pass "generated TypeScript exists: src/gen/constants.ts"

# Verify the generator wrote expected content.
if ! grep -q "APP_NAME" "$GENERATED_TS"; then
    fail "generated TypeScript does not contain APP_NAME"
fi
if ! grep -q "CodegenTestApp" "$GENERATED_TS"; then
    fail "generated TypeScript does not contain value CodegenTestApp"
fi
if ! grep -q "getAllKeys" "$GENERATED_TS"; then
    fail "generated TypeScript does not contain getAllKeys function"
fi
pass "generated TypeScript contains expected constants and function"

# ---------------------------------------------------------------------------
# Step 3: verify the ts_compile compiled the generated source
# ---------------------------------------------------------------------------
GENERATED_JS="$BAZEL_BIN/src/gen/constants.js"
GENERATED_DTS="$BAZEL_BIN/src/gen/constants.d.ts"

if [[ ! -f "$GENERATED_JS" ]]; then
    fail "expected compiled output not found: $GENERATED_JS"
fi
pass "compiled generated output exists: src/gen/constants.js"

if [[ ! -f "$GENERATED_DTS" ]]; then
    fail "expected generated declaration not found: $GENERATED_DTS"
fi
pass "generated declaration exists: src/gen/constants.d.ts"

# Verify the compiled .js exports the generated constants.
if ! grep -q "APP_NAME\|getAllKeys" "$GENERATED_JS"; then
    fail "compiled constants.js does not contain expected exports"
fi
pass "compiled constants.js contains expected exports"

# ---------------------------------------------------------------------------
# Step 4: verify the consumer ts_compile output exists
# ---------------------------------------------------------------------------
APP_JS="$BAZEL_BIN/src/app/index.js"
APP_DTS="$BAZEL_BIN/src/app/index.d.ts"

if [[ ! -f "$APP_JS" ]]; then
    fail "expected compiled consumer output not found: $APP_JS"
fi
pass "compiled consumer output exists: src/app/index.js"

if [[ ! -f "$APP_DTS" ]]; then
    fail "expected consumer declaration not found: $APP_DTS"
fi
pass "consumer declaration exists: src/app/index.d.ts"

# Verify the compiled consumer .js contains expected function names.
if ! grep -q "describe\|listKeys" "$APP_JS"; then
    fail "compiled app/index.js does not contain expected function names"
fi
pass "compiled app/index.js contains expected function names"

echo "ALL PASSED"
