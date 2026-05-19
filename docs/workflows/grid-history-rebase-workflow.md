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
- Phase 5: Bound TUI/runtime materialized history memory. Complete.
- Phase 6: Reapply remote/protobuf/app changes without raw journal UI history. Complete.
- Final verification and daemon cleanup. Complete.

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
go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPage' -count=1
go test ./tuiv2/app -run 'TestCopyModeLoadsOlderScrollback|TestCopyModeBoundedWindowRequestsNextOlderPage|TestCopyModeTopLoadsOlderScrollbackIntoFrozenBuffer|TestCopyModeEnterPrefetchesHistoryWhenFrozenBufferHasNoScrollback|TestCopyModeHistoryRequestUsesCanonicalCols' -count=1
go test ./tuiv2/runtime/...
go test ./tuiv2/app/...
git cherry-pick dd2f0e61
git cherry-pick 57692c45
rg -n "terminal_event_log|terminalEventLog|terminal_event|raw PTY|pty journal|EventLog|terminal_history_line|historyLine" -S termx-core termx-vterm internal termx-proto tuiv2 remote-ui termx-app termx-remote web-control docs
go test ./internal/protocol/... ./termx-proto/... ./termx-remote/... ./termx-cli/cmd/termx ./termx-hub/cmd/termx-hub
go test ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...
cd remote-ui && npm run typecheck
cd remote-ui && npm run test
go test ./internal/protocol -run 'TestEncodeRemotePairStartAcceptsLegacyGetterParams|TestClientRequestStreamAndProtocolError' -count=1
go test ./internal/protocol/... ./termx-proto/... ./termx-remote/... ./termx-cli/cmd/termx ./termx-hub/cmd/termx-hub
go build -o /tmp/termx-grid-e2e.zj2pYi/termx ./termx-cli/cmd/termx
tmux new-session -d -s termx-grid-e2e -x 120 -y 36 '/tmp/termx-grid-e2e.zj2pYi/termx --socket /tmp/termx-grid-e2e.zj2pYi/termx.sock --log-file /tmp/termx-grid-e2e.zj2pYi/termx.log attach 1'
tmux capture-pane -t termx-grid-e2e -epJS - > /tmp/termx-grid-e2e.zj2pYi/stress100-copy-top.txt
tmux new-session -d -s termx-grid-e2e-page -x 120 -y 36 '/tmp/termx-grid-e2e.zj2pYi/termx --socket /tmp/termx-grid-e2e.zj2pYi/termx.sock --log-file /tmp/termx-grid-e2e.zj2pYi/termx.log attach 2'
tmux capture-pane -t termx-grid-e2e-page -epJS - > /tmp/termx-grid-e2e.zj2pYi/page1000-copy-top-wait3.txt
tmux new-session -d -s termx-grid-e2e-qr -x 120 -y 36 '/tmp/termx-grid-e2e.zj2pYi/termx --socket /tmp/termx-grid-e2e.zj2pYi/termx.sock --log-file /tmp/termx-grid-e2e.zj2pYi/termx.log attach 3'
tmux capture-pane -t termx-grid-e2e-qr -epJS - > /tmp/termx-grid-e2e.zj2pYi/qr-copy-bottom.txt
go test ./...
go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-hub/... ./termx-proto/... ./termx-remote/... ./termx-shared/... ./termx-testkit/... ./termx-vterm/... ./tuiv2/...
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

## Phase 5 Notes

- Runtime materialized grid viewport pages are now bounded by `materializedScrollbackRowLimit` (`12000` rows). Loading older pages prepends rows but trims the newest materialized scrollback rows from the snapshot window instead of growing without bound.
- Copy mode uses the same `terminalMaterializedScrollbackLimit` (`12000` rows). Frozen copy buffers continue to track logical loaded depth separately from materialized row count, so the next page request advances by `offset + len(page)` even after the visible buffer has been trimmed.
- `ScrollbackLoadedLimit` is now used as logical loaded depth for paging progress rather than as a proxy for how many rows must remain resident in `Snapshot.Scrollback`.
- `ensureActivePaneScrollbackCmd` and `copyModeScrollbackCmd` compute paging offsets from loaded depth (`ScrollbackOffset + len(window)`, `LoadedRows`, or `ScrollbackLoadedLimit`) rather than from the current materialized slice length.
- The local VTerm remains on latest live content; loaded history pages stay snapshot-only and bounded.

