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
- Last commit: pending repeated cat/ls viewport anchor fix commit
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


### Follow-up tmux validation after app gate fix

Commands:

```sh
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
# tmux session: termx-resize-reflow-userfix
# isolated paths:
#   socket: /tmp/termx-resize-reflow-userfix.sock
#   config: /tmp/termx-reflow-userfix-config
#   state: /tmp/termx-reflow-userfix-state
#   log: /tmp/termx-reflow-userfix.log
# captures:
#   /tmp/termx-reflow-userfix-before.txt
#   /tmp/termx-reflow-userfix-shrink.txt
#   /tmp/termx-reflow-userfix-expand.txt
#   /tmp/termx-reflow-userfix-real-output.txt
```

Results:

- Before capture contains original hard-column row:
  - line 3: `COL_A                 COL_B                 COL_C`
- Shrink capture now shows all columns via preview reflow:
  - line 3: `COL_A                 COL_B`
  - line 4: `COL_C`
- Expand capture restores original hard-column row:
  - line 3: `COL_A                 COL_B                 COL_C`
- Real output capture confirms terminal output continues after preview:
  - line 7: `AFTER_REAL_OUTPUT`

Conclusion:

- The user-visible no-effect issue was caused by the app-level already-sized gate, not the reflow helper itself.
- After commit `056dbbb`, real tmux output confirms the feature is visible.


### Follow-up final validation commands

Commands:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render ./tuiv2/app -run 'TestResizePreview|TestRenderPipeline.*ResizePreview|TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot'
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
```

Results:

- Targeted runtime/render/app tests passed.
- Required runtime/render package tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.664s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.211s`
- Required build passed:
  - `go build -o ./termx ./cmd/termx`

Commit:

- `f26d6ba` Record follow-up final validation after resize gate fix


## Follow-up: Real `ls` Output Reflow

User feedback: hard-column `printf` validation worked, but real `ls` output did not reorder/reflow.

Reproduction:

- tmux session: `termx-resize-real-ls`
- command inside termx shell: `clear; command ls`
- initial size: `120x32`
- shrink size: `55x20`
- captures:
  - `/tmp/termx-real-ls-before.txt`
  - `/tmp/termx-real-ls-shrink.txt`
  - `/tmp/termx-real-ls-expand.txt`

Observed before fix:

- Before capture showed real `ls` rows with right-side columns such as `server_contract_test.go`, `termx_test.go`, `third_party`, `transport`, `tuiv2`, and `vterm`.
- Shrink capture showed right-side entries clipped to prefixes like `serve`, `snaps`, `strea`, and `termi`.
- The synthetic `printf` ls-like case passed because `cat` kept preview visible; real `ls` returned to shell and prompt output caused preview to exit quickly, exposing the live vterm resized/cropped state.

Fix:

- During resize preview, after generating the provisional reflow/crop snapshot, load that snapshot back into the local vterm via `loadSnapshotIntoVTerm` instead of calling raw `vt.Resize`.
- This makes the reflowed non-alt preview become the local vterm base for subsequent real shell/prompt output, so preview exit no longer reveals a cropped live vterm.
- Alt-screen behavior still uses crop/restore snapshot semantics and loads the cropped grid as the local base until real app redraw arrives.

Validation after fix:

- tmux session: `termx-resize-real-ls3`
- captures:
  - `/tmp/termx-real-ls3-before.txt`
  - `/tmp/termx-real-ls3-shrink.txt`
  - `/tmp/termx-real-ls3-expand.txt`
- Shrink capture no longer shows the old clipped prefixes in the same way; it shows reflowed entries as whole tokens, for example:
  - `transport_slow_consumer_test.go`
  - `terminal.go                      tuiv2`
  - `terminalmeta                     vterm`
- Targeted tests/build after the fix passed:
  - `GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render ./tuiv2/app -run 'TestResizePreview|TestRuntimeResizePane|TestRenderPipeline.*ResizePreview|TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot'`
  - `GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx`

Commit:

- Pending real ls reflow fix commit.


## Follow-up: Real `ls` Shrink Content and Expand Restore

