# Perf Checklist

Repo state lock:

- [x] Confirm branch: `master`
- [x] Confirm HEAD: `d3947a1e55cd3bc70643134bdc6aaaf22de1eb24`
- [x] Confirm worktree status
- [x] Confirm only untracked files to avoid committing: `termx-go-bundle.txt`, `termx_bugfix_clean.patch`

Phase 0:

- [x] Create `tmp/perf` workspace
- [x] Write workspace README with naming rules and runbook
- [x] Create phase report framework
- [x] Create optimization backlog with scoring
- [x] Build TUI + daemon hotspot map
- [x] Save hotspot map to `tmp/perf/reports/phase0_map.md`
- [x] Phase 0 sub-agent review

Phase 1:

- [x] Install or provision `benchstat`
- [x] Build baseline matrix for TUI
- [x] Build baseline matrix for daemon
- [ ] Build baseline matrix for combined TUI + daemon
- [x] Save raw benchmark outputs
- [x] Save CPU / alloc / heap profiles
- [ ] Save combined baseline pprof artifact
- [x] Save steady-state / residency baseline outputs
- [x] Write `tmp/perf/reports/phase1_baseline.md`
- [x] Phase 1 sub-agent review

Open corrections from phase0 review:

- [x] Re-rank hotspots to reflect current repo perf notes (`runtime.stream.output`, local `vterm.write`, `render.frame`) instead of over-prioritizing server lazy flush
- [x] Add full-compose body/canvas/window-signature/export render path to hotspot map
- [x] Add session event publication / `session.get` resync path to hotspot map and backlog
- [x] Replace the aborted broad phase1 sweep with a narrower reproducible matrix
- [x] Populate `baseline/` and `current/` as canonical phase1 sources

Phase 2:

- [x] Pick first hot-path optimization from backlog with TDD
- [x] Add failing or gap-exposing test/benchmark first
- [x] Implement optimization
- [x] Re-run benchmarks, profiles, and bytes checks
- [x] Write `tmp/perf/reports/phase2_hotpath_allocs.md`
- [x] Phase 2 sub-agent review

Phase 3:

- [x] Audit long-lived objects and capacity growth
- [x] Add residency / lifetime harness coverage where missing
- [x] Validate no stale pooled state or aliasing
- [x] Write `tmp/perf/reports/phase3_residency_lifetime.md`
- [ ] Phase 3 sub-agent review

Phase 4:

- [x] Deep-optimize daemon hot paths
- [x] Audit goroutines / queues / fanout / sync / metadata churn
- [x] Re-run daemon and combined measurements
- [x] Write `tmp/perf/reports/phase4_daemon.md`
- [ ] Phase 4 sub-agent review

Phase 5:

- [x] Re-run key benchmark matrix
- [x] Re-run residency and steady-state harnesses
- [x] Produce final comparison tables
- [x] Summarize remaining hotspots and risks
- [x] Write `tmp/perf/reports/phase5_final.md`
- [ ] Phase 5 sub-agent review