## Phase 5 Test Results

- PASS: `go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPage' -count=1`
- PASS: `go test ./tuiv2/app -run 'TestCopyModeLoadsOlderScrollback|TestCopyModeBoundedWindowRequestsNextOlderPage|TestCopyModeTopLoadsOlderScrollbackIntoFrozenBuffer|TestCopyModeEnterPrefetchesHistoryWhenFrozenBufferHasNoScrollback|TestCopyModeHistoryRequestUsesCanonicalCols' -count=1`
- PASS: `go test ./tuiv2/runtime/...`
- PASS: `go test ./tuiv2/app/...`

## Phase 6 Notes

- Cherry-picked `dd2f0e61 Accept copied v4 pairing payloads`.
- Cherry-picked `57692c45 Rewrite remote pairing flow`.
- No active `terminal_event_log`, raw PTY journal, or terminal history line implementation was reintroduced. The only raw/journal hits after Phase 6 are documentation, generic JSON/raw naming, database WAL settings, and non-history protocol internals.
- Remote UI terminal history remains structured: `terminalProtocolClient` requests `grid.viewport`, decodes `GridViewport`, and replays structured rows for scrollback pages.
- Native mobile `NativeConnectionProxy` still uses an authenticated `ws://127.0.0.1:<port>` bridge between JS and the native plugin, but the reviewed data path bridges native channels to WebRTC data channels. This was treated as an app-local native bridge, not a browser-to-agent terminal/file/history shortcut.
- Added compatibility for legacy `remote.pair.start` getter params that only expose `GetLocalPairURL()` and `GetTTLSeconds()`, defaulting `auth_ttl_seconds` to zero.

## Phase 6 Test Results

- PASS: `go test ./internal/protocol/... ./termx-proto/... ./termx-remote/... ./termx-cli/cmd/termx ./termx-hub/cmd/termx-hub`
- PASS: `go test ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...`
- PASS: `cd remote-ui && npm run typecheck`
- PASS: `cd remote-ui && npm run test` (58 files, 411 tests; jsdom emitted expected localStorage/canvas warnings)
- PASS: `go test ./internal/protocol -run 'TestEncodeRemotePairStartAcceptsLegacyGetterParams|TestClientRequestStreamAndProtocolError' -count=1`
- PASS: `go test ./internal/protocol/... ./termx-proto/... ./termx-remote/... ./termx-cli/cmd/termx ./termx-hub/cmd/termx-hub`

## Phase 6 Review Notes

- Reviewer D finding:
  - Medium compatibility risk in `internal/protocol/control_payload.go`: `remote.pair.start` params accepted only the new three-getter interface, so older wrappers with `GetLocalPairURL()` and `GetTTLSeconds()` fell through to struct reflection and failed. Fixed by accepting the old two-getter shape and adding `TestEncodeRemotePairStartAcceptsLegacyGetterParams`.
- Reviewer D no-findings:
  - Structured terminal history remains the UI path.
  - No raw PTY journal/event-log was found in remote UI/copy-mode history.
  - No direct agent HTTP/WebSocket/filesystem shortcut was found for browser/app terminal/file/runtime data; the localhost WebSocket is a native app bridge into WebRTC-backed channels.
  - Go and TypeScript generated `auth_ttl_seconds` fields are present.

## tmux Verification

