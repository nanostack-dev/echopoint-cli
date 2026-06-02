#!/usr/bin/env bash
# Run all action tests.
# Usage: ./action-tests/run_tests.sh [--tap]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if ! command -v bats >/dev/null 2>&1; then
  echo "ERROR: bats not found. Install with: brew install bats-core" >&2
  exit 1
fi

extra_args=()
if [ "${1:-}" = "--tap" ]; then
  extra_args+=(--formatter tap)
fi

# Expand extra_args safely: under `set -u`, "${extra_args[@]}" on an empty array errors
# ("unbound variable") on bash 3.2 (the macOS default), so guard the expansion.
exec bats ${extra_args[@]+"${extra_args[@]}"} "${SCRIPT_DIR}/action.bats"
