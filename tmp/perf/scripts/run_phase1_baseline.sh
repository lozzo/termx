#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT"

BENCHSTAT="$(./tmp/perf/scripts/ensure_benchstat.sh)"

mkdir -p tmp/perf/bench tmp/perf/pprof tmp/perf/baseline tmp/perf/current

copy_if_missing() {
  local src="$1"
  local dst="$2"
  if [[ ! -e "$dst" ]]; then
    cp -R "$src" "$dst"
  fi
}

capture_bench() {
  local scope="$1"
  shift
  local out="tmp/perf/bench/phase1_${scope}_before.txt"
  "$@" | tee "$out"
  cp "$out" "tmp/perf/current/phase1_${scope}_before.txt"
  copy_if_missing "$out" "tmp/perf/baseline/phase1_${scope}_before.txt"
}

capture_bench tui_writer \
  go test ./tuiv2/app \
  -run '^$' \
  -bench '^Benchmark(OutputCursorWriterWriteFrameLinesDamageProfile|OutputCursorWriterWriteFrameLinesScrollBytesWithCursorMotion|OutputCursorWriterWriteFrameLinesRectScrollBytesByGate)($|/)' \
  -benchmem -count=5

capture_bench tui_render \
  go test ./tuiv2/render \
  -run '^$' \
  -bench '^Benchmark(CoordinatorRenderFrameFourPanesCached|CoordinatorRenderFrameFourPanesInvalidated|CoordinatorRenderFrameOverlapInvalidated|CoordinatorRenderFrameFloatingDrag|CoordinatorRenderFrameFloatingDragSize|CoordinatorRenderFrameFloatingDragContentComplexity)($|/)' \
  -benchmem -count=5

capture_bench daemon \
  go test . \
  -run '^$' \
  -bench '^Benchmark(ServerHandleRequestList|ServerList|ServerListParallel|ServerGet|EventBusPublish64Subscribers)($|/)' \
  -benchmem -count=5

go test ./tuiv2/app \
  -run '^$' \
  -bench 'BenchmarkOutputCursorWriterWriteFrameLinesDamageProfile/partial_damage$' \
  -benchmem -count=1 \
  -cpuprofile tmp/perf/pprof/phase1_tui_writer_cpu.prof \
  -memprofile tmp/perf/pprof/phase1_tui_writer_alloc.prof
cp tmp/perf/pprof/phase1_tui_writer_alloc.prof tmp/perf/pprof/phase1_tui_writer_heap.prof
cp tmp/perf/pprof/phase1_tui_writer_cpu.prof tmp/perf/current/phase1_tui_writer_cpu.prof
cp tmp/perf/pprof/phase1_tui_writer_alloc.prof tmp/perf/current/phase1_tui_writer_alloc.prof
cp tmp/perf/pprof/phase1_tui_writer_heap.prof tmp/perf/current/phase1_tui_writer_heap.prof
copy_if_missing tmp/perf/pprof/phase1_tui_writer_cpu.prof tmp/perf/baseline/phase1_tui_writer_cpu.prof
copy_if_missing tmp/perf/pprof/phase1_tui_writer_alloc.prof tmp/perf/baseline/phase1_tui_writer_alloc.prof
copy_if_missing tmp/perf/pprof/phase1_tui_writer_heap.prof tmp/perf/baseline/phase1_tui_writer_heap.prof

go test ./tuiv2/render \
  -run '^$' \
  -bench 'BenchmarkCoordinatorRenderFrameOverlapInvalidated/one_floating$' \
  -benchmem -count=1 \
  -cpuprofile tmp/perf/pprof/phase1_tui_render_cpu.prof \
  -memprofile tmp/perf/pprof/phase1_tui_render_alloc.prof
