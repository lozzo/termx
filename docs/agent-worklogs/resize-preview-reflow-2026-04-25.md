# Resize Preview Reflow Worklog

## Requirement Summary

Implement pane resize preview reflow for `termx` `tuiv2`.

When a pane is resized, the client layout can change before the real PTY/application receives resize/SIGWINCH and emits a redraw. During that gap, `termx` should render a local temporary preview derived from a captured preview source rather than exposing a lossy live vterm intermediate state.

Expected behavior:

- Non-alt-screen terminal pages reflow ordinary terminal text to the current pane width during preview.
- Alt-screen/fullscreen pages do not text-reflow; they preserve 2D grid semantics with crop on shrink and restore on expand.
- Continuous shrink/expand operations regenerate preview from the original preview source captured when entering preview, not from previous provisional previews.
- Real PTY/application output exits preview and shows the real terminal state.
- Resize echo, viewport-only, cursor-only, metadata-only, or layout-only updates must not exit preview.
- Render / Visible / projection paths must stay pure read-only.
- Screen update / snapshot / bootstrap transport must remain binary encoded.

## Current Branch

`feature/tuiv2-resize-preview-reflow`

## TODO

- [x] 1. Create branch and worklog
- [x] 2. Inspect existing resize pipeline
- [x] 3. Study tmux / terminal resize behavior
- [x] 4. Reproduce current missing capability in tmux
- [x] 5. Design preview source and lifecycle
- [x] 6. Implement non-alt-screen reflow preview
- [x] 7. Implement alt-screen crop/restore preview
- [x] 8. Implement preview exit on real output
- [x] 9. Add runtime tests
- [x] 10. Add render tests
- [x] 11. Validate with tmux capture
- [x] 12. Run final tests/build
- [x] 13. Write final summary

## Phase Log

### 1. Create branch and worklog

Commands:

```sh
git status --short
git branch --show-current
git switch -c feature/tuiv2-resize-preview-reflow || git switch -c feature/tuiv2-resize-preview-reflow-$(date +%Y%m%d)
mkdir -p docs/agent-worklogs
cat > docs/agent-worklogs/resize-preview-reflow-2026-04-25.md
```

Results:

- Started from `master`.
- Created branch `feature/tuiv2-resize-preview-reflow`.
- Created this worklog at `docs/agent-worklogs/resize-preview-reflow-2026-04-25.md`.

Capture files:

- None yet.

Commit:

- `c630104` Document resize preview reflow requirements and staged workflow

## Investigation Notes

### 2. Inspect existing resize pipeline

Commands:

```sh
rg --files | rg '(^|/)(AGENTS\.md|tuiv2/runtime/resize\.go|tuiv2/runtime/stream\.go|tuiv2/runtime/screen_update_contract\.go|tuiv2/runtime/terminal_registry\.go|tuiv2/runtime/snapshot\.go|tuiv2/app/layout_resize_service\.go|tuiv2/app/terminal_interaction_service\.go|tuiv2/render/pane_render_projection\.go|tuiv2/render/body_canvas_render\.go|_tmux-src/(screen|grid)\.c|vterm|third_party/github.com/charmbracelet/x/vt)'
sed -n '1,240p' tuiv2/runtime/resize.go
sed -n '1,260p' tuiv2/runtime/stream.go
sed -n '1,260p' tuiv2/runtime/snapshot.go
sed -n '1,260p' tuiv2/runtime/screen_update_contract.go
rg -n "PreferSnapshot|PendingOwnerResize|BootstrapPending|ScreenUpdate|HasContentChange|Resize\(|Size\(|IsAlternateScreen|snapshotFromVTerm|applyScreenUpdateSnapshot|Visible" tuiv2/runtime tuiv2/render tuiv2/app vterm third_party/github.com/charmbracelet/x/vt _tmux-src/screen.c _tmux-src/grid.c
```

Results:

