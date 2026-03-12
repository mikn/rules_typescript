#!/usr/bin/env bash
# test_css_assets.sh — Bootstrap test: CSS modules, plain CSS, and asset imports.
#
# Simulates the full user journey for a project with CSS Modules, SVG assets,
# and JSON data files:
#   1. Create a temp directory with a minimal MODULE.bazel.
#   2. Write TypeScript sources that import CSS modules, SVG assets, and JSON.
#   3. Run `bazel run //:gazelle` to generate BUILD files.
#   4. Verify Gazelle generated css_module, asset_library, and json_library targets.
#   5. Run `bazel build //...` to compile and type-check.
#   6. Assert expected output files (.css.d.ts, .svg.d.ts, .json.d.ts) exist.

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

# Correctness
build --incompatible_strict_action_env
build --nolegacy_external_runfiles

# Always type-check on build
build --output_groups=+_validation

# Test output
test --test_output=errors
test --test_summary=terse
EOF

cat > MODULE.bazel <<EOF
"""Bootstrap test: CSS modules, plain CSS, and asset imports."""

module(
    name = "bootstrap_css_assets",
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
# Write source files
# ---------------------------------------------------------------------------
mkdir -p src/components

# CSS Module: Gazelle will generate a css_module target for this.
cat > src/components/Button.module.css <<'EOF'
.button {
  color: red;
  background: blue;
  padding: 8px 16px;
  border-radius: 4px;
  cursor: pointer;
}

.button:hover {
  opacity: 0.8;
}

.icon {
  margin-right: 4px;
}
EOF

# SVG asset: Gazelle will generate an asset_library target for this.
cat > src/components/logo.svg <<'EOF'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
  <circle cx="50" cy="50" r="50" fill="#3b82f6"/>
  <text x="50" y="55" text-anchor="middle" fill="white" font-size="30">TS</text>
</svg>
EOF

# JSON data file: Gazelle will generate a json_library target for this.
cat > src/components/config.json <<'EOF'
{
  "appName": "CSS Assets Test",
  "version": "1.0.0",
  "theme": {
    "primaryColor": "#3b82f6",
    "fontSize": 16
  }
}
EOF

# Button.tsx imports the CSS module and the SVG asset.
# Uses explicit return types for isolated declarations compatibility.
cat > src/components/Button.tsx <<'EOF'
import styles from "./Button.module.css";
import logo from "./logo.svg";

export interface ButtonProps {
  label: string;
  onClick?: () => void;
}

export function Button({ label, onClick }: ButtonProps): string {
  // At runtime, styles.button is a hashed class name string (CSS Modules).
  // logo is a URL string (asset import).
  return `<button class="${styles.button}" onclick="${onClick?.toString() ?? ""}">
    <img src="${logo}" class="${styles.icon}" alt="logo" />
    ${label}
  </button>`;
}

export function getButtonClass(): string {
  return styles.button;
}
EOF

# App.tsx imports the JSON config and uses the Button component.
cat > src/components/App.tsx <<'EOF'
import config from "./config.json";
import { Button } from "./Button";

export function getAppTitle(): string {
  return config.appName;
}

export function renderApp(): string {
  const btn = Button({ label: "Click me" });
  return `<div style="font-size: ${config.theme.fontSize}px">${btn}</div>`;
}
EOF

# ---------------------------------------------------------------------------
# Step 1: run Gazelle to generate BUILD files
# ---------------------------------------------------------------------------
echo "INFO: running gazelle..."
if ! bazel_run run //:gazelle 2>&1; then
    fail "bazel run //:gazelle exited non-zero"
fi
pass "bazel run //:gazelle"

# ---------------------------------------------------------------------------
# Step 2: verify Gazelle generated BUILD file
# ---------------------------------------------------------------------------
if [[ ! -f "src/components/BUILD.bazel" ]]; then
    fail "Gazelle did not generate src/components/BUILD.bazel"
fi
pass "src/components/BUILD.bazel generated"

# Verify css_module target was generated for Button.module.css
if ! grep -q "css_module" src/components/BUILD.bazel; then
    fail "src/components/BUILD.bazel does not contain a css_module target"
fi
pass "css_module target present in src/components/BUILD.bazel"

# Verify asset_library target was generated for logo.svg
if ! grep -q "asset_library" src/components/BUILD.bazel; then
    fail "src/components/BUILD.bazel does not contain an asset_library target"
fi
pass "asset_library target present in src/components/BUILD.bazel"

# Verify json_library target was generated for config.json
if ! grep -q "json_library" src/components/BUILD.bazel; then
    fail "src/components/BUILD.bazel does not contain a json_library target"
fi
pass "json_library target present in src/components/BUILD.bazel"

# Verify ts_compile target was generated for the TypeScript sources
if ! grep -q "ts_compile" src/components/BUILD.bazel; then
    fail "src/components/BUILD.bazel does not contain a ts_compile target"
fi
pass "ts_compile target present in src/components/BUILD.bazel"

# ---------------------------------------------------------------------------
# Step 3: build — compile + type-check
# ---------------------------------------------------------------------------
echo "INFO: running bazel build //..."
if ! bazel_run build //... 2>&1; then
    fail "bazel build //... exited non-zero"
fi
pass "bazel build //..."

# ---------------------------------------------------------------------------
# Step 4: verify output files exist in bazel-bin
# ---------------------------------------------------------------------------
BAZEL_BIN="$(bazel_run info bazel-bin 2>/dev/null)"

# Verify TypeScript compilation outputs
for rel in "src/components/Button.js" "src/components/Button.d.ts" \
           "src/components/App.js" "src/components/App.d.ts"; do
    f="$BAZEL_BIN/$rel"
    if [[ ! -f "$f" ]]; then
        fail "expected output file not found: $f"
    fi
    pass "output file exists: $rel"
done

# Verify the CSS module .d.ts was generated by the css_module rule.
# css_module generates <basename>.d.ts next to the CSS file.
CSS_DTS="$BAZEL_BIN/src/components/Button.module.css.d.ts"
if [[ ! -f "$CSS_DTS" ]]; then
    fail "expected CSS module declaration not found: $CSS_DTS"
fi
pass "CSS module declaration exists: src/components/Button.module.css.d.ts"

# Verify the CSS module .d.ts contains the expected class names.
if ! grep -q "button" "$CSS_DTS"; then
    fail "CSS module declaration does not contain 'button' class"
fi
if ! grep -q "icon" "$CSS_DTS"; then
    fail "CSS module declaration does not contain 'icon' class"
fi
pass "CSS module declaration contains expected class names"

# Verify the SVG asset .d.ts was generated by asset_library.
SVG_DTS="$BAZEL_BIN/src/components/logo.svg.d.ts"
if [[ ! -f "$SVG_DTS" ]]; then
    fail "expected asset declaration not found: $SVG_DTS"
fi
pass "asset declaration exists: src/components/logo.svg.d.ts"

# Verify the JSON .d.ts was generated by json_library.
JSON_DTS="$BAZEL_BIN/src/components/config.json.d.ts"
if [[ ! -f "$JSON_DTS" ]]; then
    fail "expected JSON declaration not found: $JSON_DTS"
fi
pass "JSON declaration exists: src/components/config.json.d.ts"

# Verify the JSON .d.ts contains typed fields from the JSON structure.
if ! grep -q "appName" "$JSON_DTS"; then
    fail "JSON declaration does not contain 'appName' field"
fi
pass "JSON declaration contains 'appName' field"

echo "ALL PASSED"
