#!/usr/bin/env bash
# Verify that asset_library generates correct ambient .d.ts declarations.
set -euo pipefail

runfiles_dir="${RUNFILES_DIR:-${TEST_SRCDIR:-$0.runfiles}}"

# Find the generated .d.ts file for the SVG.
dts_file=$(find "$runfiles_dir" -name "logo.svg.d.ts" 2>/dev/null | head -1)
if [[ -z "$dts_file" ]]; then
  echo "ERROR: logo.svg.d.ts not found in runfiles" >&2
  exit 1
fi

echo "Found .d.ts: $dts_file"
cat "$dts_file"

# Verify it contains the expected ambient declaration.
if ! grep -q "declare const asset: string" "$dts_file"; then
  echo "ERROR: .d.ts missing 'declare const asset: string'" >&2
  exit 1
fi

if ! grep -q "export default asset" "$dts_file"; then
  echo "ERROR: .d.ts missing 'export default asset'" >&2
  exit 1
fi

# Verify the SVG source file is also present in runfiles.
svg_file=$(find "$runfiles_dir" -name "logo.svg" ! -name "*.d.ts" 2>/dev/null | head -1)
if [[ -z "$svg_file" ]]; then
  echo "ERROR: logo.svg not found in runfiles" >&2
  exit 1
fi

echo "Found SVG: $svg_file"
echo "PASS: asset_library .d.ts is correctly generated"