- `tuiv2/runtime/resize.go` currently owns local resize coordination through `Runtime.ResizePane`.
- `ResizePane` sends `client.Resize`, then resizes local `VTerm`, bumps surface version, and refreshes snapshot.
- Existing local preview is shrink-only: `shouldPreferSnapshotDuringLocalShrink` returns true only when new cols/rows are less than or equal to old cols/rows and at least one dimension shrinks.
- Existing `provisionalSnapshotForLocalShrink` only clones the previous snapshot, changes `Size`, and hides/clamps cursor. It does not reflow non-alt-screen rows and does not preserve a first-resize preview source across later expand/shrink operations.
- `TerminalRuntime.PreferSnapshot` makes render prefer snapshot over live vterm via `visibleSurface`; this is a good existing boundary for preview rendering.
- `applyDecodedScreenUpdateContract` classifies updates with `protocol.ScreenUpdateClassification.HasContentChange` and routes placeholder/noop versus delta/full replace. This is the likely place to exit preview on real content screen updates without mutating render/projection paths.
- `applyScreenUpdateContract` applies decoded real screen state to `terminal.Snapshot` and local `VTerm`, then sets `PreferSnapshot = false`. This already exits the older shrink-only snapshot preference when a real update is applied.
- `snapshotFromVTerm` and `snapshotFromRowSource` can capture screen, scrollback, cursor, modes, row timestamps, and row kinds. This is the likely preview source capture mechanism.
- `vterm.VTerm.Resize` delegates to `charmbracelet/x/vt` emulator resize. Relying on the live vterm after resize is unsafe for this feature because the requirement says the preview must not expose lossy intermediate emulator state.
- Render paths such as `tuiv2/render/pane_render_projection.go` and `tuiv2/render/body_canvas_render.go` consume visible runtime snapshots/surfaces. No design should add mutation there.
- `tuiv2/app/layout_resize_service.go` tracks pending pane resizes and calls runtime resize, but preview source lifecycle should stay in runtime or a service boundary rather than render/projection.

### 3. Study tmux / terminal resize behavior

Commands:

```sh
rg -n "func screen_resize|grid_reflow|alternate|reflow" _tmux-src/screen.c _tmux-src/grid.c
sed -n '324,390p' _tmux-src/screen.c
sed -n '631,650p' _tmux-src/screen.c
sed -n '658,718p' _tmux-src/screen.c
sed -n '1431,1508p' _tmux-src/grid.c
```

Results:

- `_tmux-src/screen.c:screen_resize_cursor` only calls `screen_reflow` when width changes and reflow is enabled.
- `_tmux-src/screen.c:screen_reflow` calls `grid_reflow`, preserving cursor logical wrap position via `grid_wrap_position` / `grid_unwrap_position`.
- `_tmux-src/grid.c:grid_reflow` iterates history + visible grid lines. Lines wider than the new width split; previously wrapped lines can join into the next line when expanding.
- `_tmux-src/screen.c:screen_alternate_on` saves the normal grid and disables history for the alternate screen; alternate screen is a separate cursor-positioned grid.
- `_tmux-src/screen.c:screen_alternate_off` restores saved normal grid and resizes with reflow disabled for the alternate restore path.
- Implementation implication: non-alt preview can be a local reflow of captured logical text rows; alt-screen preview should crop/restore the captured 2D grid without ordinary text wrapping.

Capture files:

- None in this stage.

Commit:

- Pending investigation commit.


## Tmux Reproduction Notes

### 4. Reproduce current missing capability in tmux

Commands:

```sh
go build -o ./termx ./cmd/termx
SESSION=termx-resize-reflow
(tmux kill-session -t "$SESSION" 2>/dev/null || true)
tmux new-session -d -s "$SESSION" -x 100 -y 30 'cd /Users/lozzow/Documents/workdir/termx && ./termx'
sleep 2
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-start.txt
tmux send-keys -t "$SESSION:0.0" "clear; printf 'COL_A                 COL_B                 COL_C\n'; cat" Enter
sleep 1
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-before.txt
tmux resize-window -t "$SESSION:0" -x 50 -y 20
sleep 0.5
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-shrink.txt
tmux resize-window -t "$SESSION:0" -x 100 -y 30
sleep 0.5
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-expand.txt
rg -n "COL_A|COL_B|COL_C|COL_" /tmp/termx-reflow-before.txt /tmp/termx-reflow-shrink.txt /tmp/termx-reflow-expand.txt
```

Results:

