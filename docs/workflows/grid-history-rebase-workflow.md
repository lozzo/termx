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
- Phase 2: Fix resize swallowing history. Complete.
- Phase 3: Fix middle-history continuity. Complete.
- Phase 4: Fix history file loss/recovery. Complete.
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
gofmt -w termx-vterm/vterm/vterm.go termx-core/terminal.go
go test ./termx-vterm/... ./termx-core/...
go test ./termx-vterm/vterm -run 'ResizeWithDamage' -v
go test ./termx-core -run 'TerminalGridResizeDamage' -v
gofmt -w termx-vterm/vterm/vterm.go termx-vterm/vterm/load_snapshot_test.go termx-core/terminal_test.go
go test ./termx-vterm/... ./termx-core/...
go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamage|TestVTermResizeReflows' -count=1
go test ./termx-core -run 'TestTerminalGridResizeDamage|TestTerminalGridStoreRowsReflow' -count=1
go test ./termx-vterm/... ./termx-core/...
rg -n "GridViewportWithOptions|ApplyGridViewportPage|loadTerminalHistoryViewportCmd|copyModeScrollbackCmd|ensureTerminalScrollbackLoadedCmd|ScrollbackLoadedLimit|ResizeWithDamage" termx-core tuiv2 internal termx-vterm -S
go test ./termx-core -run 'TestTerminalGridViewportUsesCanonicalCols|TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamage|TestServerGridViewportFromStoreUsesDefaultCanonicalCols|TestServerHistorySurvivesServerRestart' -count=1
go test ./tuiv2/app -run 'TestCopyModeHistoryRequestUsesCanonicalCols|TestPaneScrollbackPrefetchUsesCanonicalCols|TestCopyModeSelectedTextPreservesSoftWrappedLines|TestCopyModeSelectedTextNormalizesReverseMultiRowSelection' -count=1
go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPage|TestRuntimeLoadGridViewportDoesNotReplaceLiveSnapshot' -count=1
go test ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...
go test ./termx-core -run 'TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamageKeepsWrappedContinuity' -count=1
go test ./termx-core -run 'TestTerminalGridStoreRecovery|TestServerRemoveDeletesPersistentGridHistory|TestNewTerminalGridStoreStartsFreshGeneration|TestTerminalGridStoreReopensPersistedRows|TestServerHistorySurvivesServerRestart' -count=1
go test ./termx-core/...
go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamagePreservesWrappedBoundary|TestVTermResizeWithDamageAppendsRowsDisplacedByShrink|TestVTermResizeWithDamagePreservesWideRows' -count=1
go test ./termx-vterm/... ./termx-core/...
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

## Phase 2 Notes

- Added `VTerm.ResizeWithDamage(cols, rows)`.
- `VTerm.Resize` now delegates to the same damage-aware path and discards the returned damage.
- Resize damage is marked full-replace with reason `resize`, carries the new size, cursor and modes, and includes `ScrollbackAppend` rows for normal-screen content that can be lost when the emulator has normal scrollback disabled.
- The resize capture uses parsed screen cells before resize, reflows them to the new canonical width, compares against the post-resize visible screen, and appends affected logical lines as structured rows. This is deliberately conservative for wrapped groups: if part of a logical line would be split between history and screen, the complete logical line is written to grid history to avoid broken copy-mode pages.
- Resize synthetic append rows are now treated as the resize preservation authority instead of being skipped when emulator scrollback also reports rows. This avoids partial emulator append rows leaving grid continuity dependent on emulator scrollback configuration.
- `Terminal.Resize` now calls `vterm.ResizeWithDamage`, captures alternate damage, appends normal `ScrollbackAppend` rows via `appendGridFromDamageLocked`, bumps the screen revision, and broadcasts a fresh screen update after the resize frame.
- `Terminal.Resize` now commits `t.size` only after `pty.Resize` succeeds, so failed resize does not corrupt canonical terminal size.
- Added vterm tests for resize damage:
  - shrink preserving 000098/000099/000100-style tail rows across damage plus screen
  - wrapped boundary continuity
  - wide-char and QR-like row preservation
- Added core tests for grid store persistence of resize damage:
  - 100 and 1000 stress rows after shrink
  - wrapped logical line continuity through `terminalGridStore.Viewport`
  - wide-char and QR-like rows through compact grid codec and viewport

## Phase 2 Test Results

