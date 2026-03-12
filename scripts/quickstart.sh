#!/usr/bin/env bash
#
# quickstart.sh — Verify zero-prerequisites first run for rules_typescript
#
# Creates a minimal consumer workspace in a temporary directory that depends on
# rules_typescript via local_path_override, then runs bazel build //... to
# confirm that everything fetches and builds correctly with only Bazelisk (or
# Bazel 9+) installed.
#
# No pre-installed tools are required: Rust, Go, Node.js, and all npm packages
# are fetched hermetically by Bazel during the first build.
#
# Usage:
#   bash scripts/quickstart.sh [--rules-path PATH]
#
# Options:
#   --rules-path PATH   Path to a rules_typescript checkout.
#                       Defaults to the parent directory of this script.
#
# Exit code 0 on success, non-zero on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RULES_TS_ROOT="$(dirname "$SCRIPT_DIR")"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rules-path)
      RULES_TS_ROOT="$(realpath "$2")"
      shift 2
      ;;
    --help|-h)
      sed -n '3,20p' "$0" | sed 's/^# \{0,2\}//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# ── Prerequisite check ────────────────────────────────────────────────────────

if ! command -v bazel &>/dev/null; then
  cat >&2 <<'EOF'
Error: 'bazel' not found in PATH.

Install Bazelisk (the recommended Bazel launcher) with one of:

  # macOS (Homebrew)
  brew install bazelisk

  # Linux / macOS (manual)
  curl -Lo ~/.local/bin/bazel \
    https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64
  chmod +x ~/.local/bin/bazel

  # Go (any platform)
  go install github.com/bazelbuild/bazelisk@latest

Bazelisk reads the .bazelversion file and downloads the correct Bazel version
automatically. The rules_typescript repository requires Bazel 9+.
EOF
  exit 1
fi

BAZEL_VERSION="$(bazel version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")"
BAZEL_MAJOR="$(echo "$BAZEL_VERSION" | cut -d. -f1)"
if [[ "$BAZEL_MAJOR" -lt 9 ]] 2>/dev/null; then
  echo "Error: Bazel 9+ is required (found $BAZEL_VERSION)." >&2
  echo "Install Bazelisk to manage the Bazel version automatically." >&2
  exit 1
fi

echo "Using Bazel $BAZEL_VERSION"
echo "Using rules_typescript from: $RULES_TS_ROOT"
echo

# ── Create temp workspace ─────────────────────────────────────────────────────

TMPDIR_ROOT="$(mktemp -d)"
WORKSPACE="$TMPDIR_ROOT/my_project"
mkdir -p "$WORKSPACE/src/lib" "$WORKSPACE/src/app"

cleanup() {
  echo
  echo "Cleaning up temp workspace: $WORKSPACE"
  # Shut down the Bazel server launched in the temp workspace so the temp
  # directory can be removed on all platforms.
  (cd "$WORKSPACE" && bazel shutdown 2>/dev/null || true)
  rm -rf "$TMPDIR_ROOT"
}
trap cleanup EXIT

# .bazelversion — inherit the same Bazel version as this repository
cp "$RULES_TS_ROOT/.bazelversion" "$WORKSPACE/.bazelversion"

# WORKSPACE.bazel — empty sentinel required by Bazel 9 for non-module workspaces
touch "$WORKSPACE/WORKSPACE.bazel"

# MODULE.bazel — minimal consumer depending on rules_typescript
cat > "$WORKSPACE/MODULE.bazel" <<EOF
"""Minimal rules_typescript consumer — zero-prerequisites quickstart test."""

module(
    name = "my_project",
    version = "0.0.0",
)

bazel_dep(name = "rules_typescript", version = "0.1.0")

# Use the local checkout instead of the BCR version.
local_path_override(
    module_name = "rules_typescript",
    path = "$RULES_TS_ROOT",
)
EOF

# Root BUILD.bazel — empty (Gazelle would generate targets; we do it manually)
cat > "$WORKSPACE/BUILD.bazel" <<'EOF'
# Root package (intentionally minimal for quickstart)
EOF

# src/lib/math.ts — simple utility with explicit return types (isolated_declarations)
cat > "$WORKSPACE/src/lib/math.ts" <<'EOF'
export function add(a: number, b: number): number {
  return a + b;
}

export function multiply(a: number, b: number): number {
  return a * b;
}
EOF

# src/lib/index.ts — barrel export
cat > "$WORKSPACE/src/lib/index.ts" <<'EOF'
export { add, multiply } from "./math";
EOF

# src/lib/BUILD.bazel
cat > "$WORKSPACE/src/lib/BUILD.bazel" <<'EOF'
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "lib",
    srcs = [
        "index.ts",
        "math.ts",
    ],
    visibility = ["//visibility:public"],
)
EOF

# src/app/main.ts — uses the lib
cat > "$WORKSPACE/src/app/main.ts" <<'EOF'
import { add, multiply } from "../lib/index";

const sum: number = add(2, 3);
const product: number = multiply(4, 5);

console.log("sum:", sum);
console.log("product:", product);
EOF

# src/app/BUILD.bazel
cat > "$WORKSPACE/src/app/BUILD.bazel" <<'EOF'
load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "app",
    srcs = ["main.ts"],
    deps = ["//src/lib"],
    visibility = ["//visibility:public"],
)
EOF

# ── Run the build ─────────────────────────────────────────────────────────────

echo "=== Step 1: bazel build //... ==="
echo "(This fetches all toolchains on first run. It may take a few minutes.)"
echo

if ! (cd "$WORKSPACE" && bazel build //... 2>&1); then
  echo
  echo "ERROR: bazel build //... failed in the test workspace." >&2
  exit 1
fi

echo
echo "=== Step 2: bazel build //... --output_groups=+_validation ==="
echo "(Runs tsgo type-checking in addition to compilation.)"
echo

if ! (cd "$WORKSPACE" && bazel build //... --output_groups=+_validation 2>&1); then
  echo
  echo "ERROR: type-checking (--output_groups=+_validation) failed." >&2
  exit 1
fi

echo
echo "=== Quickstart passed ==="
echo
echo "Both compilation and type-checking succeeded from a fresh workspace"
echo "with only Bazelisk installed. The workspace was:"
echo "  $WORKSPACE"
echo
echo "The only prerequisite for rules_typescript is Bazelisk (or Bazel 9+)."
echo "All other tools (Rust, Go, Node.js, npm packages) are fetched by Bazel."