- Build of current branch binary succeeded before reproduction.
- `tmux` session: `termx-resize-reflow`.
- Initial capture showed the termx UI and a terminal pane.
- Scenario A input reached the inner shell: before capture contains `COL_A                 COL_B                 COL_C`.
- After shrinking the outer tmux window from `100x30` to `50x20`, capture does not contain `COL_A`, `COL_B`, or `COL_C`; visible pane content is stale earlier directory listing rows with dotted clipped fill. This demonstrates the current preview is not generated from the requested hard-column source in a useful reflow form during this timing window.
- After expanding back to `100x30`, capture contains `COL_A                 COL_B                 COL_`; `COL_C` is truncated/lost. This demonstrates expand is derived from a lossy intermediate surface or clipped snapshot rather than the original preview source.

Capture files:

- `/tmp/termx-reflow-start.txt`
- `/tmp/termx-reflow-before.txt`
- `/tmp/termx-reflow-shrink.txt`
- `/tmp/termx-reflow-expand.txt`

Observed evidence:

```text
/tmp/termx-reflow-before.txt: COL_A                 COL_B                 COL_C
/tmp/termx-reflow-shrink.txt: no COL_A/COL_B/COL_C match
/tmp/termx-reflow-expand.txt: COL_A                 COL_B                 COL_
```

Commit:

- Pending tmux reproduction commit.


## Design Notes

### 5. Design preview source and lifecycle

Design decision after code and tmux investigation:

- Keep preview lifecycle in `tuiv2/runtime`; do not mutate render, Visible, or projection paths.
- Extend `TerminalRuntime` with a runtime-only preview source field that stores a cloned `protocol.Snapshot` captured when entering resize preview.
- Capture preview source before resizing the live vterm. Prefer current `terminal.Snapshot` if available because it is the last renderable authoritative surface; otherwise use `snapshotFromVTerm`.
- While preview is active, every resize regenerates `terminal.Snapshot` from the same saved source and sets `PreferSnapshot = true`; it must not use the previous provisional snapshot as the new source.
- Non-alt-screen generation should reflow captured rows to the requested cols and visible rows. Initial implementation can use cell-preserving row wrapping with blank trimming, preserving styles per cell and wide-cell width boundaries as far as current `protocol.Cell.Width` allows.
- Alt-screen generation should not text-reflow; it should clone/crop the original 2D screen grid into the requested size so expand can restore cells from the source.
- Apply real decoded content updates through existing `applyScreenUpdateContract`, then clear resize preview source and set `PreferSnapshot = false`.
- Noop/placeholder screen updates should not clear preview; this follows the existing classification path because `screenUpdateLifecycleNoop` still applies state but has no content change and placeholder returns before apply.
- Stream PTY output that produces local vterm writes should clear preview when actual bytes arrive before refresh/invalidate; this prevents a stuck old preview over real shell/app output.
- `terminalAlreadySized` must not treat a provisional preview snapshot as proof that the live terminal is already sized; otherwise expand after shrink can skip resize and preserve truncation.

Commands:

```sh
# Design based on previous rg/sed investigation and tmux captures.
```

Results:

- Chosen model uses runtime-owned `ResizePreviewSource` plus snapshot generation functions.
- Render remains a pure consumer of `terminal.Snapshot` / `PreferSnapshot` state.
- Transport remains unchanged.

Capture files:

- Design uses prior `/tmp/termx-reflow-*.txt` artifacts.

Commit:

- Pending design commit.


## Implementation Notes

### 8. Implement preview exit on real output

Commands:

```sh
apply_patch # update TerminalRuntime, resize, stream, screen update, transaction restore
gofmt -w tuiv2/runtime/terminal_registry.go tuiv2/runtime/resize.go tuiv2/runtime/stream.go tuiv2/runtime/screen_update_contract.go tuiv2/runtime/transaction_restore.go
mkdir -p .cache/go-build
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime
```

Results:

- Added `TerminalRuntime.ResizePreviewSource` as a runtime-owned clone of the first resize preview source.
- `ResizePane` now captures the source before live vterm resize and regenerates provisional snapshots from that source for any size change, not only local shrink.
- Real output frames clear `ResizePreviewSource` before invalidating live output.
- Contentful decoded screen updates (`delta` or `full replace`) clear `ResizePreviewSource`; placeholder/noop updates do not.
- Resize echo frames regenerate a provisional snapshot from `ResizePreviewSource` when preview is active instead of merely changing snapshot size.
- `terminalAlreadySized` no longer treats provisional preview snapshot geometry as sufficient to skip live resize.
- Transaction clone helpers now reuse existing protocol snapshot clone helpers.
- Validation note: `GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime` reached test execution but failed in pre-existing socket-dependent tests:
  - `TestRuntimeListTerminalsDoesNotPopulateRegistry`: dial unix `/tmp/termx-bfe9a084198bde98.sock`: no such file or directory
  - `TestRuntimeAttachSnapshotInputAndResize`: dial unix `/tmp/termx-012593f900a71b78.sock`: no such file or directory

