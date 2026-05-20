# Phase 4: Daemon Deep Optimization

## Goal

Validate daemon-specific hotspots after the phase2 fix, quantify the remaining real daemon work, and explicitly defer low-leverage or risky changes.

## Commands

```bash
go test . -run '^$' -bench 'Benchmark(ServerHandleRequestList|ServerList|ServerGet|EventBusPublish64Subscribers)' -benchmem -count=5
go tool pprof -top tmp/perf/pprof/phase2_daemon_cpu.prof
go tool pprof -top -sample_index=alloc_space tmp/perf/pprof/phase2_daemon_alloc.prof
TERMX_RUN_DAEMON_RESIDENCY=1 go test . -run TestPerfResidencyDaemon -count=1 -v
TERMX_RUN_COMBINED_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyCombined -count=1 -v
```

## Key Results

- The daemon list-path fix is the largest measured daemon win so far:
  - `ServerList`: `153.9µs -> 81.9µs`, `254.5KiB -> 128.1KiB`, `4048 allocs -> 4 allocs`
  - `ServerListParallel`: `48.04µs -> 45.47µs`
- `ServerHandleRequestList` stays effectively free because the protocol list cache is already working; it is not a useful next optimization target.
- After the fix, daemon alloc profile is now dominated by legitimate `Server.List` output construction rather than error-object churn from `strconv.ParseUint`.

## Optimizations

- Implemented:
  - `PERF-DAEMON-004`: non-numeric parse fallback removal in `parseNumericString`
- Explicitly deferred:
  - `PERF-DAEMON-002` residual terminal flush copies
    - Reason: current repo perf notes and phase1/phase2 data show this is no longer the primary live-scroll bottleneck; changing it now would add risk without measured evidence of top-line payoff
  - `PERF-DAEMON-001` session event / `session.get` resync optimization
    - Reason: this still looks important, but phase4 did not yet have a focused benchmark or profile isolating the shared-session path; optimizing blindly would be lower rigor than the current bar

## Risks

- `EventBus` and `fanout` use fixed-depth channels and drop-on-overflow policies. This prevents unbounded residency but means sustained slow consumers can still lose events or output summaries.
- `handleTransport` and attachment streaming still spin goroutines per connection/channel. The lifetime audit did not find a leak, but client count still scales goroutine count.
- `Terminal.pendingVTermOutput` still copies once per flush. This remains a legitimate cleanup candidate, just not a verified top-priority one after current measurements.

## Next Phase

Phase 5 should summarize the completed TUI/daemon wins, the stable residency picture, and the remaining major work: render full-compose metadata/body export, `runtime.stream.output`, and session event/resync churn.

