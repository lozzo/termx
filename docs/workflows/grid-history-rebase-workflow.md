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

- Phase 0: Read baseline and sizing implementation scope. Complete.
- Phase 1: Restore/fix tmux-like grid history main path. Complete for the clean baseline; keep enforcing during later merges.
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
rg -n "GridViewport|appendGridFromDamage|WriteWithDamage|WriteForLatestFrame|ResizeWithDamage|ScreenUpdate|ScrollbackAppend|terminal_event|event_log|terminalHistoryLine|Resize\\(" termx-core termx-vterm tuiv2 internal termx-proto remote-ui termx-app termx-remote web-control
git show c9d35322:termx-vterm/vterm/vterm.go | rg -n "ResizeWithDamage|func \\(v \\*VTerm\\) Resize|capture.*row|ScrollbackAppend|ScreenUpdate|damage" -C 8
git show c9d35322:termx-core/terminal.go | rg -n "func \\(t \\*Terminal\\) Resize|append.*Damage|ScrollbackAppend|screenInvalidation|bootstrap|fresh|latest" -C 8
rg -n "terminal_event_log|terminalEvent|historyLine|terminal_history_line|raw PTY|pty journal|event log|EventLog" -S .
```

Cherry-pick note: `d4527bfd` conflicted because `docs/terminal-pool-pty-journal.md` and `termx-core/docs/terminal-history-event-log-plan.md` were deleted on the `feac69ea` baseline. The resolution kept those files deleted and restored only `docs/terminal-history-grid-decision.md`.

## Phase 0 Notes

- Read:
  - `termx-core/terminal_grid_store.go`
  - `termx-core/terminal_grid_appender.go`
  - `termx-core/terminal_grid_codec.go`
  - `termx-core/terminal.go`
  - `termx-vterm/vterm/vterm.go`
  - `tuiv2/runtime/snapshot.go`
  - `tuiv2/runtime/resize.go`
- Current clean baseline already keeps `terminalGridStore`, `terminalGridAppender`, and `terminalGridCodec`.
- `spawnTerminalProcess` creates a canonical vterm and calls `DisableEmulatorScrollback()`, so normal history is intended to come from vterm damage into `terminalGridStore`.
- `Terminal.Snapshot`, `Terminal.GridViewportWithOptions`, and `Terminal.HistoryReplay` flush the grid appender and prefer structured grid rows before falling back to live vterm scrollback.
- `terminalGridStore.windowRefs` already expands a requested window backward when `start` lands after a wrapped row, avoiding a viewport that begins in the middle of a wrapped continuation group.
- Current `Terminal.Resize` only does `pty.Resize`, `vterm.Resize`, and broadcast resize. It does not append rows displaced by resize into `terminalGridStore`.
- `c9d35322` implements the right resize damage shape without needing the raw journal path:
  - `VTerm.ResizeWithDamage(cols, rows)` wraps `resizeWithDamageLocked`.
  - `resizeWithDamageLocked` snapshots screen fingerprints, screen rows, timestamps, row kinds, cursor, and old geometry before resize.
  - For normal screen, it computes `resizeScrollbackAppendFromScreenLocked(...)` before `emu.Resize`.
  - After resize it reconciles metadata, builds a full-replace damage with reason `resize`, and fills `damage.ScrollbackAppend` with the displaced rows when normal scrollback did not already capture them.
  - `Terminal.Resize` calls `appendHistoryLinesFromDamageLocked(damage)`, then emits a screen update payload or placeholder. In this branch that should become `appendGridFromDamageLocked(damage)`.

## Phase 1 Notes

- Search found no active `terminal_event_log`, `terminal_history_line`, or raw PTY journal implementation in the clean baseline.
- Normal UI history entry points are structured:
  - core: `GridViewport`, `HistoryReplay`, `Snapshot`
  - protocol: `GridViewport` binary/protobuf payload
  - TUI runtime: `LoadGridViewport`
  - TUI app copy-mode paging: `LoadGridViewport`
- Phase 6 must preserve this shape when remote/protobuf changes are replayed.

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