- PASS: `go test ./termx-vterm/... ./termx-core/...`
- PASS: `go test ./termx-vterm/vterm -run 'ResizeWithDamage' -v`
- PASS: `go test ./termx-core -run 'TerminalGridResizeDamage' -v`
- PASS: `go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamage|TestVTermResizeReflows' -count=1`
- PASS: `go test ./termx-core -run 'TestTerminalGridResizeDamage|TestTerminalGridStoreRowsReflow' -count=1`

## Phase 2 Review Notes

- Reviewer A (grid store / resize correctness) findings:
  - Complete wrapped logical line append can duplicate a suffix that remains visible after resize. This is a real grid/screen boundary risk. Current Phase 2 keeps the conservative no-loss append for grid history because live screen-update payloads do not transmit `damage.ScrollbackAppend`; Phase 3 must make snapshot/copy-mode merge explicitly avoid duplicate screen/history boundary rows.
  - Synthetic resize preservation was originally skipped when emulator scrollback produced any append rows. Fixed now: `ResizeWithDamage` always computes synthetic resize append rows for normal screen and uses those as the resize preservation authority, avoiding partial emulator append behavior.
- Reviewer B (TUI/tmux behavior) findings:
  - `tuiv2/app/update_helpers.go` currently computes history load cols from active pane viewport and passes that through `Runtime.LoadGridViewport` to core `GridViewport`, so follower/floating/App history can be reflowed at observer width instead of canonical PTY width. This must be fixed in Phase 3.
  - `tuiv2/runtime/snapshot.go` merges grid viewport pages into `terminal.Snapshot`, then `loadSnapshotIntoVTerm` treats `snapshot.Size.Cols` as authoritative. A page loaded at observer width can locally resize the runtime vterm without owner PTY resize. This must be fixed in Phase 3/5.
  - `tuiv2/runtime/resize.go` provisional local shrink snapshot changes declared size but does not crop row cells to new cols. This is a short-window owner-shrink rendering inconsistency to fix with TUI resize work.
- Reviewer C (storage recovery) findings:
  - Explicit remove currently closes persistent grid history without deletion; replay paths may still read removed-terminal history. This belongs to Phase 4 retention/generation cleanup semantics.
  - Crash recovery trusts `grid.index` and does not validate page payload durability. Need index/page scan and truncation to last valid committed row in Phase 4.
  - Partial index tails are floored in memory but not truncated, so future append may misalign records. Fix in Phase 4.
  - Metadata write is non-atomic and metadata read errors can brick valid index/page history. Fix in Phase 4 by making metadata advisory/atomic.
  - Persistent retention is unbounded. Fix through explicit retention/generation policy in Phase 4.
  - Low-risk appender slice aliasing: fixed immediately by cloning damage rows in `terminalGridAppender.append`.

## Phase 3 Notes

- Live `Terminal.GridViewportWithOptions` now ignores caller `Cols` and always reads/reflows normal grid history at the terminal's canonical `t.size.Cols`.
- Store-backed/offline `Server.GridViewport` now ignores caller `Cols` and uses server default canonical cols instead of observer width. This keeps exited-terminal history from being reshaped by pane/floating/mobile widths.
- TUI history prefetch and copy-mode paging now pass `terminalCanonicalCols`, derived from snapshot, VTerm, or resize ownership size; they no longer use pane content width as the history interpretation width.
- `Terminal.Snapshot` trims duplicate grid/screen boundary rows introduced by conservative resize preservation, but only when latest snapshot offset is zero and content, wrapped metadata, and available row metadata agree. This avoids dropping unrelated older rows that merely share the same text.
- `Runtime.ApplyGridViewportPage` rejects stale geometry pages when page cols differ from the current snapshot cols.
- `Runtime.ApplyGridViewportPage` now applies loaded history pages to the snapshot cache only and marks `PreferSnapshot`; it does not reload materialized history into the local VTerm. This prevents observer history paging from resizing or bloating the live local emulator.
- Copy-mode selected text now uses `ScrollbackWrapped`/`ScreenWrapped` to avoid inserting hard newlines between soft-wrapped rows, closer to tmux copy semantics.
- Added focused tests for:
  - canonical cols in live core `GridViewport`
  - canonical cols in store-backed/offline `GridViewport`
  - resize grid/screen duplicate trimming
  - pane prefetch and copy-mode history requests using canonical cols
  - copy-mode selection preserving soft-wrapped lines
  - runtime rejection of stale-geometry history pages
  - runtime history page merge staying snapshot-only