Capture files:

- None in this stage.

Commit:

- Pending runtime lifecycle commit.


### 6-7, 9. Implement preview generation and runtime tests

Commands:

```sh
cat > tuiv2/runtime/resize_preview_test.go
gofmt -w tuiv2/runtime/resize_preview_test.go
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreview|TestRuntimeResizePaneShrinkKeepsRenderOnSnapshotUntilOutput'
```

Results:

- Added runtime tests for non-alt-screen hard-column shrink reflow.
- Added runtime test for shrink→expand restoration from the original source.
- Added runtime test for alt-screen crop on shrink and source-grid restore on expand.
- Added lifecycle tests proving real output and content screen updates clear `ResizePreviewSource`.
- Added lifecycle test proving noop screen updates do not clear `ResizePreviewSource`.
- Targeted runtime test command passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.462s`

Capture files:

- None in this stage.

Commit:

- Pending runtime tests commit.

### Runtime preview blank-tail correction

Commands:

```sh
apply_patch # trim trailing blank source rows before non-alt preview reflow
gofmt -w tuiv2/runtime/resize.go tuiv2/runtime/resize_preview_test.go
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreview'
```

Results:

- Tmux validation showed the first implementation could render blank shrink/expand previews when the source screen had many blank rows below the command output.
- Root cause: non-alt preview reflow used all screen rows then selected the last viewport-height rows; trailing blank rows pushed meaningful command output into provisional scrollback.
- Fixed by trimming trailing blank preview source rows before reflow/screen-window selection.
- Added runtime test coverage where the source has many trailing blank screen rows.
- Targeted runtime tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.542s`

Capture files:

- Failed intermediate validation artifacts:
  - `/tmp/termx-reflow-final-before.txt`
  - `/tmp/termx-reflow-final-shrink.txt`
  - `/tmp/termx-reflow-final-expand.txt`
  - `/tmp/termx-reflow-final-real-output.txt`

Commit:

- Pending blank-tail correction commit.

### Runtime preview source freshness correction

Commands:

```sh
apply_patch # prefer live vterm when surface version is newer than snapshot version
gofmt -w tuiv2/runtime/resize.go
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreview'
```

Results:

- Tmux validation still showed expand restoring a stale clipped snapshot after shell output.
- Root cause: `captureResizePreviewSource` preferred `terminal.Snapshot` even when real output had advanced the live surface without refreshing the snapshot.
- Fixed by capturing from live vterm when `SurfaceVersion > SnapshotVersion` and `PreferSnapshot` is false.
- Targeted runtime tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.328s`

Commit:

- Pending source freshness correction commit.

### Runtime preview word-boundary reflow correction

Commands:

```sh
apply_patch # prefer whitespace cut points for non-alt preview reflow
gofmt -w tuiv2/runtime/resize.go
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreview'
```

Results:

- Tmux validation after source freshness fix showed shrink preview contained `COL_A`/`COL_B` but clipped `COL_C` to `COL_` at the pane edge.
- Fixed non-alt preview wrapping to prefer whitespace cut points and trim leading/trailing segment spaces, preserving hard-column tokens during shrink.
- Targeted runtime tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.527s`

Commit:

- Pending word-boundary reflow correction commit.

## Test and Validation Notes

### 10. Add render tests

Commands:

```sh
cat > tuiv2/render/resize_preview_test.go
gofmt -w tuiv2/render/resize_preview_test.go
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/render -run 'TestRenderPipeline.*ResizePreview'
```

Results:

- Added render pipeline tests proving a non-alt resize preview snapshot displays reflowed rows (`COL_A`, `COL_B`, `COL_C`).
- Added render pipeline test proving expanded preview displays restored hard-column content.
- Added render pipeline test proving alt-screen cropped preview grid is displayed without render-layer text reflow.
- Targeted render tests passed:
  - `ok github.com/lozzow/termx/tuiv2/render 0.484s`
- No render/projection mutation was added; render tests consume prepared snapshots.

