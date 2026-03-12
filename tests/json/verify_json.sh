#!/usr/bin/env bash
# Verify that json_library generates a correctly typed .d.ts declaration.
set -euo pipefail

runfiles_dir="${RUNFILES_DIR:-${TEST_SRCDIR:-$0.runfiles}}"

# Find the generated .d.ts file for config.json.
dts_file=$(find "$runfiles_dir" -name "config.json.d.ts" 2>/dev/null | head -1)
if [[ -z "$dts_file" ]]; then
  echo "ERROR: config.json.d.ts not found in runfiles" >&2
  exit 1
fi

echo "Found .d.ts: $dts_file"
cat "$dts_file"

# Verify the overall structure: should start with declare const data.
if ! grep -q "^declare const data:" "$dts_file"; then
  echo "ERROR: .d.ts missing 'declare const data:' declaration" >&2
  exit 1
fi

# Verify it exports the data.
if ! grep -q "^export default data" "$dts_file"; then
  echo "ERROR: .d.ts missing 'export default data'" >&2
  exit 1
fi

# Verify typed fields are present (not just `unknown`).
if grep -q ": unknown" "$dts_file"; then
  echo "ERROR: .d.ts contains 'unknown' — type inference failed" >&2
  exit 1
fi

# Verify specific typed fields.
for field in '"name"' '"port"' '"debug"' '"database"'; do
  if ! grep -q "readonly ${field}:" "$dts_file"; then
    echo "ERROR: .d.ts missing field ${field}" >&2
    exit 1
  fi
done

# Verify name is typed as string.
if ! grep -q '"name": string' "$dts_file"; then
  echo "ERROR: .d.ts 'name' field is not typed as string" >&2
  exit 1
fi

# Verify port is typed as number.
if ! grep -q '"port": number' "$dts_file"; then
  echo "ERROR: .d.ts 'port' field is not typed as number" >&2
  exit 1
fi

# Verify debug is typed as boolean.
if ! grep -q '"debug": boolean' "$dts_file"; then
  echo "ERROR: .d.ts 'debug' field is not typed as boolean" >&2
  exit 1
fi

echo "PASS: json_library .d.ts is correctly typed"