User feedback: real `ls` shrink content was still insufficient, and shrink→expand did not fully restore.

Reproduction after previous fix:

- `/tmp/termx-real-ls3-shrink.txt` showed only the tail of the reflowed listing, starting around `transport_slow_consumer_test.go`.
- `/tmp/termx-real-ls3-expand.txt` did not fully restore the original wide layout until later lifecycle changes.

Root causes:

- `handleOutputFrame` and content screen updates cleared `ResizePreviewSource` immediately. Real shell prompt output after `ls` arrived during the resize burst and destroyed the original wide `ls` source before expand.
- Releasing `PreferSnapshot` during that output let prompt output reveal the local vterm state and scroll the reflowed preview toward the tail.
- The non-alt preview viewport selection took the bottom of reflowed rows, which hid the top of the listing during shrink.

Fixes:

- Keep `ResizePreviewSource` across real output/screen updates during a resize burst.
- Keep `PreferSnapshot` while `ResizePreviewSource` is active, so resize echo/prompt output does not immediately displace the preview.
- Clear `ResizePreviewSource` and release `PreferSnapshot` on the next real user input via `SendInput`, then invalidate runtime visible cache.
- For non-alt reflow preview, select the beginning of the reflowed rows rather than the bottom when the reflowed content exceeds viewport height.

Validation:

- tmux session: `termx-resize-real-ls6`
- captures:
  - `/tmp/termx-real-ls6-before.txt`
  - `/tmp/termx-real-ls6-shrink.txt`
  - `/tmp/termx-real-ls6-expand.txt`
- Shrink capture now starts with the top of the listing and includes right-side entries as later rows:
  - `AGENTS.md             fanout`
  - `server_contract_test.go          termx_test.go`
  - `CLAUDE.md             frameaudit`
  - `server_perf_test.go              third_party`
  - `transport_integration_test.go`
  - `transport_slow_consumer_test.go`
  - `terminal.go                      tuiv2`
- Expand capture restores the original wide multi-column `ls` layout.
- Tests/build passed:
  - `GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render ./tuiv2/app -run 'TestResizePreview|TestRuntimeResizePaneShrinkKeepsRenderOnSnapshotUntilOutput|TestRenderPipeline.*ResizePreview|TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot'`
  - `GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render`
  - `GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx`

Commit:

- Pending real ls shrink/expand restore fix commit.


## Follow-up: Repeated `cat /tmp/termx-real-ls-shrink.txt; ls` History Resize

User feedback: with multiple blocks of output, resizing caused content to offset, disappear, or restore to the wrong visible region. Suggested reproduction was repeatedly running `cat /tmp/termx-real-ls-shrink.txt` and `ls`.

Reproduction:

- tmux session: `termx-resize-repeat-cat-ls`
- generated `/tmp/termx-real-ls-shrink.txt` from a real `command ls` capture
- ran three rounds of:
  - `cat /tmp/termx-real-ls-shrink.txt; command ls`
- captures before the final fix:
  - `/tmp/termx-repeat-cat-ls-before.txt`
  - `/tmp/termx-repeat-cat-ls-shrink.txt`
  - `/tmp/termx-repeat-cat-ls-expand.txt`

Observed issue:

- The current screen contained a mix of previous captured UI text, the latest real `ls`, and shell prompt.
- Shrink preview could jump to the tail of the reflowed content, hiding the visible top of the latest `ls` block.
- Expand could appear to restore a different offset because the preview viewport selection was anchored inconsistently.

Fix:

- Removed cursor/prompt-bottom anchoring for non-alt preview viewport selection.
- Non-alt resize preview now anchors to the top of the captured visible source rows when reflowed content exceeds viewport height.
- This avoids jumping to the tail of multi-block history during shrink and keeps expand aligned with the same captured visible region.

Validation after fix:

- tmux session: `termx-resize-repeat-cat-ls3`
- captures:
  - `/tmp/termx-repeat-cat-ls3-before.txt`
  - `/tmp/termx-repeat-cat-ls3-shrink.txt`
  - `/tmp/termx-repeat-cat-ls3-expand.txt`