## Phase 3 Test Results

- PASS: `go test ./termx-core -run 'TestTerminalGridViewportUsesCanonicalCols|TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamage|TestServerGridViewportFromStoreUsesDefaultCanonicalCols|TestServerHistorySurvivesServerRestart' -count=1`
- PASS: `go test ./tuiv2/app -run 'TestCopyModeHistoryRequestUsesCanonicalCols|TestPaneScrollbackPrefetchUsesCanonicalCols|TestCopyModeSelectedTextPreservesSoftWrappedLines|TestCopyModeSelectedTextNormalizesReverseMultiRowSelection' -count=1`
- PASS: `go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPage|TestRuntimeLoadGridViewportDoesNotReplaceLiveSnapshot' -count=1`
- PASS: `go test ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...`
- PASS: `go test ./termx-core -run 'TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamageKeepsWrappedContinuity' -count=1`

## Phase 3 Review Notes

- Reviewer B (TUI/tmux behavior) follow-up:
  - Store-backed/offline `grid.viewport` still reflowed parsed history at caller width. Fixed by making `gridViewportCoreFromStore` use server default canonical cols and adding a regression test.
  - Copy-mode selected text inserted hard newlines between all physical rows. Fixed by consulting wrapped metadata and adding a soft-wrap copy regression test.
- Reviewer C (storage recovery) follow-up:
  - Reconfirmed high-priority Phase 4 risks: page/index append failure recovery, crash validation, partial index tail truncation, persistent remove/generation semantics, metadata atomicity/advisory behavior, and retention.

## Phase 4 Notes

- Grid store startup now validates committed index records against page payloads by reading and decoding each referenced row. `grid.index` records remain the commit source of truth; rows after the first invalid/missing/truncated page payload are truncated.
- Partial index tails are truncated on open before any future append, so new records do not append after a misaligned tail.
- Page files are truncated to the last valid committed row end; later-generation page files beyond the valid max sequence are removed.
- Metadata read failures are now advisory. A corrupt or truncated `grid.meta.pb` no longer prevents replay from valid index/page rows. Read-only replay close no longer rewrites metadata.
- Append accounting now advances `currentBytes` for page bytes actually written before index commit. If index append fails, the index writer truncates back to its pre-write size.
- Persistent `newTerminalGridStore` starts a fresh generation by deleting any previous deterministic grid directory for the same terminal ID. `Server.Remove` and protocol `remove` also delete persistent grid history.
- Resize reflow metadata was improved while handling reviewer A feedback: resize reflow rows now carry timestamp/kind/fingerprint through line splitting, and visible block matching avoids last-match tie breaking. The remaining wrapped-line source duplicate risk is not fully removed because writing only a visible-suffix fragment can break logical-line order; this remains a Phase 5/final risk to address with a more explicit grid/screen merge model.

## Phase 4 Test Results

- PASS: `go test ./termx-core -run 'TestTerminalGridStoreRecovery|TestServerRemoveDeletesPersistentGridHistory|TestNewTerminalGridStoreStartsFreshGeneration|TestTerminalGridStoreReopensPersistedRows|TestServerHistorySurvivesServerRestart' -count=1`
- PASS: `go test ./termx-core/...`
- PASS: `go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamagePreservesWrappedBoundary|TestVTermResizeWithDamageAppendsRowsDisplacedByShrink|TestVTermResizeWithDamagePreservesWideRows' -count=1`
- PASS: `go test ./termx-vterm/... ./termx-core/...`

## Phase 4 Review Notes

- Reviewer C storage findings addressed:
  - page/index crash recovery now validates payload durability and truncates to the last valid committed row
  - partial index tails are repaired before append
  - corrupt metadata is advisory when index/page rows are valid
  - explicit remove and fresh generation cleanup delete persistent grid directories
- Remaining storage risk:
  - There is still no retention cap/generation retention policy beyond fresh-generation/remove cleanup; persistent history can grow until explicit deletion or future retention work.
- Reviewer A resize findings addressed in part:
  - resize visible block tie breaking now prefers the first best match instead of the last
  - resize reflow lines carry metadata through splitting
  - conservative complete wrapped logical group append remains for no-loss semantics and is still deduped at latest snapshot grid/screen boundary

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
