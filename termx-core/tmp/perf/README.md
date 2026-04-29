# termx Perf Workspace

Current phase: `phase0_map`

Workspace layout:

- `tmp/perf/baseline/`: frozen baseline artifacts copied forward from the first clean measurement run for each phase.
- `tmp/perf/current/`: latest rerun artifacts after code changes in the active phase.
- `tmp/perf/reports/`: phase reports and final summary.
- `tmp/perf/pprof/`: raw `pprof` files (`cpu`, `alloc`, `heap`, `goroutine`, `mutex`, `block` when collected).
- `tmp/perf/bench/`: raw benchmark outputs and `benchstat` comparisons.
- `tmp/perf/scripts/`: reproducible harness helpers for bench/profile/residency runs.
- `tmp/perf/notes/`: checklist, optimization backlog, measurement notes, and audit records.
- `tmp/perf/bin/`: local helper binaries such as `benchstat`.

File naming:

- Raw benchmark output: `tmp/perf/bench/<phase>_<scope>_before.txt`
- Raw benchmark output: `tmp/perf/bench/<phase>_<scope>_after.txt`
- `benchstat`: `tmp/perf/bench/<phase>_<scope>_benchstat.txt`
- CPU profile: `tmp/perf/pprof/<phase>_<scope>_cpu.prof`
- Alloc profile: `tmp/perf/pprof/<phase>_<scope>_alloc.prof`
- Heap profile: `tmp/perf/pprof/<phase>_<scope>_heap.prof`
- Other profiles: `tmp/perf/pprof/<phase>_<scope>_<kind>.prof`
- Phase reports: `tmp/perf/reports/phase0_map.md` through `tmp/perf/reports/phase5_final.md`

Primary harness entrypoints:

- Install tooling: `./tmp/perf/scripts/ensure_benchstat.sh`
- Phase 1 baseline bench/profile sweep: `./tmp/perf/scripts/run_phase1_baseline.sh`
- Native interactive `nvim` trace harness: `./scripts/nvim-scroll-perf.sh <out-dir>`
- Later phases will add targeted scripts beside the phase reports and keep the same artifact naming.

Measurement policy:

- TUI, daemon, and combined runs must each save raw benchmark output, `benchstat`, and profiles when comparable data exists.
- Residency and long-run memory checks should save raw text output in `tmp/perf/current/` or `tmp/perf/baseline/` and link them from the phase report.
- Any `sync.Pool`, cache, or reuse optimization must record stale-state / aliasing / cross-frame contamination checks in the relevant phase report.
- Current `*_alloc.prof` and `*_heap.prof` files are the same Go memprofile viewed with different `pprof` sample indices (`alloc_space` vs `inuse_space`); they are not independent captures unless explicitly noted otherwise.

Latest data sources:

- Baseline source: pending `phase1`
- Current source: pending `phase1`

Notes:

- If a run is intentionally aborted because the matrix was too broad or noisy, preserve it with an `_aborted` suffix and do not treat it as the canonical baseline/current artifact.
- `docs/tuiv2-render-perf-2026-04-09.md` and `scripts/nvim-scroll-perf.sh` are first-class references for the current TUI/combined bottleneck map and should be kept in sync with new phase reports.
