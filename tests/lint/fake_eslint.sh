#!/usr/bin/env bash
# fake_eslint.sh — Linter stub that accepts only ESLint's CLI.
#
# ts_lint must pass the warning-as-error flag of the linter it was told about,
# so this stub rejects oxlint's spelling of it.
set -euo pipefail

saw_max_warnings=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      shift 2
      ;;
    --max-warnings=0)
      saw_max_warnings=1
      shift
      ;;
    --deny-warnings)
      echo "fake_eslint: unknown option '--deny-warnings' (that is oxlint's)" >&2
      exit 2
      ;;
    -*)
      echo "fake_eslint: unknown option '$1'" >&2
      exit 2
      ;;
    *)
      if [[ ! -f "$1" ]]; then
        echo "fake_eslint: file not found: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [[ "$saw_max_warnings" -ne 1 ]]; then
  echo "fake_eslint: expected --max-warnings=0" >&2
  exit 3
fi
