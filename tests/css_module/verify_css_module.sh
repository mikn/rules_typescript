#!/usr/bin/env bash
# Verify that css_module generates a properly typed .d.ts declaration.
set -euo pipefail

# Bazel puts runfiles under a path we can discover.
runfiles_dir="${RUNFILES_DIR:-${TEST_SRCDIR:-$0.runfiles}}"

# Find the generated .d.ts file.
dts_file=$(find "$runfiles_dir" -name "Button.module.css.d.ts" 2>/dev/null | head -1)
if [[ -z "$dts_file" ]]; then
  echo "ERROR: Button.module.css.d.ts not found in runfiles" >&2
  exit 1
fi

echo "Found .d.ts: $dts_file"
cat "$dts_file"

# Verify it contains the expected structure.
if ! grep -q "declare const styles" "$dts_file"; then
  echo "ERROR: .d.ts missing 'declare const styles'" >&2
  exit 1
fi

if ! grep -q "export default styles" "$dts_file"; then
  echo "ERROR: .d.ts missing 'export default styles'" >&2
  exit 1
fi

# Verify class names were extracted correctly.
for class_name in container button label; do
  if ! grep -q "readonly ${class_name}: string" "$dts_file"; then
    echo "ERROR: .d.ts missing class '${class_name}'" >&2
    exit 1
  fi
done

# Verify 'disabled' is also present (used as a compound class .button.disabled).
if ! grep -q "readonly disabled: string" "$dts_file"; then
  echo "ERROR: .d.ts missing class 'disabled'" >&2
  exit 1
fi

echo "PASS: css_module .d.ts is correctly typed"
