#!/usr/bin/env bash
# What `wrangler types` does for a worker, without wrangler: each binding name
# becomes a WorkerEnv member, and the build id a global no binding could be.
set -euo pipefail

BINDINGS=""
OUT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bindings) BINDINGS="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "env_types_gen.sh: unknown argument: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$BINDINGS" || -z "$OUT" ]]; then
  echo "env_types_gen.sh: --bindings and --out are required" >&2
  exit 1
fi

{
  echo "declare const WORKER_BUILD_ID: string;"
  echo
  echo "interface WorkerEnv {"
  while read -r name; do
    [[ -z "$name" ]] && continue
    printf '\treadonly %s: string;\n' "$name"
  done < "$BINDINGS"
  echo "}"
} > "$OUT"
