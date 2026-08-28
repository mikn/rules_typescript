#!/usr/bin/env bash
# Stands in for the pnpm binary: reports the directory it was started in and
# the command line it was handed, and touches nothing.
set -euo pipefail
echo "cwd=${PWD}"
echo "argv=$*"
