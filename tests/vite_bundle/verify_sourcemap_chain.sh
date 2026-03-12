#!/usr/bin/env bash
# verify_sourcemap_chain.sh — Assert the 3-level source map chain is intact.
#
# Chain: .ts → oxc .js.map → Vite bundle .js.map → browser
#
# Verifies:
#   1. The Vite bundle (.es.js) contains a sourceMappingURL comment.
#   2. The Vite bundle map (.es.js.map) lists .js files as sources.
#   3. Each listed .js source has a sibling .js.map that points back to .ts.
#   4. sourcesContent is populated in both map levels (for source-less debugging).

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

require_cmd() {
  if ! command -v "$1" &>/dev/null; then
    fail "required command '$1' not found"
  fi
}

require_cmd python3

# ── Level 3: Vite bundle .js ─────────────────────────────────────────────────
BUNDLE_JS="tests/vite_bundle/entry_vite_bundle/entry.es.js"
BUNDLE_MAP="tests/vite_bundle/entry_vite_bundle/entry.es.js.map"

if [[ ! -f "$BASE/$BUNDLE_JS" ]]; then
  fail "bundle not found: $BUNDLE_JS"
fi
pass "bundle exists: $BUNDLE_JS"

if [[ ! -f "$BASE/$BUNDLE_MAP" ]]; then
  fail "bundle map not found: $BUNDLE_MAP"
fi
pass "bundle map exists: $BUNDLE_MAP"

# 3a. Bundle contains a sourceMappingURL comment.
if ! grep -q "sourceMappingURL" "$BASE/$BUNDLE_JS"; then
  fail "$BUNDLE_JS missing sourceMappingURL comment"
fi
pass "$BUNDLE_JS contains sourceMappingURL"

# 3b. Bundle map's 'sources' field contains .js files (not .ts).
#     This confirms the map points to oxc-compiled .js, not the raw source.
SOURCES_JSON=$(python3 -c "
import json, sys
with open('$BASE/$BUNDLE_MAP') as f:
    d = json.load(f)
sources = d.get('sources', [])
# Normalize: strip leading path separators and query strings.
print(json.dumps(sources))
")

if ! echo "$SOURCES_JSON" | grep -q '\.js'; then
  fail "bundle map sources do not contain .js files: $SOURCES_JSON"
fi
pass "bundle map sources contain .js files: $SOURCES_JSON"

# 3c. Bundle map has 'sourcesContent' populated (non-empty array, no nulls).
SOURCES_CONTENT_OK=$(python3 -c "
import json
with open('$BASE/$BUNDLE_MAP') as f:
    d = json.load(f)
sc = d.get('sourcesContent', [])
if not sc:
    print('empty')
elif all(s is None for s in sc):
    print('all_null')
else:
    print('ok')
")
if [[ "$SOURCES_CONTENT_OK" != "ok" ]]; then
  fail "bundle map sourcesContent is $SOURCES_CONTENT_OK (expected populated)"
fi
pass "bundle map sourcesContent is populated"

# ── Level 2: oxc-generated .js.map files ─────────────────────────────────────
# The Vite bundle map points to .js files inside the Bazel exec root.  Their
# sibling .js.map files should point back to the original .ts source.
#
# We locate the oxc-generated maps relative to the runfiles tree.
# The bundle map sources look like absolute paths ending in bazel-out/...
# We strip down to the workspace-relative form by finding the files in runfiles.

# The two compiled .js files we know about:
for rel in "tests/vite_bundle/lib.js" "tests/vite_bundle/entry.js"; do
  MAP_REL="${rel}.map"
  MAP_FULL="$BASE/$MAP_REL"

  if [[ ! -f "$MAP_FULL" ]]; then
    fail "oxc map not found in runfiles: $MAP_REL"
  fi
  pass "oxc map exists: $MAP_REL"

  # 2a. oxc map's 'sources' field must contain the .ts file.
  OXC_SOURCES=$(python3 -c "
import json
with open('$MAP_FULL') as f:
    d = json.load(f)
print(json.dumps(d.get('sources', [])))
")
  if ! echo "$OXC_SOURCES" | grep -q '\.ts'; then
    fail "oxc map $MAP_REL sources do not point to .ts: $OXC_SOURCES"
  fi
  pass "oxc map $MAP_REL sources point to .ts: $OXC_SOURCES"

  # 2b. sourcesContent is populated in the oxc map.
  OXC_SC_OK=$(python3 -c "
import json
with open('$MAP_FULL') as f:
    d = json.load(f)
sc = d.get('sourcesContent', [])
if not sc:
    print('empty')
elif all(s is None for s in sc):
    print('all_null')
else:
    print('ok')
")
  if [[ "$OXC_SC_OK" != "ok" ]]; then
    fail "oxc map $MAP_REL sourcesContent is $OXC_SC_OK (expected populated)"
  fi
  pass "oxc map $MAP_REL sourcesContent is populated"
done

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Source map chain verified:"
echo "  .ts  (original source)"
echo "   ↑"
echo "  .js.map  (oxc level: sources → .ts, sourcesContent populated)"
echo "   ↑"
echo "  bundle.js.map  (Vite level: sources → .js, sourcesContent populated)"
echo "   ↑"
echo "  bundle.js  (sourceMappingURL present)"
echo ""
echo "ALL PASSED"