- Shrink capture starts with the same visible `ls` block instead of jumping to the tail:
  - `AGENTS.md             fanout`
  - `server_contract_test.go          termx_test.go`
  - `CLAUDE.md             frameaudit`
  - `server_perf_test.go              third_party`
  - `transport_integration_test.go`
  - `transport_slow_consumer_test.go`
  - `terminal.go                      tuiv2`
- Expand capture restores the same top visible region from before resize.
- Note: because the source screen itself contained previous `cat` output of the termx UI below the latest `ls`, expand correctly restores that content too; this is not new data loss but part of the captured visible source.

Validation commands passed:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render ./tuiv2/app -run 'TestResizePreview|TestRuntimeResizePaneShrinkKeepsRenderOnSnapshotUntilOutput|TestRenderPipeline.*ResizePreview|TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot'
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
```

Commit:

- Pending repeated cat/ls viewport anchor fix commit.

## Follow-up: Native tmux Reflow Semantics and Current Gaps

User feedback on the current branch:

- Repeated real multi-line command output is still unstable during shrink/expand.
- Reproduction inside `termx`:
  - `cat /tmp/termx-real-ls-shrink.txt; command ls`
  - `cat /tmp/termx-real-ls-shrink.txt; command ls`
  - `cat /tmp/termx-real-ls-shrink.txt; command ls`
  - then shrink/expand the pane.
- Observed failure: content can shift to the wrong paragraph, disappear, or recover inconsistently.
- Important conclusion: the current non-alt preview reflow implementation is not the correct endpoint. It uses whitespace/token-like wrapping and simple viewport anchoring, which does not match tmux grid reflow semantics.

Native tmux experiment:

- Command in native tmux: `clear; command ls`.
- Resize path: start at `120x34`, shrink to `58x22`, then expand back to `120x34`.
- Native tmux shrink does not recompute `ls` as filename/token columns.
- It splits existing grid cells by display width. For example, the filename `terminalmeta` can become:
  - `ter`
  - `minalmeta`
- Native tmux can expand back to the original wide layout because it maintains wrapped-line semantics, not tokenized command-output semantics.

Relevant tmux source:

- `_tmux-src/grid.c`
  - `grid_reflow()`
  - `grid_reflow_split()`
  - `grid_reflow_join()`
- `_tmux-src/screen.c`
  - `screen_reflow()`
  - `grid_wrap_position()`
  - `grid_unwrap_position()`

Tmux-like reflow semantics to model:

- Reflow operates over history plus visible grid rows.
- It uses each row's `cellused` / display width, not shell command structure or filename tokens.
- When a row is wider than the new width, split by cell display width, not by whitespace or token boundaries.
- Continuation rows produced by split carry wrapped-line metadata equivalent to tmux `GRID_LINE_WRAPPED`.
- When a row is narrower than the new width and was originally wrapped, join can pull cells back from following wrapped continuation rows.
- Hard lines that were not wrapped must not merge with the next hard line.
- Cursor position is preserved through logical wrapped coordinates using tmux-style wrap/unwrap position handling, not by guessing from prompt text.
- Alt-screen/fullscreen content should not use ordinary text reflow. It should keep a two-dimensional grid model and crop/restore cells across resize preview.

Current implementation gaps to fix next:

- `tuiv2/runtime/resize.go` still contains `previewReflowCut`, which prefers whitespace boundaries and trims leading/trailing spaces during preview generation. This is incompatible with tmux-style cell-width splitting.
- The non-alt preview currently builds rows from the already materialized snapshot without explicit wrapped-line metadata, so it cannot reliably distinguish hard line breaks from wrapped continuations.
- Viewport selection is still heuristic. Recent commits moved between bottom anchoring and visible-top anchoring, but tmux preserves logical grid/cursor position instead of choosing rows by prompt or simple top/bottom rules.
- Loading the provisional snapshot back into the local vterm helped hide clipped live-vterm states, but it should not be treated as the fundamental solution. Preview generation must be regenerated from a stable preview source, not repeatedly derived from previous provisional output.

Design direction for the next implementation phase:

- Replace whitespace/token-like non-alt preview reflow with a tmux-like preview grid source.
- Capture preview source with scrollback rows, visible screen rows, row effective width/cellused, wrapped flags, cursor absolute row/col, terminal size, modes/alt-screen, timestamps, row kinds, and styles.
- Generate non-alt preview by splitting rows on cell display width and marking split continuations as wrapped.
- Generate expand previews from the original captured source so shrink→expand can restore the original hard-line and wrapped-line structure.
- Keep alt-screen preview as two-dimensional crop/restore, without ordinary text reflow.
- Keep preview source alive through resize echo/prompt noise during a resize burst, but clear it on real user input and on true new command output once that lifecycle is clearly distinguished.

Required real validation after redesign:

- Hard columns: `clear; printf 'COL_A                 COL_B                 COL_C\n'; cat`, then shrink/expand. `COL_A`, `COL_B`, and `COL_C` must remain visible on shrink and restore on expand.
- Real command `ls`: `clear; command ls`, then shrink/expand. Shrink should resemble native tmux cell-split behavior and expand should restore the original multi-column layout.
- Repeated history: generate `/tmp/termx-real-ls-shrink.txt`, run `cat /tmp/termx-real-ls-shrink.txt; command ls` multiple times, then shrink/expand. Preview must not jump to a wrong paragraph, obviously lose current visible content, or fail to restore the captured visible area on expand.
- Real output exit: after the next true user input, preview must not remain stuck and real command output must become visible.

Validation commands required after implementation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Commit:

- Pending documentation commit.

## Follow-up: TDD Cell-Width Split Preview Reflow Slice

Goal:

- Start the redesign with TDD and remove the most direct tmux mismatch in non-alt preview reflow: whitespace/token-aware splitting.
- This is not the full tmux-like preview grid model yet. Snapshot/protocol still lacks explicit wrapped-line metadata, so this commit intentionally limits scope to cell-width splitting and preserving whitespace cells inside split segments.

Tests added first:

- `TestResizePreviewNonAltShrinkSplitsByCellWidthNotWhitespace`
  - Source row: `terminalmeta`.
  - Shrink width: `3`.
  - Expected first segments: `ter`, `min`, `alm`, matching grid-cell splitting rather than filename/token wrapping.
- `TestResizePreviewNonAltShrinkPreservesSplitWhitespaceCells`
  - Source row: `ab   cd`.
  - Shrink width: `4`.
  - Initial failure showed the old implementation trimmed the leading continuation space and produced `cd` instead of ` cd`.
- `TestResizePreviewNonAltHardLinesDoNotJoinOnExpand`
  - Source hard rows: `abc`, `def`.
  - Expand width: `12`.
  - Confirms this slice does not merge independent hard rows.

Implementation:

- Updated `tuiv2/runtime/resize.go` non-alt preview row generation to keep split segments exactly as source cells selected by display width.
- Removed whitespace-preferred split behavior from `previewReflowCut`.
- Stopped trimming leading continuation spaces and trailing segment spaces during preview generation.
- Kept alt-screen crop/restore behavior unchanged.
- Did not modify render/projection paths.
- Did not modify screen update / snapshot / bootstrap binary protocol.

Validation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreviewNonAltShrinkSplitsByCellWidthNotWhitespace|TestResizePreviewNonAltShrinkPreservesSplitWhitespaceCells|TestResizePreviewNonAltHardLinesDoNotJoinOnExpand|TestResizePreviewNonAltShrinkReflowsHardColumns|TestResizePreviewNonAltShrinkExpandRestoresFromOriginalSource'
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Results:

- Targeted runtime resize preview tests passed.
- Required runtime/render tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.901s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.171s`
- Required build passed after rerun with a non-readonly shell variable name.
- `.cache` removed after validation.

