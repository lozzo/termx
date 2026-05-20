# Phase 3: Residency / Lifetime Audit

## Goal

Identify long-lived objects, queue/cache capacity behavior, and ownership/lifetime boundaries so later optimizations do not simply trade heap allocations for larger resident memory.

## Commands

```bash
rg -n "sync\\.Pool|make\\(chan .*256|make\\(chan .*64|pendingVTermOutput|protocolListCache|listInfoCache|visibleCache|lastResult|bodyCache|queueLimit|ScrollbackLoadedLimit|ScrollbackLoadingLimit|time\\.AfterFunc|NewTicker|go func\\(" termx.go terminal.go event.go fanout/fanout.go protocol/client.go tuiv2/runtime/*.go tuiv2/render/coordinator.go tuiv2/app/cursor_writer.go tuiv2/app/run_session_forwarder.go
TERMX_RUN_TUI_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyTUI -count=1 -v
TERMX_RUN_DAEMON_RESIDENCY=1 go test . -run TestPerfResidencyDaemon -count=1 -v
TERMX_RUN_COMBINED_RESIDENCY=1 go test ./tuiv2/app -run TestPerfResidencyCombined -count=1 -v
```

## Key Results

- No obviously unbounded queue or cache was found on the main hot path:
  - `protocol.clientStream.queueLimit = 256`
  - `fanout` subscriber channel depth = `256`
  - `EventBus` subscriber channel depth = `64`
  - `Terminal.pendingVTermOutput` is bounded by `serverVTermFlushThreshold` and `protocol.MaxFrameSize`
  - `Server.protocolListCache` and `Terminal.listInfoCache` are single-entry mutation-invalidated caches
- The largest long-lived residents are still:
  - `runtime.TerminalRuntime.Snapshot`
  - `runtime.TerminalRuntime.VTerm`
  - `Runtime.visibleCache`
  - `render.Coordinator.lastResult` / `lastFrame` / `bodyCache`
- Post-GC / post-scavenge floor measurements stayed similar after phase2:
  - TUI-only RSS floor: `16.5-17.5MB` -> `16.2-16.8MB`
  - daemon-only RSS floor: `9.8MB` -> `9.9MB`
  - combined post-burst RSS floor was volatile (`56.6MB` baseline, `71.2MB` on the later rerun), and phase1 lacked an idle combined baseline, so this does not prove steady-state live RSS parity

## Residency Findings

- `protocol.Client.clientStream`
  - Owner: one per attached channel
  - Lifetime: channel attach -> stream close
  - Capacity strategy: queue cap `256`, queue slice cap `min(queueLimit, 16)` then grows up to limit
  - Risk: payload copies stay resident until drained, but growth is bounded and `close()` nils the queue
- `Fanout.subscriber.ch`
  - Owner: daemon `Terminal.stream`
  - Lifetime: subscription ctx -> close
  - Capacity strategy: fixed `256`
  - Risk: no unbounded growth; overflow converts to dropped-byte accounting
- `EventBus.eventSubscriber.ch`
  - Owner: daemon event bus
  - Lifetime: subscriber ctx -> close
  - Capacity strategy: fixed `64`
  - Risk: overflow drops events after logging rather than accumulating resident backlog
- `Terminal.pendingVTermOutput`
  - Owner: daemon `Terminal`
  - Lifetime: PTY read burst -> flush/epoch change
  - Capacity strategy: append until threshold or flush timer
  - Risk: bounded, but residual copy work still exists and remains a lower-priority daemon audit item
- `Runtime.visibleCache`
  - Owner: TUI runtime
  - Lifetime: until invalidated by `touch()`
  - Capacity strategy: exactly one cached visible projection
  - Risk: safe steady-state resident; not a leak
- `Coordinator.lastResult` / `bodyCache`
  - Owner: TUI render coordinator
  - Lifetime: until invalidate or reset
  - Capacity strategy: single cached frame/body
  - Risk: expected single-frame resident cost
- `TerminalRuntime.Snapshot` / `ScrollbackLoadedLimit`
  - Owner: TUI runtime registry entry
  - Lifetime: terminal attachment / snapshot load
  - Capacity strategy: monotonic toward the largest loaded scrollback window
  - Risk: main source of long-lived resident growth on the TUI side
- `presentedCellPool`
  - Owner: cursor writer
  - Lifetime: process-wide pool
  - Capacity strategy: pool only slices with cap `<= 2048`
  - Safety: `clear(cells)` before `Put`, returned as zero-length slice; no stale state found in current usage

## Risks

- Combined residency remains materially higher than TUI-only or daemon-only, so future optimizations must target snapshot/VTerm ownership and runtime stream buffering rather than adding more caches.
- The current residency helpers force `runtime.GC()` and `debug.FreeOSMemory()` before sampling, so they measure a post-scavenge floor, not a continuous-load steady-state plateau.
- `ScrollbackLoadedLimit` is intentionally monotonic, so heavy scrollback sessions can legitimately retain larger snapshots even after UI activity becomes idle.
- There are many goroutines, but they are lifecycle-bound:
  - per-subscriber cleanup goroutines in `event.go` / `fanout/fanout.go`
  - per-stream goroutines in `protocol/client.go` and `runtime/stream.go`
  - terminal timers in `terminal.go`
  - no long-lived `time.Ticker` loops were found in the daemon path

## Next Phase

Phase 4 should focus daemon effort on the remaining high-leverage areas that still affect long-run steady state: session event/resync churn, combined `runtime.stream.output`, and any queue retention that shows up once those paths are stressed.
