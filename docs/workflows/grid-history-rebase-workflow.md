# Grid History Rebase Workflow

This workflow tracks the terminal history rebuild from a clean parsed-grid baseline.

## Branch And Baseline

- Current branch: `grid-history-rebase`
- Baseline commit: `feac69ea Preserve terminal grid geometry across resize`
- Direction commits restored:
  - `d4527bfd Document grid history direction`
  - `2228279e Clarify terminal viewport size semantics`
  - `4eb98a43 Align history semantics with tmux`
- Main design document: `docs/terminal-history-grid-decision.md`

## Stage Status

- Phase 0: Reading baseline and sizing implementation scope. In progress.
- Phase 1: Restore/fix tmux-like grid history main path. Pending.
- Phase 2: Fix resize swallowing history. Pending.
- Phase 3: Fix middle-history continuity. Pending.
- Phase 4: Fix history file loss/recovery. Pending.
- Phase 5: Bound TUI/runtime materialized history memory. Pending.
- Phase 6: Reapply remote/protobuf/app changes without raw journal UI history. Pending.
- Final verification and daemon cleanup. Pending.

## Decisions

- UI, copy-mode, TUI, App, and `remote-ui` terminal history must use core-owned parsed grid/cell rows.
- `terminalGridStore`, `terminalGridAppender`, and `terminalGridCodec` remain the main history storage path.
- Raw PTY journal/event-log code is not a normal UI history query path.
- The real PTY size is globally unique. Other panes/windows/apps are observer viewports and must project canonical terminal content instead of reflowing history at observer width.
- Resize damage from vterm is worth porting from `c9d35322`, but the raw event-log direction is not.
- Every stage should update this document before committing.

## Commands Executed

```bash
git status --short --branch
git log --oneline --decorate -20
git switch -c grid-history-rebase feac69ea
git show --stat --name-only d4527bfd
git show --stat --name-only 2228279e
git show --stat --name-only 4eb98a43
git cherry-pick d4527bfd 2228279e 4eb98a43
git rm docs/terminal-pool-pty-journal.md termx-core/docs/terminal-history-event-log-plan.md
git cherry-pick --continue
```

Cherry-pick note: `d4527bfd` conflicted because `docs/terminal-pool-pty-journal.md` and `termx-core/docs/terminal-history-event-log-plan.md` were deleted on the `feac69ea` baseline. The resolution kept those files deleted and restored only `docs/terminal-history-grid-decision.md`.

## Phase 0 Notes

- Pending code reads:
  - `termx-core/terminal_grid_store.go`
  - `termx-core/terminal_grid_appender.go`
  - `termx-core/terminal_grid_codec.go`
  - `termx-core/terminal.go`
  - `termx-vterm/vterm/vterm.go`
  - `tuiv2/runtime/snapshot.go`
  - `tuiv2/runtime/resize.go`
- Pending comparison against `c9d35322`:
  - `VTerm.ResizeWithDamage`
  - `Terminal.Resize`
  - latest/fresh screen recovery fixes

## tmux Verification

- Not run yet.
- Planned session name: `termx-grid-e2e`
- Need to discover actual TUI launch, panel/split/floating/takeover/copy-mode commands before running scenario tests.

## Sub-Agent Review Summary

- No sub-agent reviews run yet.
- Planned after Phase 2:
  - Reviewer A: grid store / resize correctness
  - Reviewer B: TUI/tmux behavior
  - Reviewer C: storage recovery
- Planned after Phase 6:
  - Reviewer D: remote/protobuf compatibility

## Test Results

- Not run yet.

## Unresolved Risks

- Need to confirm how `GridViewport` currently handles wrapped continuations and canonical width.
- Need to confirm whether resize currently loses visible rows before they reach `terminalGridStore`.
- Need to confirm recovery semantics for `grid.index`, page payloads, metadata, and `removeOnClose`.
- Need to confirm TUI runtime materialized scrollback behavior and copy-mode cloning behavior.
