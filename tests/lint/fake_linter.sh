#!/usr/bin/env bash
# fake_linter.sh — Minimal linter stub used in tests.
#
# Accepts the same flags as oxlint/eslint but always succeeds (exit 0).
# Used to test the ts_lint rule plumbing without requiring a real linter binary.

set -euo pipefail

# Parse arguments: we accept --config, --deny-warnings, and file paths.
# We don't actually lint anything — just verify the arguments are well-formed.
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      shift  # skip config path
      shift
      ;;
    --deny-warnings)
      shift
      ;;
    -*)
      shift  # skip unknown flags
      ;;
    *)
      # A source file — verify it exists.
      if [[ ! -f "$1" ]]; then
        echo "fake_linter: file not found: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

# Success — write nothing, exit 0.
exit 0
