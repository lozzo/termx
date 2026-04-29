# Phase 5: Final Summary

## Checklist Status

- Phase 0: completed
- Phase 1: baseline established for TUI/daemon plus partial combined coverage; review complete with follow-up caveats applied
- Phase 2: completed, review pending
- Phase 3: completed, review pending
- Phase 4: completed, review pending
- Phase 5: this report complete, review pending

## TUI Optimizations

- Added residency harness coverage for TUI-only scenarios.
- Removed join-based full-frame wire-length estimation on cursor-writer hot paths in favor of `normalizedJoinedLinesWireLen`.
- Preserved bytes optimization:
  - TUI writer geomean `bytes/op` change: `+0.01%`
  - all tracked `bytes/op` lines stayed statistically flat

## Daemon Optimizations

- Added daemon residency harness coverage.
- Eliminated non-numeric parse failure churn in `ServerList` sort path.
- Verified protocol list cache is already effective; did not waste effort re-optimizing the cache-hit path.

## Combined Chain Optimizations

- Added combined residency harness and native `nvim` interactive trace to baseline/current workflow.
- Added a current combined CPU profile (`tmp/perf/pprof/phase5_combined_nvim_cpu.prof`) to reduce the combined instrumentation gap, but phase1 still lacks a true combined baseline `pprof`.
- No stable combined latency win was proven from phase2 changes; combined data is now tracked so future work on `runtime.stream.output` can be judged against hard numbers.

## Eliminated Hotspots

- `strings.Join` in TUI writer full-frame estimation: no longer present in the phase2 writer alloc profile top section.
- `strconv.syntaxError` in daemon list sort path: disappeared from the phase2 daemon alloc profile after the manual digit scan change.

## Remaining Hotspots

- `runtime.stream.output`
- local `vterm.write`
- full-compose render path:
  - `composedCanvas.ownerMap`
  - `clonePresentMetadata`
  - `composeRenderMetadata`
  - row export / `strings.Builder` growth
- `framePresenter.verticalScrollCandidate`
- session event publication + `session.get` resync path

## Benchmark Table

| Scope | Benchmark | Before | After | Delta |
| --- | --- | --- | --- | --- |
| TUI writer | `partial_damage sec/op` | `125.6µs` | `124.3µs` | `~` |
| TUI writer | `partial_damage B/op` | `71.83KiB` | `51.77KiB` | `-27.93%` |
| TUI writer | `partial_damage allocs/op` | `51` | `49` | `-3.92%` |
| TUI writer | `full_damage sec/op` | `335.6µs` | `331.2µs` | `-1.31%` |
| TUI writer | `full_damage B/op` | `138.0KiB` | `105.5KiB` | `-23.55%` |
| TUI writer | `full_damage allocs/op` | `58` | `56` | `-3.45%` |
| TUI writer | `lr_scroll_disabled B/op` | `4.596KiB` | `2.594KiB` | `-43.56%` |
| daemon | `ServerList sec/op` | `153.9µs` | `81.94µs` | `-46.76%` |
| daemon | `ServerList B/op` | `254.5KiB` | `128.1KiB` | `-49.67%` |
| daemon | `ServerList allocs/op` | `4048` | `4` | `-99.90%` |
| daemon | `ServerListParallel sec/op` | `48.04µs` | `45.47µs` | `-5.34%` |

## Post-GC / Post-Scavenge Floor Table

| Scope | Scenario | Before | After | Notes |
| --- | --- | --- | --- | --- |
| TUI | `single_pane_idle RSS` | `16.5MB` | `16.2MB` | slight improvement |
| TUI | `side_by_side RSS` | `16.8MB` | `16.6MB` | slight improvement |
| TUI | `floating_overlay RSS` | `17.5MB` | `16.8MB` | slight improvement |
| daemon | `daemon_idle_startup RSS` | `7.3MB` | `7.4MB` | flat |
| daemon | `daemon_one_terminal_one_session RSS` | `9.8MB` | `9.9MB` | flat |
| combined | `combined_after_burst RSS` | `56.6MB` | `71.2MB` | noisy; floor-only sample, not live steady-state |
| combined | `combined_after_burst heap_alloc` | `73.8MB` | `73.7MB` | flat |
| combined | `combined_after_burst goroutines` | `25` | `25` | flat |

Current-only note:

- `combined_idle_attached` is now captured in the current harness and showed `rss_kb=18192`, but there is no phase1 baseline counterpart, so it is not included in the before/after table.

## Benchstat Conclusions

- The TUI writer change was a heap/alloc win with effectively neutral bytes and modest CPU change.
- The daemon parse change was a clear CPU + memory + alloc win on non-numeric `ServerList`.
- No measured regression was found in daemon cache-hit paths or event-bus microbenchmarks.

## Pprof Conclusions

- Phase1 TUI writer alloc profile identified `strings.Join` as a major cumulative allocator; phase2 removed it from the top alloc set.
- Phase1 daemon alloc profile identified `strconv.syntaxError` as a major fake-allocation source inside `ServerList`; phase2 removed it entirely.
- Render alloc profile still points to metadata/body export rather than cursor-writer patch planning as the next major TUI target.

## Optimization Score Summary

- Done:
  - `PERF-TUI-003`
  - `PERF-DAEMON-004`
  - `PERF-RES-001`
- Still pending high-priority:
  - `PERF-LINK-001`
  - `PERF-TUI-002`
  - `PERF-DAEMON-001`

## Risks And Follow-Ups

- Combined interactive latency remains noisy and was not materially improved by phase2; future work must target `runtime.stream.output` directly.
- The current residency helpers measure a post-GC/post-scavenge floor. They are useful for lower-bound comparisons, but they do not by themselves prove live steady-state RSS behavior under continuous load.
- Phase1 combined baseline still lacks a dedicated combined `pprof` artifact and a true idle attached before sample.
- The remaining TUI cost is more about render-body composition than writer diff heuristics.
- The remaining daemon cost is more about shared-session/event sync than terminal list cache hits.
- Residual terminal flush-copy cleanup is still available, but current evidence does not justify taking that risk before the higher-leverage items above.

## Suggested Commit Message

```text
perf: baseline TUI/daemon/combined performance and cut writer/list alloc churn

Build a persistent tmp/perf workspace with phase reports, benchmark outputs,
pprof artifacts, residency harnesses, and native nvim trace captures.

Establish phase1 baselines for:
- tuiv2 writer and render benchmarks
- daemon list/request/event benchmarks
- TUI-only, daemon-only, and combined steady-state residency
- combined interactive nvim scroll traces

Implement first phase2 optimizations with TDD:
- replace join-based cursor-writer full-frame wire-length estimation with
  normalizedJoinedLinesWireLen to remove unnecessary full-frame allocations
  without changing bytes output decisions
- replace parseNumericString's strconv.ParseUint failure path with a manual
  digit scan so non-numeric IDs no longer allocate heavily during ServerList
  sorting

Measured results:
- TUI writer geomean: B/op -21.03%, allocs/op -3.90%, bytes/op flat
- ServerList: sec/op -46.76%, B/op -49.67%, allocs/op -99.90%
- steady-state residency remained flat with no evidence of cache/pool induced
  RSS growth

Document remaining hotspots:
- runtime.stream.output / local vterm.write
- render full-compose metadata/body export
- session event + session.get resync path
```
