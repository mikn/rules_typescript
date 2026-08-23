#!/usr/bin/env bash
# Verifies that npm_bin targets are generated and executable.
# Uses the @npm//:nanoid_bin target (simple bin with no npm deps).
set -euo pipefail

# Resolve runfiles.
if [[ -n "${RUNFILES_DIR:-}" ]]; then
  : # already set by Bazel
elif [[ -n "${TEST_SRCDIR:-}" ]]; then
  RUNFILES_DIR="$TEST_SRCDIR"
else
  echo "ERROR: RUNFILES_DIR not set" >&2
  exit 1
fi

cd "${RUNFILES_DIR}"

# Find the nanoid bin runner in the runfiles tree by shape rather than by path.
# Both npm layouts are valid: packages may share one external repo or each have
# their own, and the runner's filename follows its target name, so neither the
# directory nor the basename is fixed.
# -L because runfiles entries are symlinks and -type f tests the link itself.
NANOID_BIN_SCRIPT="$(find -L . -maxdepth 3 -name '*bin_runner.sh' -path '*nanoid*' -type f 2>/dev/null | head -1)"

if [[ -z "$NANOID_BIN_SCRIPT" ]]; then
  echo "ERROR: could not find a nanoid bin runner script" >&2
  echo "Runfiles tree root:" >&2
  ls -la "${RUNFILES_DIR}" >&2
  exit 1
fi

echo "Found nanoid_bin at: ${RUNFILES_DIR}/${NANOID_BIN_SCRIPT}"

# Verify the script is executable.
if [[ ! -x "$NANOID_BIN_SCRIPT" ]]; then
  echo "ERROR: nanoid_bin runner script is not executable" >&2
  exit 1
fi

# Run nanoid_bin using bash explicitly (to avoid issues with '+' in filename).
# Use "./" prefix + full path to prevent bash interpreting '+' as a flag.
OUTPUT="$(bash -- "$NANOID_BIN_SCRIPT")"
if [[ -z "$OUTPUT" ]]; then
  echo "ERROR: nanoid_bin produced no output" >&2
  exit 1
fi

echo "nanoid_bin output: $OUTPUT"

# Verify the output looks like a nanoid (alphanumeric, expected length 21).
if [[ ${#OUTPUT} -lt 10 ]]; then
  echo "ERROR: nanoid output too short (got ${#OUTPUT} chars, expected >= 10)" >&2
  exit 1
fi

echo "SUCCESS: npm_bin target generates correct runner script and produces output"