- Built an isolated test binary at `/tmp/termx-grid-e2e.zj2pYi/termx`.
- Used isolated `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, socket, and log under `/tmp/termx-grid-e2e.zj2pYi`.
- Disabled remote runtime for the local test daemon with `TERMX_REMOTE_ENABLE=false` to avoid touching the user's normal daemon or remote deployment.
- Confirmed TUI launch path:
  - bare `termx` launches the workspace TUI
  - `termx attach <id>` launches the TUI attached to an existing terminal
  - copy mode enters with `Ctrl-V`, top/bottom use `g`/`G`, exit uses `Esc`
- Stress/wrapped styled output:
  - Ran `python scripts/generate_terminal_stress.py --lines 100 --seed 100 --width-hint 120`.
  - Resized the tmux window from `120x36` to `90x28` and back to exercise owner resize.
  - Copy mode loaded a structured grid viewport for the styled/wrapped history and showed the first stress rows (`000000`, `000001`) after `Ctrl-V`, `g`.
- Tail preservation:
  - Ran deterministic short output `SIMPLE000001` through `SIMPLE000120`.
  - Live TUI and copy-mode bottom both showed `SIMPLE000100` and `SIMPLE000120`.
- Pagination continuity:
  - Created a clean terminal with `PAGE000001` through `PAGE001000`.
  - Live TUI showed `PAGE000978` through `PAGE001000` continuously.
  - Copy mode bottom showed `PAGE000931` through `PAGE001000` continuously.
  - Repeated `g` at the top triggered older grid viewport pages: visible windows moved continuously through `PAGE000728-000797`, `PAGE000478-000547`, `PAGE000228-000297`, and finally `PAGE000001-000070`; no gaps or duplicates were found in captured visible windows.
- TUI re-entry / persisted daemon history:
  - Killed the `termx-grid-e2e-page` tmux session, reattached to terminal `2`, entered copy mode, and paged to top.
  - The re-entered TUI still showed `PAGE000001-000070` continuously.
- QR output:
  - Created a terminal that ran `termx remote pair --ttl 30s --auth-ttl 1m`.
  - Live TUI showed the QR block and `termx://pair?payload=...`.
  - Copy mode showed the QR block at top and, at bottom, the full URI text plus `expires_at` / `authorization_ttl`.
- Capture logs are under `/tmp/termx-grid-e2e.zj2pYi/*.txt` for this run.

## Sub-Agent Review Summary

- Reviewer A: grid store / resize correctness. Issues fixed or recorded in Phase 2/4 review notes.
- Reviewer B: TUI/tmux behavior. Issues fixed in Phase 3.
- Reviewer C: storage recovery. Issues fixed or recorded in Phase 4.
- Reviewer D: remote/protobuf compatibility. Compatibility issue fixed in Phase 6.

## Test Results

- Latest focused/package results are listed in each phase section.
- Root `go test ./...` does not work in this workspace: `pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies`.
- PASS: `go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-hub/... ./termx-proto/... ./termx-remote/... ./termx-shared/... ./termx-testkit/... ./termx-vterm/... ./tuiv2/...`

## Follow-Up Hardening: tmux-like resize boundary and retention

- User follow-up after final verification: current model is close to tmux but remaining bugs still need fixing.
- Re-audited the documented unresolved risks:
  - raw grid-store duplicate rows when a wrapped logical line is split between history and visible screen during resize;
  - persistent grid history has no formal history-limit/retention cap.
- Fixed resize boundary behavior:
  - `VTerm.ResizeWithDamage` now uses a tail-screen resize path for width-shrink cases where the reflowed visible content exceeds the new screen height.
  - Rows that remain visible after resize are no longer appended to `damage.ScrollbackAppend`; only rows that leave the visible screen are written to `terminalGridStore`.
  - The stored prefix keeps `Wrapped=true` when the logical line continues into the visible screen, preserving copy-mode soft-wrap continuity across the history/screen boundary.
  - The tail-screen path resizes the emulator screen geometry directly and replaces the visible rows, instead of calling normal emulator `Resize` after pushing prefix rows. This avoids a second reflow pass that could pollute emulator scrollback when core-owned grid history is the source of truth.
  - Normal resize behavior remains covered by existing vterm resize tests.
