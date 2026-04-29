#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-${TERMX_PERF_DIR:-/tmp/termx-perf-$(date +%Y%m%d-%H%M%S)}}"

mkdir -p "$OUT_DIR"

export TERMX_RUN_NVIM_TRACE=1
export TERMX_PERF_OUT="$OUT_DIR/nvim-scroll.json"

cd "$ROOT_DIR"

echo "writing perf artifacts to: $OUT_DIR"
echo "running nvim scroll perf trace..."

go test ./app -run TestPerfNvimScrollReport -count=1 -v | tee "$OUT_DIR/test.log"

if [[ "${TERMX_PERF_PPROF:-0}" == "1" ]]; then
  echo "collecting cpu profile..."
  go test ./app -run TestPerfNvimScrollReport -count=1 -cpuprofile "$OUT_DIR/cpu.out"
fi

echo "perf report: $OUT_DIR/nvim-scroll.json"
