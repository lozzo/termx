# Optimization Backlog

| ID | Title | Scope | Expected Benefit | Complexity | Risk | Type | Priority | Score | Implement | Drop Reason |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| PERF-LINK-001 | Reduce combined `runtime.stream.output` + local `vterm.write` + client batching cost on live output bursts | both | high | medium | medium | combo | P0 | 9 | pending | |
| PERF-TUI-002 | Reduce full-compose render body cost (`body_canvas_render`, window signature projection, row draw, compositor export) | TUI | high | medium | medium | combo | P0 | 9 | pending | |
| PERF-DAEMON-001 | Reduce session event publication / `session.get` resync / session snapshot marshal cost on shared-workbench updates | daemon | high | medium | medium | combo | P0 | 8 | pending | |
| PERF-TUI-001 | Reduce per-frame `presentMeta` clone / owner-map rebuild cost in cursor-writer path | TUI | medium | low | low | alloc | P1 | 7 | pending | |
| PERF-TUI-003 | Avoid unnecessary full-frame `strings.Join` churn when only estimating normalized wire length | TUI | medium | low | low | alloc | P1 | 6 | done | Replaced join-based full repaint length estimation with `normalizedJoinedLinesWireLen`; bytes/op stayed flat while B/op and allocs/op dropped |
| PERF-LINK-002 | Audit `protocol.Client` stream queue copies and steady-state queue residency | both | medium | medium | medium | residency | P1 | 6 | pending | |
| PERF-DAEMON-002 | Audit residual `Terminal` output flush copies and screen-update payload churn after lazy flush | daemon | medium | low | low | alloc | P2 | 5 | pending | |
| PERF-DAEMON-003 | Audit `Server` attach/detach and request-path temporary slices/maps | daemon | medium | low | low | alloc | P2 | 5 | pending | |
| PERF-DAEMON-004 | Avoid allocation-heavy non-numeric parse fallback in `ServerList` sort path | daemon | high | low | low | alloc | P1 | 8 | done | Replaced `strconv.ParseUint` failure path with manual digit scan; `ServerList` allocs/op dropped from 4048 to 4 |
| PERF-RES-001 | Add long-run residency harness for idle TUI / daemon / combined | both | high | medium | low | residency | P0 | 8 | done | Added `TestPerfResidencyTUI`, `TestPerfResidencyDaemon`, and `TestPerfResidencyCombined` and wired them into phase1 baseline |
| PERF-RES-002 | Audit queue/cache/pool steady-state capacity retention after burst load | both | high | medium | medium | residency | P1 | 7 | pending | |
| PERF-SAFETY-001 | Reject optimizations that trade heap allocs for unbounded resident caches | both | high | low | low | memory | guardrail | 10 | guardrail | Not an optimization item; permanent rule |

Notes:

- `Implement` values: `pending`, `active`, `done`, `dropped`.
- `Priority` is the execution order gate; `Score` is the numeric tie-breaker used when qualitative buckets are equal.
- If an item is dropped later, keep the row and fill `Drop Reason` with the measured reason.