- Fixed grid viewport reflow at a truncated wrapped boundary:
  - If a store page ends with a row whose committed metadata says it continues, the viewport preserves that final `Wrapped=true` instead of forcing it to false.
  - This prevents copy-mode/text selection from inserting a hard newline at a page boundary that is only a materialization boundary.
- Added persistent grid retention:
  - `terminalGridStore` now supports `maxRows`.
  - Terminal creation wires persistent grid retention to `ScrollbackSize`, making file-backed history follow the same bounded-history intent as tmux `history-limit`.
  - Retention rewrites the committed index to the newest rows and prunes unreferenced page files. `RowCount`/`TotalRows` report currently retained committed rows.
- Sub-agent note:
  - Attempted to spawn an additional focused review agent for this follow-up, but the session had reached the sub-agent limit. Work proceeded locally with focused regression tests.

## Follow-Up Commands

```sh
git status --short --branch
git log --oneline --decorate -12
rg -n "TODO|FIXME|remaining|risk|duplicate|ResizeWithDamage|ScrollbackAppend|history-limit|retention|removeOnClose|materialized" docs/workflows/grid-history-rebase-workflow.md termx-core termx-vterm tuiv2 internal remote-ui 2>/dev/null
go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamageDoesNotAppendVisibleSuffix|TestVTermResizeWithDamagePreservesWrappedBoundary|TestVTermResizeReflowsSoftWrappedRows' -count=1 -v
go test ./termx-core -run 'TestTerminalGridResizeDamageDoesNotPersistVisibleSuffix|TestTerminalGridStoreReflowPreservesTrailingWrappedContinuation|TestTerminalGridResizeDamageKeepsWrappedContinuity|TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamagePreservesStressTailRows|TestTerminalGridResizeDamagePreservesWideAndQRLikeRows' -count=1 -v
go test ./termx-core -run 'TestTerminalGridStoreRetentionCapsCommittedRows' -count=1 -v
go test ./termx-vterm/... ./termx-core/...
go test ./tuiv2/runtime/... ./tuiv2/app/...
go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-proto/... ./termx-vterm/... ./tuiv2/runtime/... ./tuiv2/app/...
```

## Follow-Up Test Results

- PASS: `go test ./termx-vterm/vterm -run 'TestVTermResizeWithDamageDoesNotAppendVisibleSuffix|TestVTermResizeWithDamagePreservesWrappedBoundary|TestVTermResizeReflowsSoftWrappedRows' -count=1 -v`
- PASS: `go test ./termx-core -run 'TestTerminalGridResizeDamageDoesNotPersistVisibleSuffix|TestTerminalGridStoreReflowPreservesTrailingWrappedContinuation|TestTerminalGridResizeDamageKeepsWrappedContinuity|TestTerminalSnapshotTrimsResizeGridScreenOverlap|TestTerminalGridResizeDamagePreservesStressTailRows|TestTerminalGridResizeDamagePreservesWideAndQRLikeRows' -count=1 -v`
- PASS: `go test ./termx-core -run 'TestTerminalGridStoreRetentionCapsCommittedRows' -count=1 -v`
- PASS: `go test ./termx-vterm/... ./termx-core/...`
- PASS: `go test ./tuiv2/runtime/... ./tuiv2/app/...`
- PASS: `go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-proto/... ./termx-vterm/... ./tuiv2/runtime/... ./tuiv2/app/...`

## tmux Structure Reference Backlog

This section records the current code-read comparison with tmux. The goal is not to clone tmux as a product, but to copy the mature parts of its terminal history model where TermX still has avoidable complexity or bug risk.

### Current Alignment

- UI, copy mode, and remote history use core-owned parsed grid/cell rows, not raw PTY journal replay.
- `Terminal.GridViewportWithOptions` serves history at the terminal canonical width, not at observer panel/floating/app viewport width.
- `terminalGridStore` preserves structured cells, style runs, wrapped metadata, row timestamps, row kinds, and crash-recoverable page/index commit semantics.
- Runtime/copy-mode materializes bounded viewport pages instead of growing a full in-memory scrollback snapshot without limit.
- Resize damage now appends only rows that leave the visible screen in the covered wrapped/tail cases.

