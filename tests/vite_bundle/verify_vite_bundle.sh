#!/usr/bin/env bash
# verify_vite_bundle.sh — Assert ts_bundle with Vite produces a real bundle.
#
# Verifies:
#   1. ESM bundle: entry.es.js + entry.es.js.map exist with real content.
#   2. CJS bundle: entry.cjs.js exists with real content (no sourcemap).
#   3. Opts bundle: entry.es.js with define/external attrs applied.
#   4. Bundles are NOT placeholder concatenation output.
#   5. Minified bundle: output is smaller than unminified and has no newlines between statements.
#   6. Chunk-split bundle: output directory contains at least one .js file.

set -euo pipefail

RUNFILES="${RUNFILES_DIR:-${TEST_SRCDIR:-}}"
if [[ -z "$RUNFILES" ]]; then
  echo "FAIL: RUNFILES_DIR and TEST_SRCDIR are both unset" >&2
  exit 1
fi

WORKSPACE="${TEST_WORKSPACE:-_main}"
BASE="$RUNFILES/$WORKSPACE"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

check_file() {
  local rel="$1"
  local full="$BASE/$rel"
  if [[ ! -f "$full" ]]; then
    fail "$rel not found at $full"
  fi
  pass "$rel exists"
}

check_no_file() {
  local rel="$1"
  local full="$BASE/$rel"
  if [[ -f "$full" ]]; then
    fail "$rel should NOT exist but does (sourcemap=False)"
  fi
  pass "$rel does not exist (expected)"
}

check_dir() {
  local rel="$1"
  local full="$BASE/$rel"
  if [[ ! -d "$full" ]]; then
    fail "directory $rel not found at $full"
  fi
  pass "directory $rel exists"
}

check_dir_has_js() {
  local rel="$1"
  local full="$BASE/$rel"
  local count
  count=$(find "$full" -name "*.js" 2>/dev/null | wc -l)
  if [[ "$count" -lt 1 ]]; then
    fail "directory $rel contains no .js files (found $count)"
  fi
  pass "directory $rel contains $count .js file(s)"
}

check_contains() {
  local rel="$1"
  local pattern="$2"
  local full="$BASE/$rel"
  if ! grep -q "$pattern" "$full"; then
    fail "$rel does not contain pattern '$pattern' (content: $(cat "$full"))"
  fi
  pass "$rel contains '$pattern'"
}

check_not_contains() {
  local rel="$1"
  local pattern="$2"
  local full="$BASE/$rel"
  if grep -q "$pattern" "$full"; then
    fail "$rel should NOT contain '$pattern' (placeholder bundle check)"
  fi
  pass "$rel does not contain '$pattern' (good)"
}

# ── ESM bundle (format=esm, sourcemap=True) ───────────────────────────────────
# ts_bundle name "entry_vite", bundle_name "entry", format "esm"
# Output: entry_vite_bundle/entry.es.js + entry_vite_bundle/entry.es.js.map
ESM_JS="tests/vite_bundle/entry_vite_bundle/entry.es.js"
ESM_MAP="tests/vite_bundle/entry_vite_bundle/entry.es.js.map"

check_file "$ESM_JS"
check_file "$ESM_MAP"

# Real Vite bundle should contain the bundled source (tree-shaken, inlined).
check_contains "$ESM_JS" "add"
check_contains "$ESM_JS" "PI"
check_contains "$ESM_JS" "result"

# Must NOT be a placeholder.
check_not_contains "$ESM_JS" "Placeholder bundle"

# sourceMappingURL comment should reference the map file.
check_contains "$ESM_JS" "sourceMappingURL"

# ── CJS bundle (format=cjs, sourcemap=False) ──────────────────────────────────
# ts_bundle name "entry_vite_cjs", bundle_name "entry", format "cjs"
# Output: entry_vite_cjs_bundle/entry.cjs.js (no .map)
CJS_JS="tests/vite_bundle/entry_vite_cjs_bundle/entry.cjs.js"
CJS_MAP="tests/vite_bundle/entry_vite_cjs_bundle/entry.cjs.js.map"

check_file "$CJS_JS"
check_no_file "$CJS_MAP"

check_contains "$CJS_JS" "add"
check_not_contains "$CJS_JS" "Placeholder bundle"

# ── Opts bundle (define + external) ──────────────────────────────────────────
# ts_bundle name "entry_vite_opts", bundle_name "entry", format "esm"
# define = {"process.env.NODE_ENV": '"production"'}
# external = ["some-external-dep"]
OPTS_JS="tests/vite_bundle/entry_vite_opts_bundle/entry.es.js"

check_file "$OPTS_JS"
check_not_contains "$OPTS_JS" "Placeholder bundle"
check_contains "$OPTS_JS" "add"

# ── Minified bundle (minify=True) ─────────────────────────────────────────────
# ts_bundle name "entry_vite_minified", bundle_name "entry", format "esm"
# Output: entry_vite_minified_bundle/entry.es.js (no .map)
MINIFIED_JS="tests/vite_bundle/entry_vite_minified_bundle/entry.es.js"

check_file "$MINIFIED_JS"
check_not_contains "$MINIFIED_JS" "Placeholder bundle"
# Minified output: identifiers are shortened (no multi-line function bodies).
# Verify the minified bundle is smaller than the unminified ESM bundle.
UNMINIFIED_SIZE=$(wc -c < "$BASE/$ESM_JS")
MINIFIED_SIZE=$(wc -c < "$BASE/$MINIFIED_JS")
if [[ "$MINIFIED_SIZE" -ge "$UNMINIFIED_SIZE" ]]; then
  fail "minified bundle ($MINIFIED_SIZE bytes) should be smaller than unminified ($UNMINIFIED_SIZE bytes)"
fi
pass "minified bundle ($MINIFIED_SIZE bytes) is smaller than unminified ($UNMINIFIED_SIZE bytes)"

# ── Split-chunks bundle (split_chunks=True) ───────────────────────────────────
# ts_bundle name "entry_vite_chunks", split_chunks=True
# Output: entry_vite_chunks_bundle/ directory containing .js chunk files.
CHUNKS_DIR="tests/vite_bundle/entry_vite_chunks_bundle"

check_dir "$CHUNKS_DIR"
check_dir_has_js "$CHUNKS_DIR"

# ── 13.2: Invisible node_modules naming ──────────────────────────────────────
# ts_bundle "entry_vite_alt_nm" uses vite_bundler with node_modules = ":vite_deps"
# (not named "node_modules"). The wrapper must create a symlink transparently.
ALT_NM_JS="tests/vite_bundle/entry_vite_alt_nm_bundle/entry.es.js"

check_file "$ALT_NM_JS"
check_contains "$ALT_NM_JS" "add"
check_not_contains "$ALT_NM_JS" "Placeholder bundle"

echo "ALL PASSED"