Capture files:

- None in this stage.

Commit:

- Pending render tests commit.


### 11. Validate with tmux capture

Commands:

```sh
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
SESSION=termx-resize-reflow-final-ok
SOCKET=/tmp/termx-resize-reflow-final-ok.sock
CONFIG_HOME=/tmp/termx-reflow-config-ok
STATE_HOME=/tmp/termx-reflow-state-ok
LOG=/tmp/termx-reflow-final-ok.log
(tmux kill-session -t "$SESSION" 2>/dev/null || true)
rm -rf "$CONFIG_HOME" "$STATE_HOME" "$SOCKET" "$LOG"
mkdir -p "$CONFIG_HOME" "$STATE_HOME"
tmux new-session -d -s "$SESSION" -x 100 -y 30 "cd /Users/lozzow/Documents/workdir/termx && XDG_CONFIG_HOME=$CONFIG_HOME XDG_STATE_HOME=$STATE_HOME ./termx --socket $SOCKET --log-file $LOG"
sleep 3
tmux send-keys -t "$SESSION:0.0" Enter
sleep 1
tmux send-keys -t "$SESSION:0.0" "resize-preview" Enter Enter
sleep 3
tmux send-keys -t "$SESSION:0.0" "clear; printf 'COL_A                 COL_B                 COL_C\n'; cat" Enter
sleep 1
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-final-ok-before.txt
tmux resize-window -t "$SESSION:0" -x 50 -y 20
sleep 0.2
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-final-ok-shrink.txt
tmux resize-window -t "$SESSION:0" -x 100 -y 30
sleep 0.2
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-final-ok-expand.txt
tmux send-keys -t "$SESSION:0.0" C-c
sleep 0.2
tmux send-keys -t "$SESSION:0.0" "printf 'AFTER_REAL_OUTPUT\n'" Enter
sleep 0.8
tmux capture-pane -t "$SESSION:0.0" -p -S -200 > /tmp/termx-reflow-final-ok-real-output.txt
rg -n "AFTER_REAL_OUTPUT|COL_A|COL_B|COL_C|COL_" /tmp/termx-reflow-final-ok-*.txt
```

Results:

- Final hard-column tmux validation used isolated XDG config/state and socket paths so no existing workspace state affected startup.
- Before capture contains full original row:
  - `/tmp/termx-reflow-final-ok-before.txt`: `COL_A                 COL_B                 COL_C`
- Shrink capture contains all columns after preview reflow:
  - `/tmp/termx-reflow-final-ok-shrink.txt`: line 3 contains `COL_A                 COL_B`
  - `/tmp/termx-reflow-final-ok-shrink.txt`: line 4 contains `COL_C`
- Expand capture restores original hard-column row:
  - `/tmp/termx-reflow-final-ok-expand.txt`: `COL_A                 COL_B                 COL_C`
- Real output capture shows app/shell output is not blocked by preview:
  - `/tmp/termx-reflow-final-ok-real-output.txt`: `AFTER_REAL_OUTPUT`
- Real-output capture still includes an older clipped `COL_` line from the live terminal state after interrupting `cat`; this is real terminal history after preview exit, not a stuck preview. The required signal is that `AFTER_REAL_OUTPUT` is visible and terminal behavior continues.

Capture files:

- `/tmp/termx-reflow-final-ok-before.txt`
- `/tmp/termx-reflow-final-ok-shrink.txt`
- `/tmp/termx-reflow-final-ok-expand.txt`
- `/tmp/termx-reflow-final-ok-real-output.txt`
- `/tmp/termx-reflow-final-ok.log`

Alt-screen validation:

- Automated tmux alt-screen interaction is more fragile because the clean startup flow requires modal creation steps before launching a fullscreen app.
- Alt-screen semantics are covered by Go runtime and render tests:
  - `TestResizePreviewAltScreenCropAndRestoreGrid`
  - `TestRenderPipelineKeepsAltResizePreviewCroppedGrid`

Commit:

- Pending tmux validation commit.

### 12. Run final tests/build

Commands:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
```

Results:

- Final required package tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.846s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.179s`
- Final required build passed:
  - `go build -o ./termx ./cmd/termx`

Capture files:

- No new capture files in this stage.

Commit:

- Pending final tests/build commit.

## Known Issues

- None yet.

## Next Step Suggestions