### Gaps To Copy From tmux

- Unified row identity across history and visible screen:
  - tmux keeps history and visible screen in one `screen->grid` address space (`hsize + sy` rows).
  - TermX currently bridges `VTerm` live screen and `terminalGridStore` history with `WriteDamage.ScrollbackAppend`.
  - Follow-up: introduce a canonical row identity/generation at the core history boundary, even if storage remains file-backed pages. This should make page requests, resize append, retention, and overlap trimming reason about the same logical rows instead of matching visible rows by content/fingerprint.

- Resize as grid mutation rather than screen-diff inference:
  - tmux `grid_reflow` mutates one grid and moves the history/screen boundary naturally.
  - TermX computes resize damage from before/after screen rows.
  - Follow-up: refactor resize planning around an explicit logical-line/grid-row model before applying emulator resize. Keep the file-backed store, but make the append plan come from row movement decisions, not from post-resize screen matching.

- Copy-mode backing model:
  - tmux copy-mode has a backing `screen/grid` and a rendered view screen.
  - TermX copy-mode has a frozen protocol snapshot plus paged viewport materialization.
  - Follow-up: keep bounded pages, but track cursor/selection against canonical history row identity plus page generation. Avoid treating materialized snapshot length as the only coordinate truth.

- Wrapped logical-line navigation:
  - tmux consistently uses `GRID_LINE_WRAPPED` when moving by visual rows, logical lines, selections, and copy extraction.
  - TermX has wrapped metadata in store/snapshot/copy-mode, but bugs can still appear at page boundaries, resize boundaries, and history/screen boundaries.
  - Follow-up: add tmux-style wrapped-line helper functions in TermX for "logical line start/end", "previous/next logical line", and "selection string extraction"; make copy-mode and viewport trimming use those helpers instead of local ad hoc checks.

- History coordinate semantics:
  - tmux addresses rows in one backing grid using `screen_hsize`, `screen_size_y`, and `oy`.
  - TermX exposes `ScrollbackOffset`, `LoadedRows`, and `ScrollbackTotal` from newest-retained rows.
  - Follow-up: document and test the TermX coordinate contract as a tmux-inspired model: `total retained rows`, `newest-relative offset`, `loaded raw rows`, `materialized visual rows`, and `history generation`. Add protocol fields only if stale-page bugs remain.

- Screen/history boundary:
  - tmux does not need overlap trimming because there is one grid.
  - TermX still trims possible duplicates between grid history and live screen in `Snapshot(offset=0)`.
  - Follow-up: reduce overlap trimming to a defensive fallback by making resize/scroll append own the boundary explicitly.

- Retention behavior:
  - tmux `history-limit` drops oldest grid lines in the same backing grid.
  - TermX now caps committed rows by `ScrollbackSize`, rewrites index records, and prunes pages.
  - Follow-up: add tests for retention across wrapped logical lines, page boundary retention, resize after retention, and stale client requests after retention.

- Extraction/replay:
  - tmux extracts strings from grid cells (`grid_string_cells` / `grid_view_string_cells`) rather than replaying PTY bytes.
  - TermX UI history uses structured viewport, while `HistoryReplay` still encodes rows into an ANSI-like replay payload for older paths.
  - Follow-up: keep structured viewport as primary. Treat string replay as compatibility/debug unless a caller explicitly needs it. Add tests that copy-mode text extraction and remote viewport rendering do not depend on ANSI replay.

### Intentional Differences To Keep

- TermX should keep one real PTY size per terminal and many observer viewports; it should not copy tmux pane/window/session behavior wholesale.
- TermX should keep file-backed/recoverable history storage. tmux is primarily an in-memory grid; TermX needs restart recovery.
- TermX should keep remote/App/WebRTC structured viewport transport. Do not reintroduce raw PTY journal as UI history.
- TermX row metadata such as timestamps and row kinds is product-specific and should stay if it does not weaken grid consistency.

### Priority Order