Resume From Here:

- Current commit should be the TDD cell-width split slice.
- Next phase should add explicit preview-source row metadata for wrapped continuations, then test shrink→expand recovery from original hard-line/wrapped-line source.
- Need real tmux-in-tmux validation after the wrapped metadata/lifecycle phase, especially repeated `cat /tmp/termx-real-ls-shrink.txt; command ls`.

Commit:

- Pending TDD cell-width split commit.

## Follow-up: TDD Wrapped Row Metadata Preview Reflow Slice

Goal:

- Add the minimal wrapped-row metadata needed for tmux-like logical line recovery during preview generation.
- Keep the change local to existing snapshot row-kind metadata and avoid changing screen update / snapshot / bootstrap transport encoding.

Tests added first:

- `TestResizePreviewNonAltWrappedLinesJoinOnExpand`
  - Source rows: `abcde` followed by `fgh`.
  - The second row is marked `protocol.SnapshotRowKindWrapped`.
  - Expanding to width `8` should join them into `abcdefgh` and leave the next row blank.
- `TestResizePreviewNonAltSplitMarksContinuationRowsWrapped`
  - Source row: `terminalmeta`.
  - Shrinking to width `3` should mark continuation preview rows as `wrapped`, while leaving the first segment unwrapped.

Red/green notes:

