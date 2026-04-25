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
- [ ] 5. Design preview source and lifecycle
- [ ] 6. Implement non-alt-screen reflow preview
- [ ] 7. Implement alt-screen crop/restore preview
- [ ] 8. Implement preview exit on real output
- [ ] 9. Add runtime tests
- [ ] 10. Add render tests
- [ ] 11. Validate with tmux capture
- [ ] 12. Run final tests/build
- [ ] 13. Write final summary

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

Pending.

## Implementation Notes

Pending.

## Test and Validation Notes

Pending.

## Known Issues

- None yet.

## Next Step Suggestions

- Design runtime-owned preview source lifecycle.
- Implement non-alt reflow from original preview source.
- Implement alt-screen crop/restore from original preview source.
- Ensure real output exits preview while resize echo/noop does not.

## Resume From Here

Current status:

- Branch: `feature/tuiv2-resize-preview-reflow`
- Last completed TODO: `4. Reproduce current missing capability in tmux`
- Last commit: pending tmux reproduction commit
- Next step: design preview source lifecycle and implement generation from the original source.

Important artifacts:

- `docs/agent-worklogs/resize-preview-reflow-2026-04-25.md`