1. Add a canonical history row identity/generation contract and focused stale-page tests.
2. Replace resize history append inference with an explicit tmux-like logical-row movement plan.
3. Centralize wrapped logical-line helper functions and use them in viewport, copy-mode movement, selection, and text extraction.
4. Harden retention around wrapped lines, page boundaries, resize, and stale clients.
5. Reduce `Snapshot(offset=0)` overlap trimming to a defensive fallback after boundary ownership is explicit.
6. Keep `HistoryReplay` compatibility, but move remaining UI/copy-mode callers toward structured rows only.

## tmux Reference Follow-Up Phase 1: History Coordinates

- Implemented a TermX equivalent of tmux backing-grid coordinates without copying tmux's in-memory grid:
  - `terminalGridStore` now tracks `baseRowID` and `historyGeneration` in store metadata.
  - `GridViewport` carries raw committed `LoadedRows`, `HistoryGeneration`, `FirstRowID`, and `LastRowID`.
  - `Snapshot` carries the same history coordinate metadata for its materialized scrollback window.
  - protobuf/binary `GridViewport` and `Snapshot` payloads now preserve these fields, and `remote-ui` generated wire bindings were refreshed.
- Runtime and copy-mode now use raw committed `LoadedRows` as the pagination depth when available. This avoids advancing history offsets by reflowed visual row count when one committed logical row materializes into multiple viewport rows.
- Runtime and copy-mode reject older pages when history generation differs or row IDs are not adjacent to the current materialized window.
- Remote terminal history replay now prefers the structured viewport `loaded_rows` over the legacy history replay frame's row count.

### Phase 1 Commands

```sh
protoc --go_out=termx-proto --go_opt=paths=source_relative -I termx-proto termx-proto/wirepb/terminal.proto
cd remote-ui && npm run proto:wire
go test ./internal/protocol -run 'TestGridViewportPayloadRoundTripUsesBinaryRows' -count=1 -v
go test ./termx-core -run 'TestTerminalGridStoreRetentionCapsCommittedRows|TestTerminalGridStoreViewportLoadedRowsUseRawCommittedRows' -count=1 -v
go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPageRejectsStaleHistoryGeneration|TestRuntimeApplyGridViewportPageRejectsNonAdjacentRowIDs|TestRuntimeApplyGridViewportPageKeepsBoundedWindow|TestRuntimeApplyGridViewportPagePrependsHistory' -count=1 -v
cd remote-ui && npm run test -- terminalProtocolClient.behavior.test.ts
go test ./internal/protocol/... ./termx-proto/... ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...
cd remote-ui && npm run typecheck
```

### Phase 1 Test Results

- PASS: `go test ./internal/protocol -run 'TestGridViewportPayloadRoundTripUsesBinaryRows' -count=1 -v`
- PASS: `go test ./termx-core -run 'TestTerminalGridStoreRetentionCapsCommittedRows|TestTerminalGridStoreViewportLoadedRowsUseRawCommittedRows' -count=1 -v`
- PASS: `go test ./tuiv2/runtime -run 'TestRuntimeApplyGridViewportPageRejectsStaleHistoryGeneration|TestRuntimeApplyGridViewportPageRejectsNonAdjacentRowIDs|TestRuntimeApplyGridViewportPageKeepsBoundedWindow|TestRuntimeApplyGridViewportPagePrependsHistory' -count=1 -v`
- PASS: `cd remote-ui && npm run test -- terminalProtocolClient.behavior.test.ts`
- PASS: `go test ./internal/protocol/... ./termx-proto/... ./termx-core/... ./tuiv2/runtime/... ./tuiv2/app/...`
- PASS: `cd remote-ui && npm run typecheck`

## Unresolved Risks

- Persistent grid retention is now row-count bounded via `ScrollbackSize`; no time-based retention or disk-byte cap is implemented yet.
- Resize no longer stores visible suffix duplicates for the covered wrapped split cases. The remaining architectural gap versus tmux is that TermX still bridges `vterm` screen and file-backed history through resize damage rather than a single in-memory/file-backed grid mutation with row identity.
