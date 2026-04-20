# Phase 0: Hotspot Map

## Goal

Build the initial TUI + daemon performance map before touching hot-path implementation code, and identify the first high-value candidates for TDD-driven optimization.

## Discovery Commands

```bash
git branch --show-current
git rev-parse HEAD
git status --short --untracked-files=all
rg -n "package main|func main\\(" cmd/termx cmd/termx-frame-audit .
rg -n "^func Benchmark" -g'*_test.go' .
rg -n "daemon|server|session|runtime|registry|snapshot|transport|sync|stream|broker|fanout|attach|detach" -g'*.go' .
rg -n "present|render|writer|metadata|Visible|cursor_writer|present_meta|projection|viewmodel|frame" tuiv2/app tuiv2/render tuiv2/runtime tuiv2/shared -g'*.go'
```

Repo state at mapping time:

- Branch: `master`
- HEAD: `d3947a1e55cd3bc70643134bdc6aaaf22de1eb24`
- Untracked files intentionally left alone: `termx-go-bundle.txt`, `termx_bugfix_clean.patch`

## Entry Points And Critical Paths

Daemon / server:

- `cmd/termx/main.go`: `daemonCommand()` calls `termx.NewServer(...).ListenAndServe(ctx)`.
- `termx.go`: `ListenAndServe()`, `handleTransport()`, `handleRequest()`, `handleSessionRequest()`, `protocolListResponse()`.
- `terminal.go`: `readLoop()`, `queuePendingVTermOutputLocked()`, `flushPendingVTermOutputLocked()`, `screenUpdatePayloadFromDamageLocked()`, `Snapshot()`, `protocolInfoJSON()`, `listInfoSnapshot()`.
- `fanout/fanout.go`: subscriber queueing, dropped-byte accounting, priority message displacement.
- `protocol/client.go`: client-side stream queueing/coalescing and request/response demux.
- `event.go` + `termx.go`: session/terminal event publication, filtering, and wire send path used by shared-workbench sync.

TUI / render / writer:

- `tuiv2/app/view.go`: render result -> cursor writer handoff via `WriteFrameLinesWithMeta`.
- `tuiv2/app/cursor_writer.go` and subfiles: frame diff/full repaint patch planning, row parsing, metadata cloning, scroll optimization.
- `tuiv2/app/present_meta.go`: render metadata conversion and visible rect extraction.
- `tuiv2/render/coordinator.go`: invalidation, cached frame reuse, body/status/tab composition.
- `tuiv2/render/body_canvas_render.go`: production full-compose body/canvas path.
- `tuiv2/render/pane_render_projection.go`: pane content signature and window projection.
- `tuiv2/render/snapshot_render_helpers.go`: terminal row draw path.
- `tuiv2/render/compositor.go`: row export and final string assembly.
- `tuiv2/render/pipeline.go` and `tuiv2/render/result.go`: metadata generation and cloned render results.
- `tuiv2/runtime/stream.go`: client frame batching, synchronized-output handling, stream reconnect.
- `tuiv2/runtime/snapshot.go`: snapshot cloning and VTerm hydration.
- `tuiv2/runtime/terminal_registry.go`: long-lived terminal runtime metadata and snapshot residency.
- `tuiv2/app/run_session_forwarder.go` + `tuiv2/app/update_session_messages.go`: session event subscription and `session.get` resync path.

## Existing Perf Notes To Respect

Existing repository perf notes already narrow the current hotspot order:

- `docs/tuiv2-render-perf-2026-04-09.md` says server lazy flush moved server-side `VTerm` off the main live-scroll hot path.
- The same doc and `tuiv2/app/perf_nvim_scroll_test.go` point current scroll bottlenecks toward `runtime.stream.output`, local `vterm.write`, and `render.frame`.
- `scripts/nvim-scroll-perf.sh` is the current end-to-end interactive harness and should remain part of phase1+ measurement flow.

## Existing Benchmark Surface

TUI:

- `tuiv2/app/cursor_writer_benchmark_test.go`
- `tuiv2/app/cursor_writer_patch_planner_matrix_test.go`
- `tuiv2/app/mouse_benchmark_test.go`
- `tuiv2/render/benchmark_test.go`

Daemon:

- `server_perf_test.go`
- `server_benchmark_test.go`
- `protocol/frame_benchmark_test.go`

Gap called out for Phase 1:

- No existing combined TUI + daemon benchmark matrix.
- No existing long-run residency harness for idle daemon / idle TUI / combined.
- No explicit daemon benchmarks for attach/detach, session snapshot/update fanout, or registry/metadata churn.

## Code-Level Hotspot Inventory

High-frequency functions:

- `(*framePresenter).presentLines()` and `planFramePatch()` in `tuiv2/app/cursor_writer.go` and `tuiv2/app/cursor_writer_patch_planner.go`
- `presentMetaFromRender()` and `clonePresentMeta()` in `tuiv2/app/present_meta.go`
- `(*Coordinator).RenderFrame()` and `renderResult()` in `tuiv2/render/coordinator.go`
- `renderBodyCanvas()`, `rebuildBodyCanvas()`, `drawTerminalSourceWithOffsetAndMetrics()`, and `(*composedCanvas).contentString()` in `tuiv2/render`
- `coalesceClientOutputFrames()` and `handleOutputFrame()` in `tuiv2/runtime/stream.go`
- `(*Server).handleRequest()` and `(*Server).handleSessionRequest()` in `termx.go`
- `(*Server).handleEventsRequest()` in `termx.go` and `(*EventBus).Publish()` in `event.go`
- `(*Terminal).screenUpdatePayloadFromDamageLocked()` in `terminal.go`
- `(*clientStream).send()` / `enqueueOutputLocked()` in `protocol/client.go`
- `(*Fanout).BroadcastMessage()` in `fanout/fanout.go`