- Design runtime-owned preview source lifecycle.
- Implement non-alt reflow from original preview source.
- Implement alt-screen crop/restore from original preview source.
- Ensure real output exits preview while resize echo/noop does not.

## Final Summary

Completed pane resize preview reflow for `termx` `tuiv2` on branch `feature/tuiv2-resize-preview-reflow`.

Implemented behavior:

- Runtime captures `ResizePreviewSource` when entering local resize preview.
- Continuous resize regenerates provisional preview snapshots from the original source rather than the previous provisional snapshot.
- Non-alt-screen previews reflow rows to the requested width, trim trailing blank source rows, and prefer whitespace boundaries so hard-column tokens remain visible.
- Alt-screen previews crop/restore the captured 2D grid and do not text-reflow fullscreen layout rows.
- Real output frames and contentful screen updates clear `ResizePreviewSource` and release `PreferSnapshot` so real terminal state is shown.
- Resize echo/noop screen updates keep preview active and regenerate from the source when applicable.
- Render/projection paths remain pure consumers; no mutation was added to render/Visible/projection.
- Screen update/snapshot/bootstrap transport remains unchanged and binary protocol code was not modified.

Validation summary:

- Runtime tests cover non-alt shrink reflow, shrink→expand restore, real output clearing, noop update retention, content update clearing, and alt crop/restore.
- Render tests cover visible render pipeline consumption of non-alt reflow, expanded restore, and alt cropped grid snapshots.
- Final tmux capture validation proves hard-column shrink shows `COL_A`, `COL_B`, and `COL_C`; expand restores the original row; real output shows `AFTER_REAL_OUTPUT`.
- Final required commands passed:
  - `GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render`
  - `GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx`

Key commits:

- `c630104` Document resize preview reflow requirements and staged workflow
- `7fa7c38` Record resize pipeline and tmux reflow investigation
- `49c7271` Record tmux reproduction of missing resize preview reflow
- `a84d0d9` Document resize preview source lifecycle design
- `0fdf4ef` Add runtime resize preview source lifecycle
- `249f30a` Add runtime tests for resize preview reflow lifecycle
- `6944ef5` Add render tests for resize preview snapshots
- `fb10dfe` Keep non-alt resize preview content above trailing blanks
- `eabd631` Capture fresh live surface for resize preview source
- `d236691` Wrap non-alt resize preview at whitespace boundaries
- `04339c7` Record final tmux resize preview validation
- `de4dd77` Record final resize preview test and build results
- `e63c68d` Write final resize preview reflow worklog summary

## Resume From Here

Current status:

- Branch: `feature/tuiv2-resize-preview-reflow`
- Last completed TODO: `13. Write final summary`
- Last commit: pending follow-up app already-sized gate fix
- Next step: review branch or open a PR; no implementation work remains for this task.

Important artifacts:

- `docs/agent-worklogs/resize-preview-reflow-2026-04-25.md`
- `/tmp/termx-reflow-final-ok-before.txt`
- `/tmp/termx-reflow-final-ok-shrink.txt`
- `/tmp/termx-reflow-final-ok-expand.txt`
- `/tmp/termx-reflow-final-ok-real-output.txt`


## Follow-up: App Already-Sized Gate Fix

User feedback: the previous implementation had no visible effect in actual use.

Root cause found after re-checking the real resize path:

- Outer window resize enters `tuiv2/app/layout_resize_service.go` first.
- `terminalInteractionService.resizeIfNeeded` called `model.terminalAlreadySized` before invoking `runtime.ResizeTerminal`.
- `model.terminalAlreadySized` only compared `terminal.Snapshot.Size` with the requested size.
- During preview, the provisional snapshot is deliberately set to the requested size, while the live vterm/PTY may not have the same size yet.
- That meant app code could skip the runtime resize/preview path entirely on subsequent resize passes, making the feature appear ineffective.

Fix:

- Added `Runtime.TerminalAlreadySized`, delegating to runtime's preview-aware `terminalAlreadySized` helper.
- Updated `Model.terminalAlreadySized` to call runtime instead of directly trusting snapshot dimensions.
- Added `TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot` to cover this app-level gate.

Commands:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/app -run 'TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot'
```

Result:

- `ok github.com/lozzow/termx/tuiv2/app 0.549s`

Commit:

- Pending follow-up fix commit.
