# Phase 1: Baseline

## Goal

Establish a reproducible baseline for TUI and daemon hot paths plus a first combined residency/interactive trace baseline, and save the raw artifacts under `tmp/perf/{baseline,current}`.

## Commands

```bash
./tmp/perf/scripts/ensure_benchstat.sh
./tmp/perf/scripts/run_phase1_baseline.sh
go tool pprof -top tmp/perf/pprof/phase1_tui_writer_cpu.prof
go tool pprof -top -sample_index=alloc_space tmp/perf/pprof/phase1_tui_writer_alloc.prof
go tool pprof -top tmp/perf/pprof/phase1_tui_render_cpu.prof
go tool pprof -top -sample_index=alloc_space tmp/perf/pprof/phase1_tui_render_alloc.prof
go tool pprof -top tmp/perf/pprof/phase1_daemon_cpu.prof
go tool pprof -top -sample_index=alloc_space tmp/perf/pprof/phase1_daemon_alloc.prof
```

Note:

- An earlier wide TUI sweep was aborted and preserved as `tmp/perf/bench/phase1_tui_before_aborted.txt`; it is not the canonical baseline.

## Key Results

- TUI writer baseline:
  - `partial_damage`: `125.6µs/op`, `2.153KiB bytes/op`, `71.83KiB B/op`, `51 allocs/op`
  - `full_damage`: `335.6µs/op`, `14.65KiB bytes/op`, `138.0KiB B/op`, `58 allocs/op`
  - `scroll_rows_enabled`: `227.4µs/op`, `176.5 bytes/op`, `40.78KiB B/op`, `102 allocs/op`
- TUI render baseline:
  - `FourPanesCached`: `3.998µs/op`, `33.61KiB B/op`, `56 allocs/op`
  - `FourPanesInvalidated`: `712.4µs/op`, `238.3KiB B/op`, `630 allocs/op`
  - `Overlap/one_floating`: `652.2µs/op`, `252.8KiB B/op`, `642 allocs/op`
  - `FloatingDragContentComplexity/styled_codex`: `793.0µs/op`, `405.7KiB B/op`, `725 allocs/op`
- Daemon baseline:
  - `ServerList`: `153.9µs/op`, `254.5KiB B/op`, `4048 allocs/op`
  - `ServerListParallel`: `48.04µs/op`, `128.1KiB B/op`, `4 allocs/op`
  - `ServerHandleRequestList`: `11.33ns/op`, `0 allocs/op` because the marshaled protocol list cache is hitting
- Residency baseline:
  - TUI-only: `single_pane_idle` RSS `16.5MB`; `side_by_side` RSS `16.8MB`; `floating_overlay` RSS `17.5MB`
  - daemon-only: `daemon_idle_startup` RSS `7.3MB`; `daemon_one_terminal_one_session` RSS `9.8MB`, heap alloc `5.22MB`, goroutines `8`
  - combined: only `combined_after_burst` was captured in phase1; there is no idle combined baseline in the phase1 artifact set
- Interactive combined `nvim` trace baseline:
  - `down_single first_output_ms`: `16.20`
  - `up_single first_output_ms`: `23.10`
  - `down_burst_8 first_output_ms`: `17.71`
  - `up_burst_8 first_output_ms`: `25.33`
  - `alternating_16`: `out=0`, `sync=0`, `first_output_ms=0.00`, `settle_ms=1027.28`; this action is excluded from the headline latency bullets because it did not produce a normal output burst in the raw trace

## Hotspots

- TUI writer alloc profile (`phase1_tui_writer_alloc.prof`) was dominated by:
  - `strings.(*Builder).WriteString`: `39.31%`
  - `internal/bytealg.MakeNoZero`: `41.54%`
  - `framePresenter.verticalScrollCandidate`: `8.89% flat / 42.99% cum`
  - `strings.Join`: `27.37% cum`
- TUI render alloc profile (`phase1_tui_render_alloc.prof`) was dominated by:
  - `composedCanvas.ownerMap`: `12.23%`
  - `clonePresentMetadata`: `11.79%`
  - `composeRenderMetadata`: `10.83%`
  - `cachedContentLines` / row serialization / `strings.Builder` growth
- Daemon alloc profile (`phase1_daemon_alloc.prof`) on `ServerList` was dominated by:
  - `Server.List`: `50.23%`
  - `strconv.syntaxError`: `36.74%`
  - `internal/stringslite.Clone`: `12.83%`
  - This exposed non-numeric ID parsing as a major fake-allocation hotspot in the list sort path.
- Not directly bench/profiled in phase1, but still carried forward from existing repo perf notes as likely next combined hotspots:
  - `runtime.stream.output`
  - local `vterm.write`
  - full-compose render path
  - session event + `session.get` resync path

## Risks

- `benchstat` on a single before file reports `±∞` CI notes; it is still useful here as a normalized baseline summary, but the real before/after statistical comparison starts in phase2.
- The TUI CPU profiles are partially dominated by scheduler/GC samples because the benchmark windows are short, so alloc profiles were more informative than raw CPU flat percentages in phase1.
- Combined `nvim` latency is noisy. The baseline values are hard data, but the phase1 report does not over-claim stability beyond the recorded numbers.
- Phase1 combined coverage is still incomplete:
  - no dedicated combined `pprof` artifact was captured in phase1
  - the combined residency artifact is post-burst only, not an idle/steady-state attached baseline

## Next Phase

Phase 2 will attack the first low-risk hot paths with TDD, focusing on TUI writer allocation churn and daemon list-path fake allocations, then rerun benchmarks, profiles, residency, and the interactive `nvim` harness.