Large object structures:

- `protocol.Snapshot` and nested screen/scrollback rows
- `protocol.ScreenUpdate` payloads with changed rows + scrollback appends
- `render.PresentMetadata` and `app.presentMeta` owner maps
- `runtime.TerminalRuntime` because it retains snapshot, VTerm, stream state, recovery state, and metadata
- `Terminal` because it retains VTerm, PTY, pending output buffers, attachment map, caches, and fanout

Likely repeated clone / copy paths:

- `render.PresentMetadata` -> `app.presentMeta` conversion (`presentMetaFromRender`)
- `clonePresentMeta()` on most cursor-writer state transitions
- `strings.Join(lines, "\n")` in cursor-writer full repaint and render result framing
- output frame copies in `protocol/client.go` and `terminal.go`
- final framebuffer string export in `tuiv2/render/compositor.go`
- screen/snapshot conversion in `terminal.go`, `tuiv2/runtime/snapshot.go`
- `sessionSnapshotFromState()` cloning workbench docs on session requests
- event JSON encode/send and `session.get` follow-up snapshots on shared-session updates
- `protocolListResponse()` JSON rebuild on cache miss

Long-lived / steady-state residents:

- `runtime.TerminalRegistry.terminals`
- `runtime.TerminalRuntime.Snapshot` and `VTerm`
- `Terminal.pendingVTermOutput`, cached metadata JSON, attachment map, and fanout subscribers
- `protocol.Client.streams`, per-channel queues, pending/reused frame maps
- `Coordinator.lastResult`, `lastFrame`, `bodyCache`
- workbench session state in `workbenchsvc.Service`

## Suspicion Table

| ID | Path | Suspicion | How To Measure | Expected Benefit |
| --- | --- | --- | --- | --- |
| PERF-LINK-001 | `tuiv2/runtime/stream.go` + `protocol/client.go` | current perf notes point to `runtime.stream.output`, local `vterm.write`, and client batching as the live-scroll bottleneck after server lazy flush | native `nvim` harness, targeted stream bench/profile, combined residency harness | Lower first-output latency, CPU, and allocs in combined TUI+daemon streaming |
| PERF-TUI-001 | `tuiv2/render/body_canvas_render.go`, `pane_render_projection.go`, `snapshot_render_helpers.go`, `compositor.go` | production render still fully composes body, redraws terminal rows, and exports final strings every frame | render benchmarks + CPU/alloc pprof + interactive `nvim` trace | Lower render CPU and B/op without regressing bytes optimization |
| PERF-TUI-002 | `tuiv2/app/present_meta.go` + `tuiv2/app/cursor_writer.go` | owner-map conversion and `presentMeta` cloning are O(width*height) and likely amplified in owner-aware patch planner scenarios | targeted cursor-writer meta bench + patch planner matrix sample | Lower allocs/op on owner-aware diff paths |
| PERF-DAEMON-001 | `termx.go`, `event.go`, `tuiv2/app/run_session_forwarder.go`, `tuiv2/app/update_session_messages.go` | shared-session updates publish events and often trigger follow-up `session.get`, so steady-state multi-client sync may pay both event + snapshot costs | add session event/session.get benchmarks and profiles | Lower CPU/allocs on shared workbench sync |
| PERF-DAEMON-002 | `terminal.go` | residual copies in pending output flush and screen-update payload building may still matter, but they are no longer assumed to be the primary live-scroll bottleneck | targeted daemon ingest benchmark + alloc profile | Medium CPU/B/op improvement with lower confidence |
| PERF-DAEMON-003 | `fanout/fanout.go` | per-subscriber queues, dropped-byte accounting, and priority displacement can create steady-state residency after bursts | residency harness + goroutine profile + queue audit | Lower steady-state memory and lower goroutine risk |

## First Optimization Candidates

1. `PERF-LINK-001`: highest-priority combined hotspot according to existing repo perf notes.
2. `PERF-TUI-001`: production render full-compose path must stay in the main matrix, not just cursor-writer diffs.
3. `PERF-DAEMON-001`: shared-session event and resync path needs explicit daemon coverage.
4. `PERF-TUI-002`: good first low-risk alloc optimization once the narrowed baseline is in place.

## Risks And Constraints

- Do not regress prior bytes wins from final-frame patch planner or global diff optimizer; all TUI changes must record `bytes/op` alongside CPU and allocs.
- Avoid reintroducing render-layer scattered incremental caches or sprite/damage cache systems.
- Any pooling/reuse must prove no stale state, no aliasing, and no cross-frame or cross-request contamination.
- RSS can be noisy on Go workloads; if unstable, phase reports will separate RSS observations from heap/inuse/object data instead of over-claiming.

## Next Phase

Phase 1 will build a narrowed, reproducible baseline matrix, capture CPU/alloc/heap profiles, run the existing `nvim` trace harness for combined behavior, and add missing residency/combined harness coverage where the repo still has gaps.