- Initial test run failed to compile because `protocol.SnapshotRowKindWrapped` did not exist.
- After adding the constant and reworking preview row generation, targeted tests passed.

Implementation:

- Added `protocol.SnapshotRowKindWrapped = "wrapped"`.
- Updated `tuiv2/runtime/resize.go` non-alt preview generation to build logical rows by joining source rows whose row kind is `wrapped`.
- Re-splits each logical row by display cell width for the requested preview width.
- Marks generated continuation rows as `protocol.SnapshotRowKindWrapped`.
- Preserves hard-line boundaries because only source rows explicitly marked `wrapped` are joined.
- Keeps alt-screen crop/restore behavior unchanged.
- Does not mutate render / Visible / projection paths.
- Does not change binary screen update / snapshot / bootstrap protocols.

Validation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreviewNonAltWrappedLinesJoinOnExpand|TestResizePreviewNonAltSplitMarksContinuationRowsWrapped|TestResizePreviewNonAltShrinkPreservesSplitWhitespaceCells|TestResizePreviewNonAltHardLinesDoNotJoinOnExpand'
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Results:

- Targeted wrapped metadata tests passed.
- Required runtime/render tests passed:
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.903s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.168s`
- Required build passed.
- `.cache` removed after validation.

Resume From Here:

- Current implementation can preserve logical wrapped groups if source rows carry `protocol.SnapshotRowKindWrapped`.
- Next phase must ensure preview source capture can actually infer/carry wrapped state from real terminal output, not just synthetic tests.
- Real tmux-in-tmux validation is still required for hard columns, real `ls`, repeated history, and real output exit.

Commit:

- Pending wrapped row metadata commit.

## Follow-up: TDD Real Auto-Wrap Capture Metadata Slice

Goal:

- Ensure resize preview sources captured from real local `vterm` output can carry wrapped-row metadata, not only synthetic snapshots in tests.
- This enables the previous wrapped logical-row reflow to apply to real auto-wrapped output.

Tests added first:

- `TestVTermWriteMarksAutoWrappedRows`
  - Creates a `5x3` local vterm and writes `abcdef`.
  - Expected row 1 to be marked `protocol.SnapshotRowKindWrapped`.
  - Initial failure: row kinds were all empty.
- `TestCaptureResizePreviewSourceCarriesVTermWrappedRows`
  - Writes auto-wrapped output to a local vterm, captures resize preview source, and asserts the captured snapshot carries the wrapped row kind.

Implementation:

- Added conservative wrapped-row inference during `vterm` row metadata reconciliation.
- If a non-alt screen row has content and the previous physical row uses the full terminal width, the row is marked `protocol.SnapshotRowKindWrapped` unless it already has a row kind.
- The inference reads emulator cells directly to avoid disturbing screen row view/cache reconciliation.
- Added a small display-width helper for vterm cells.
- Kept render / Visible / projection paths pure.
- Did not change screen update / snapshot / bootstrap binary protocols.

Validation and fix notes:

- First broader `./vterm` validation exposed a row-cache side effect:
  - `TestVTermPreservesRowTimestampAcrossScroll` expected `abcd` in scrollback but saw `efgh`.
- Root cause: wrapped inference used `screenRowViewLocked` while metadata/cache reconciliation was in progress.
- Fix: read current emulator cells directly via `emu.CellAt` for inference.

Validation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./vterm -run 'TestVTermWriteMarksAutoWrappedRows|TestVTermPreservesRowTimestampAcrossScroll'
GOCACHE=$PWD/.cache/go-build go test ./vterm ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Results:

- Targeted vterm tests passed.
- Broader validation passed:
  - `ok github.com/lozzow/termx/vterm 0.369s`
  - `ok github.com/lozzow/termx/tuiv2/runtime 0.779s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.163s`
- Required build passed.
- `.cache` removed after validation.

Resume From Here:

- Real output can now infer wrapped continuation rows in local vterm metadata and carry them into resize preview source snapshots.
- Next phase should run tmux-in-tmux captures for hard columns, real `ls`, repeated history, and real output exit.
- If captures still show paragraph jumps, focus next on viewport/cursor anchoring over the logical reflowed grid rather than additional whitespace/token heuristics.

Commit:

- Pending real auto-wrap capture metadata commit.

## Follow-up: TDD Hard-Row Guard for Wrapped Inference

Goal:

- Fix false-positive wrapped inference found by real tmux `clear; command ls` capture.
- Keep tmux-like cell splitting for shrink while ensuring CRLF hard rows restore as separate rows on expand.

Real tmux capture that exposed the bug:

- Session: `termx-resize-tdd-ls3`.
- Isolated paths:
  - socket: `/tmp/termx-resize-tdd-ls3/termx.sock`
  - state: `/tmp/termx-resize-tdd-ls3/state`
  - log: `/tmp/termx-resize-tdd-ls3/termx.log`
- Captures:
  - `/tmp/termx-resize-tdd-ls3-before.txt`
  - `/tmp/termx-resize-tdd-ls3-shrink.txt`
  - `/tmp/termx-resize-tdd-ls3-expand.txt`
- Failure:
  - Shrink split cells, but expand incorrectly joined independent `ls` hard rows, for example `terminalmetaCLAUDE.md` appeared on one row.

Root cause:

- Wrapped inference treated any previous row whose storage width equaled terminal width as full/wrapped.
- Because row views are dense and padded to terminal width, hard rows with trailing blanks could be misclassified as wrapped.

Tests added:

- `TestVTermWriteDoesNotMarkCRLFHardRowsWrapped`
  - Writes `alpha     beta\r\ngamma\r\n` to a `20x4` vterm.
  - Asserts the second hard row is not marked `protocol.SnapshotRowKindWrapped`.

Implementation:

- Changed vterm cell-used inference to use the last nonblank/styled cell position plus display width, rather than counting dense padded blank cells.
- Kept auto-wrap continuation detection for true full-width rows.
- Preserved the previous cache-safety fix by reading emulator cells directly during inference.

Validation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./vterm -run 'TestVTermWriteMarksAutoWrappedRows|TestVTermWriteDoesNotMarkCRLFHardRowsWrapped'
GOCACHE=$PWD/.cache/go-build go test ./vterm ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Results:

- Targeted tests passed.
- Broader validation passed:
  - `ok github.com/lozzow/termx/vterm 0.243s`
  - `ok github.com/lozzow/termx/tuiv2/runtime 1.237s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.830s`
- Required build passed.
- `.cache` removed after validation.

Follow-up real tmux validation:

- Session: `termx-resize-tdd-ls4`.
- Captures:
  - `/tmp/termx-resize-tdd-ls4-before.txt`
  - `/tmp/termx-resize-tdd-ls4-shrink.txt`
  - `/tmp/termx-resize-tdd-ls4-expand.txt`
- Result:
  - Shrink shows tmux-like cell splits, for example `terminalmeta` becomes `t` / `erminalmeta` across rows.
  - Expand restores independent `ls` hard rows instead of joining them.
  - No `terminalmetaCLAUDE.md`-style false join remained in the sampled expand capture.

Resume From Here:

- Continue real tmux-in-tmux validation for repeated history and real output exit.
- If repeated history still jumps, next likely area is viewport/cursor anchoring over the reflowed logical grid, not wrapped inference.

Commit:

- Pending hard-row wrapped inference guard commit.

## Follow-up: TDD Captured Visible-Top Viewport Anchor

Goal:

- Fix repeated-history shrink/expand jumping to the wrong part of combined scrollback + visible content.
- Keep the anchor based on captured preview source geometry, not prompt text or render-layer mutation.

Real tmux capture that exposed the bug:

- Session: `termx-resize-tdd-repeat`.
- Reproduction:
  - create `/tmp/termx-real-ls-shrink.txt` from the real-ls shrink capture.
  - run three times inside termx: `cat /tmp/termx-real-ls-shrink.txt; command ls`.
  - shrink `120x34 -> 58x22`, then expand `58x22 -> 120x34`.
- Captures:
  - `/tmp/termx-resize-tdd-repeat-before.txt`
  - `/tmp/termx-resize-tdd-repeat-shrink.txt`
  - `/tmp/termx-resize-tdd-repeat-expand.txt`
- Failure:
  - Shrink anchored too far upward at the command line / beginning of the combined history rather than the captured visible top.

Tests added first:

- `TestResizePreviewNonAltAnchorsToCapturedVisibleTopAfterHistory`
  - Builds a source snapshot with two scrollback rows plus four visible screen rows.
  - Resizing to the same screen height should keep `visible-one` at row 0 and `visible-four` at row 3.
  - Initial failure rendered `history-one` and `history-two` in the visible area.

Implementation:

- `reflowSnapshotRowsForPreview` now returns the reflowed row index corresponding to the captured visible screen top.
- `previewScreenStartForNonAltResize` uses that visible-top row as the viewport start, clamped to the available reflowed rows.
- This still regenerates preview from the original source rows and preserves scrollback before the selected screen window.
- No render / Visible / projection mutation was added.
- No screen update / snapshot / bootstrap binary protocol change was made.

Validation:

```sh
GOCACHE=$PWD/.cache/go-build go test ./tuiv2/runtime -run 'TestResizePreviewNonAltAnchorsToCapturedVisibleTopAfterHistory|TestResizePreviewNonAltShrinkReflowsHardColumns|TestResizePreviewNonAltWrappedLinesJoinOnExpand'
GOCACHE=$PWD/.cache/go-build go test ./vterm ./tuiv2/runtime ./tuiv2/render
GOCACHE=$PWD/.cache/go-build go build -o ./termx ./cmd/termx
rm -rf .cache
```

Results:

- Targeted viewport-anchor tests passed.
- Broader validation passed:
  - `ok github.com/lozzow/termx/vterm 0.923s`
  - `ok github.com/lozzow/termx/tuiv2/runtime 1.586s`
  - `ok github.com/lozzow/termx/tuiv2/render 1.919s`
- Required build passed.
- `.cache` removed after validation.

Follow-up real tmux validation:

- Session: `termx-resize-tdd-repeat2`.
- Captures:
  - `/tmp/termx-resize-tdd-repeat2-before.txt`
  - `/tmp/termx-resize-tdd-repeat2-shrink.txt`
  - `/tmp/termx-resize-tdd-repeat2-expand.txt`
  - `/tmp/termx-resize-tdd-repeat2-real-output.txt`
- Result:
  - Shrink/expand stayed in the captured visible region instead of jumping back to the command line.
  - Expand preserved the repeated-history context and then showed the current real `ls` content below it.
  - Real output exit check passed: `/tmp/termx-resize-tdd-repeat2-real-output.txt` contains `AFTER_REAL_OUTPUT` after sending `echo AFTER_REAL_OUTPUT`.

Resume From Here:

- Current branch has TDD coverage for cell-width splitting, wrapped logical row join/split, real vterm wrapped-row capture, hard-row guard, and visible-top viewport anchoring.
- Real tmux captures now cover hard columns, real `ls`, repeated history, and real output exit.
- If more polish is needed, compare the exact shrink viewport against native tmux cursor wrap/unwrap behavior; avoid prompt-string heuristics and keep changes in runtime/source projection, not render.

Commit:

- Pending captured visible-top viewport anchor commit.
