#!/usr/bin/env bash
# fake_oxlint.sh — Linter stub that accepts only oxlint's CLI.
set -euo pipefail

saw_deny_warnings=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      shift 2
      ;;
    --deny-warnings)
      saw_deny_warnings=1
      shift
      ;;
    -*)
      echo "fake_oxlint: unknown option '$1'" >&2
      exit 2
      ;;
    *)
      if [[ ! -f "$1" ]]; then
        echo "fake_oxlint: file not found: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [[ "$saw_deny_warnings" -ne 1 ]]; then
  echo "fake_oxlint: expected --deny-warnings" >&2
  exit 3
fi
