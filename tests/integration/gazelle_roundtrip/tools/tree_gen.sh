#!/usr/bin/env bash
# tree_gen.sh --names <names file> --outdir <dir>
# One .js/.d.ts pair per name in the input file, plus an index re-exporting them.
set -euo pipefail

NAMES_FILE=""
OUT_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --names) NAMES_FILE="$2"; shift 2 ;;
    --outdir) OUT_DIR="$2"; shift 2 ;;
    *) echo "tree_gen.sh: unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$NAMES_FILE" || -z "$OUT_DIR" ]]; then
  echo "tree_gen.sh: --names and --outdir are required" >&2
  exit 1
fi

mkdir -p "$OUT_DIR/messages"
: > "$OUT_DIR/index.js"
: > "$OUT_DIR/index.d.ts"

while read -r name; do
  [[ -z "$name" ]] && continue
  cat > "$OUT_DIR/messages/${name}.js" <<EOT
export const ${name} = (count) => \`${name}: \${count}\`;
EOT
  cat > "$OUT_DIR/messages/${name}.d.ts" <<EOT
export declare const ${name}: (count: number) => string;
EOT
  echo "export { ${name} } from \"./messages/${name}.js\";" >> "$OUT_DIR/index.js"
  echo "export { ${name} } from \"./messages/${name}.js\";" >> "$OUT_DIR/index.d.ts"
done < "$NAMES_FILE"