cp tmp/perf/pprof/phase1_tui_render_alloc.prof tmp/perf/pprof/phase1_tui_render_heap.prof
cp tmp/perf/pprof/phase1_tui_render_cpu.prof tmp/perf/current/phase1_tui_render_cpu.prof
cp tmp/perf/pprof/phase1_tui_render_alloc.prof tmp/perf/current/phase1_tui_render_alloc.prof
cp tmp/perf/pprof/phase1_tui_render_heap.prof tmp/perf/current/phase1_tui_render_heap.prof
copy_if_missing tmp/perf/pprof/phase1_tui_render_cpu.prof tmp/perf/baseline/phase1_tui_render_cpu.prof
copy_if_missing tmp/perf/pprof/phase1_tui_render_alloc.prof tmp/perf/baseline/phase1_tui_render_alloc.prof
copy_if_missing tmp/perf/pprof/phase1_tui_render_heap.prof tmp/perf/baseline/phase1_tui_render_heap.prof

go test . \
  -run '^$' \
  -bench 'BenchmarkServerList$' \
  -benchmem -count=1 \
  -cpuprofile tmp/perf/pprof/phase1_daemon_cpu.prof \
  -memprofile tmp/perf/pprof/phase1_daemon_alloc.prof
cp tmp/perf/pprof/phase1_daemon_alloc.prof tmp/perf/pprof/phase1_daemon_heap.prof
cp tmp/perf/pprof/phase1_daemon_cpu.prof tmp/perf/current/phase1_daemon_cpu.prof
cp tmp/perf/pprof/phase1_daemon_alloc.prof tmp/perf/current/phase1_daemon_alloc.prof
cp tmp/perf/pprof/phase1_daemon_heap.prof tmp/perf/current/phase1_daemon_heap.prof
copy_if_missing tmp/perf/pprof/phase1_daemon_cpu.prof tmp/perf/baseline/phase1_daemon_cpu.prof
copy_if_missing tmp/perf/pprof/phase1_daemon_alloc.prof tmp/perf/baseline/phase1_daemon_alloc.prof
copy_if_missing tmp/perf/pprof/phase1_daemon_heap.prof tmp/perf/baseline/phase1_daemon_heap.prof

"$BENCHSTAT" tmp/perf/bench/phase1_tui_writer_before.txt > tmp/perf/bench/phase1_tui_writer_benchstat.txt || true
"$BENCHSTAT" tmp/perf/bench/phase1_tui_render_before.txt > tmp/perf/bench/phase1_tui_render_benchstat.txt || true
"$BENCHSTAT" tmp/perf/bench/phase1_daemon_before.txt > tmp/perf/bench/phase1_daemon_benchstat.txt || true

TERMX_RUN_TUI_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyTUI -count=1 -v \
  | tee tmp/perf/current/phase1_tui_residency_before.txt
copy_if_missing tmp/perf/current/phase1_tui_residency_before.txt tmp/perf/baseline/phase1_tui_residency_before.txt

TERMX_RUN_DAEMON_RESIDENCY=1 go test . -run TestPerfResidencyDaemon -count=1 -v \
  | tee tmp/perf/current/phase1_daemon_residency_before.txt
copy_if_missing tmp/perf/current/phase1_daemon_residency_before.txt tmp/perf/baseline/phase1_daemon_residency_before.txt

TERMX_RUN_COMBINED_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyCombined -count=1 -v \
  | tee tmp/perf/current/phase1_combined_residency_before.txt
copy_if_missing tmp/perf/current/phase1_combined_residency_before.txt tmp/perf/baseline/phase1_combined_residency_before.txt

if command -v nvim >/dev/null 2>&1; then
  "$ROOT/scripts/nvim-scroll-perf.sh" "$ROOT/tmp/perf/current/phase1_combined_nvim_before"
  copy_if_missing "$ROOT/tmp/perf/current/phase1_combined_nvim_before" "$ROOT/tmp/perf/baseline/phase1_combined_nvim_before"
else
  printf 'nvim not found; skipped combined interactive harness\n' | tee "$ROOT/tmp/perf/current/phase1_combined_nvim_before.txt"
  copy_if_missing "$ROOT/tmp/perf/current/phase1_combined_nvim_before.txt" "$ROOT/tmp/perf/baseline/phase1_combined_nvim_before.txt"
fi
