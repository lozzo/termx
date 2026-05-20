# Phase 2: Hot Path Alloc / CPU Optimization

## Goal

Reduce repeated hot-path allocations in both TUI and daemon paths without regressing bytes optimization or steady-state residency.

## Commands

```bash
go test ./tuiv2/app -run 'TestNormalizedJoinedLinesWireLenMatchesNormalizedFrameLenJoinedLines|TestNormalizedLinesLenCountsLineBreakSeparators|TestShouldFallbackToFullRepaintRequiresNearFullAndBroadDamage' -count=1
go test . -run 'TestParseNumericStringRejectsNonNumericWithoutChangingOrderingSemantics|TestLessNumericStringKeepsNumericAwareOrdering' -count=1
go test ./tuiv2/app -run '^$' -bench 'Benchmark(OutputCursorWriterWriteFrameLinesDamageProfile|OutputCursorWriterWriteFrameLinesScrollBytesWithCursorMotion|OutputCursorWriterWriteFrameLinesRectScrollBytesByGate)' -benchmem -count=5 | tee tmp/perf/bench/phase2_tui_writer_after.txt
go test . -run '^$' -bench 'Benchmark(ServerHandleRequestList|ServerList|ServerGet|EventBusPublish64Subscribers)' -benchmem -count=5 | tee tmp/perf/bench/phase2_daemon_after.txt
tmp/perf/bin/benchstat tmp/perf/bench/phase2_tui_writer_before.txt tmp/perf/bench/phase2_tui_writer_after.txt > tmp/perf/bench/phase2_tui_writer_benchstat.txt
tmp/perf/bin/benchstat tmp/perf/bench/phase2_daemon_before.txt tmp/perf/bench/phase2_daemon_after.txt > tmp/perf/bench/phase2_daemon_benchstat.txt
go test ./tuiv2/app -run '^$' -bench 'BenchmarkOutputCursorWriterWriteFrameLinesDamageProfile/partial_damage$' -benchmem -count=1 -cpuprofile tmp/perf/pprof/phase2_tui_writer_cpu.prof -memprofile tmp/perf/pprof/phase2_tui_writer_alloc.prof
go test . -run '^$' -bench 'BenchmarkServerList$' -benchmem -count=1 -cpuprofile tmp/perf/pprof/phase2_daemon_cpu.prof -memprofile tmp/perf/pprof/phase2_daemon_alloc.prof
TERMX_RUN_TUI_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyTUI -count=1 -v | tee tmp/perf/current/phase2_tui_residency_after.txt
TERMX_RUN_DAEMON_RESIDENCY=1 go test . -run TestPerfResidencyDaemon -count=1 -v | tee tmp/perf/current/phase2_daemon_residency_after.txt
TERMX_RUN_COMBINED_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyCombined -count=1 -v | tee tmp/perf/current/phase2_combined_residency_after.txt
scripts/nvim-scroll-perf.sh /Users/lozzow/Documents/workdir/termx/tmp/perf/current/phase2_combined_nvim_after
```

## Key Results

- TUI optimization:
  - Replaced join-based full-frame wire-length estimation with `normalizedJoinedLinesWireLen(lines)` in cursor-writer patch planning and owner-delta heuristics.
  - This removed a large `strings.Join` allocation source while preserving the exact wire-length calculation used for repaint threshold decisions.
- daemon optimization:
  - Replaced `parseNumericString()`'s `strconv.ParseUint` failure path with a manual digit scan + overflow check.
  - This preserved ordering semantics while eliminating enormous allocation churn on non-numeric IDs such as `bench-0001`.

## Optimizations

- TUI:
  - Files: `tuiv2/app/cursor_writer.go`, `tuiv2/app/cursor_writer_patch_planner.go`, `tuiv2/app/cursor_writer_owner_delta.go`, `tuiv2/app/cursor_writer_tty_frame.go`
  - Safety:
    - `TestNormalizedJoinedLinesWireLenMatchesNormalizedFrameLenJoinedLines`
    - existing `TestNormalizedLinesLenCountsLineBreakSeparators`
    - existing `TestShouldFallbackToFullRepaintRequiresNearFullAndBroadDamage`
  - Benchstat summary:
    - `partial_damage`: `B/op -27.93%`, `allocs/op -3.92%`, `bytes/op ~`
    - `full_damage`: `sec/op -1.31%`, `B/op -23.55%`, `allocs/op -3.45%`, `bytes/op ~`
    - `lr_scroll_disabled`: `B/op -43.56%`, `allocs/op -11.11%`, `bytes/op ~`
    - TUI writer geomean: `sec/op -0.62%`, `B/op -21.03%`, `allocs/op -3.90%`, `bytes/op +0.01%`
- daemon:
  - Files: `termx.go`, `server_sort_test.go`
  - Safety:
    - `TestParseNumericStringRejectsNonNumericWithoutChangingOrderingSemantics`
    - `TestLessNumericStringKeepsNumericAwareOrdering`
  - Benchstat summary:
    - `ServerList` with non-numeric IDs: `sec/op -46.76%`, `B/op -49.67%`, `allocs/op -99.90%`
    - `ServerListParallel` is only a secondary check and uses numeric IDs; do not treat its `-5.34%` as evidence of a broad daemon-wide speedup
- After profiles:
  - TUI writer alloc profile no longer shows `strings.Join` among the major allocators; the remaining dominant sources are `strings.Builder.WriteString`, `MakeNoZero`, and `verticalScrollCandidate`.
  - daemon alloc profile is now almost entirely legitimate `Server.List` output construction; the old `strconv.syntaxError` fake-allocation hotspot disappeared.

## Risks

- The TUI improvement is mostly allocation pressure, not a large CPU win; this is still worthwhile because it cut heap churn without changing bytes output, but it does not remove the render/body hot path identified in phase1.
- The daemon improvement is strongest for non-numeric IDs. Numeric-only deployments would see less benefit from this exact change.
- Combined `nvim` interactive results after phase2 are mixed:
  - `down_burst_8 first_output_ms`: `17.71 -> 14.48` improved
  - `up_single first_output_ms`: `23.10 -> 24.75` regressed slightly
  - `up_burst_8 first_output_ms`: `25.33 -> 27.08` regressed slightly
  - This looks like measurement noise plus the fact that neither phase2 change attacks `runtime.stream.output` directly; no stable end-to-end claim should be made from this rerun alone.
- Post-GC / post-scavenge floor measurements remained similar:
  - TUI-only RSS floor moved down modestly (`16.5-17.5MB` -> `16.2-16.8MB`)
  - daemon-only RSS floor stayed flat (`9.8MB` -> `9.9MB`)
  - combined post-burst floor was volatile across reruns (`56.6MB` baseline, `71.2MB` on the later rerun), which reinforces that this is not a stable live-RSS signal
  - These numbers are useful for lower-bound comparison only; they should not be used alone to claim cache-growth safety under continuous load.

## Next Phase

Phase 3 should focus on long-lived object ownership/capacity audit, while Phase 4 should target the remaining large hotspots: `runtime.stream.output`, render full-compose metadata/body export, and session event/resync churn.
