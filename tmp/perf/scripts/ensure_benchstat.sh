#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BIN_DIR="$ROOT/tmp/perf/bin"
mkdir -p "$BIN_DIR"

if [[ -x "$BIN_DIR/benchstat" ]]; then
  echo "$BIN_DIR/benchstat"
  exit 0
fi

GOBIN="$BIN_DIR" go install golang.org/x/perf/cmd/benchstat@latest
echo "$BIN_DIR/benchstat"

