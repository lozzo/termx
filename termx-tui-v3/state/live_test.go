package state

import (
	"testing"
	"time"
)

func TestTerminalSurfaceApplySnapshotIsDetached(t *testing.T) {
	lines := []string{"one", "two"}
	screen := [][]LiveCell{{{Text: "one", Width: 3, FG: "ansi:2"}}}
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   7,
		Cols:       80,
		Rows:       24,
		Lines:      lines,
		Screen:     screen,
		Title:      "shell",
		Cursor:     LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
	})
	lines[0] = "mutated"
	screen[0][0].Text = "mutated"

	if store.TerminalID != "term-1" || store.Cols != 80 || store.Rows != 24 || store.Title != "shell" || !store.Ready {
		t.Fatalf("unexpected surface store %#v", store)
	}
	if store.Revision != 7 || store.Surfaces["term-1"].Revision != 7 {
		t.Fatalf("expected snapshot revision to be projected and cached, got %#v", store)
	}
	if !store.Cursor.Visible || store.Cursor.Row != 1 || store.Cursor.Col != 2 || store.Cursor.Shape != "bar" {
		t.Fatalf("expected detached cursor metadata, got %#v", store.Cursor)
	}
	if store.Lines[0] != "one" {
		t.Fatalf("expected detached lines, got %#v", store.Lines)
	}
	if store.Screen[0][0].Text != "one" || store.Screen[0][0].FG != "ansi:2" {
		t.Fatalf("expected detached live screen cells, got %#v", store.Screen)
	}
	cached := store.Surfaces["term-1"]
	if cached.Screen[0][0].Text != "one" || cached.Screen[0][0].FG != "ansi:2" {
		t.Fatalf("expected detached cached live screen cells, got %#v", cached.Screen)
	}
}

func TestTerminalSurfaceSnapshotReturnsDetachedPayload(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   7,
		Lines:      []string{"one"},
		Screen:     [][]LiveCell{{{Text: "one", Width: 3, FG: "ansi:2"}}},
	})

	snapshot := store.Snapshot()
	snapshot.Lines[0] = "mutated"
	snapshot.Screen[0][0].Text = "mutated"

	if store.Lines[0] != "one" {
		t.Fatalf("snapshot must not expose mutable store lines, got %#v", store.Lines)
	}
	if store.Screen[0][0].Text != "one" || store.Surfaces["term-1"].Screen[0][0].Text != "one" {
		t.Fatalf("snapshot must not expose mutable store cells, store=%#v cached=%#v", store.Screen, store.Surfaces["term-1"].Screen)
	}
}

func TestTerminalSurfaceRejectsStaleLiveRevision(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"new"},
	})
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   1,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"old"},
	})

	if store.Revision != 2 || store.Lines[0] != "new" {
		t.Fatalf("stale revision must not replace current projection, got %#v", store)
	}
	if cached := store.Surfaces["term-1"]; cached.Revision != 2 || cached.Lines[0] != "new" {
		t.Fatalf("stale revision must not replace cached surface, got %#v", cached)
	}
}

func TestTerminalSurfaceRejectsEmptyBootstrapOverContent(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"prompt"},
		Cursor:     LiveCursor{Visible: true, Row: 0, Col: 6, Shape: "bar"},
	})
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
	})

	if store.Revision != 3 || len(store.Lines) != 1 || store.Lines[0] != "prompt" || !store.Cursor.Visible {
		t.Fatalf("empty bootstrap snapshot must not replace current content, got %#v", store)
	}
	if cached := store.Surfaces["term-1"]; cached.Revision != 3 || len(cached.Lines) != 1 || cached.Lines[0] != "prompt" || !cached.Cursor.Visible {
		t.Fatalf("empty bootstrap snapshot must not replace cached content, got %#v", cached)
	}
}

func TestTerminalSurfaceAttachKeepsCachedRevision(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   5,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"connected panel"},
	})
	store = store.Attach("term-2", 80, 24)
	store = store.Attach("term-1", 80, 24)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
	})

	if store.Revision != 5 || len(store.Lines) != 1 || store.Lines[0] != "connected panel" {
		t.Fatalf("reattach must keep cached content when empty bootstrap arrives, got %#v", store)
	}
}

func TestTerminalSurfaceRestartPreservesCurrentContentAndClearsLifecycle(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   5,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"old live tail"},
		Cursor:     LiveCursor{Visible: true, Row: 0, Col: 13},
		Modes:      LiveTerminalModes{MouseTracking: true, BracketedPaste: true},
	})
	store = store.MarkExitedWithMetadata("term-1", 23, "exited", time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC), []string{"bash", "-lc", "exit 23"})

	store = store.RestartPreservingContent("term-1", 80, 24)

	if store.State != TerminalLiveAttached || store.ExitCode != 0 || !store.ExitedAt.IsZero() || len(store.Command) != 0 || store.Err != "" {
		t.Fatalf("restart should clear lifecycle metadata, got %#v", store)
	}
	if len(store.Lines) != 1 || store.Lines[0] != "old live tail" || !store.Ready {
		t.Fatalf("restart should keep live tail ready, got %#v", store)
	}
	if store.Cursor != (LiveCursor{}) || store.Modes != (LiveTerminalModes{}) {
		t.Fatalf("restart must drop old process cursor/modes, got cursor=%#v modes=%#v", store.Cursor, store.Modes)
	}
}

func TestTerminalSurfaceRestartNonCurrentOnlyUpdatesCache(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      []string{"current"},
	})
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-2",
		Cols:       40,
		Rows:       12,
		Lines:      []string{"other old tail"},
	})
	store = store.MarkExited("term-2", 9, "exited")

	store = store.RestartPreservingContent("term-2", 40, 12)

	if store.TerminalID != "term-1" || len(store.Lines) != 1 || store.Lines[0] != "current" {
		t.Fatalf("restart for non-current terminal must not switch projection, got %#v", store)
	}
	cached := store.Surfaces["term-2"]
	if cached.State != TerminalLiveAttached || cached.ExitCode != 0 || len(cached.Lines) != 1 || cached.Lines[0] != "other old tail" {
		t.Fatalf("restart should update only the target cache, got %#v", cached)
	}
}

func TestTerminalSurfaceResizeBoundaryRejectsLateOldSizeFrame(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"before resize"},
	})
	store = store.Resize(100, 40)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"late old size"},
	})
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      []string{"late unknown revision"},
	})

	if store.Cols != 100 || store.Rows != 40 || store.Lines[0] != "before resize" {
		t.Fatalf("late old-size frames must not roll back resized projection, got %#v", store)
	}
	if !store.ResizeBoundary.Active {
		t.Fatalf("resize boundary should remain until matching-size surface arrives, got %#v", store.ResizeBoundary)
	}
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       98,
		Rows:       38,
		Lines:      []string{"accepted non-old size"},
	})
	if store.Cols != 98 || store.Rows != 38 || store.Lines[0] != "accepted non-old size" {
		t.Fatalf("resize boundary should not reject non-old-size live frames, got %#v", store)
	}
	store = store.Resize(100, 40)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   4,
		Cols:       100,
		Rows:       40,
		Lines:      []string{"after resize"},
	})
	if store.ResizeBoundary.Active || store.Cols != 100 || store.Rows != 40 || store.Lines[0] != "after resize" {
		t.Fatalf("matching-size frame should clear resize boundary, got %#v", store)
	}
}

func TestTerminalSurfaceExitBoundaryRejectsLateOrdinaryFrame(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"last screen"},
	})
	store = store.MarkExitedWithMetadata("term-1", 0, "done", time.Time{}, nil)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"late ordinary"},
	})

	if store.State != TerminalLiveExited || store.ExitReason != "done" {
		t.Fatalf("late ordinary frame must not clear exit state, got %#v", store)
	}
	if store.Lines[0] != "last screen" {
		t.Fatalf("exit boundary should preserve last accepted screen, got %#v", store.Lines)
	}
}

func TestTerminalSurfaceOrdinaryRunningSnapshotDoesNotClearExitBoundary(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   9,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"terminal exited: term-1 code:0 exited"},
	})
	store = store.MarkExitedWithMetadata("term-1", 0, "exited", time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC), []string{"/bin/zsh"})
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"terminal exited: term-1 code:0 exited", "% "},
		Cursor:     LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
		State:      TerminalLiveAttached,
	})

	if store.State != TerminalLiveExited || store.ExitReason != "exited" || store.Revision != 9 {
		t.Fatalf("ordinary live snapshot must not clear exited boundary, got %#v", store)
	}
}

func TestTerminalSurfaceAttachAllowsFreshFrameAfterExitBoundary(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"last screen"},
	})
	store = store.MarkExitedWithMetadata("term-1", 0, "done", time.Time{}, nil)
	store = store.Attach("term-1", 80, 24)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   1,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"reattached"},
	})

	if store.State != TerminalLiveAttached || store.ExitReason != "" || store.Lines[0] != "reattached" {
		t.Fatalf("explicit attach boundary should accept a fresh ordinary frame, got %#v", store)
	}
}

func TestTerminalSurfaceMarkAttachedClearsBoundaryAndAcceptsCursorSnapshot(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"old live tail"},
	})
	store = store.MarkExitedWithMetadata("term-1", 0, "exited", time.Date(2026, 6, 17, 12, 40, 0, 0, time.UTC), []string{"/bin/zsh"})

	store = store.MarkAttached("term-1")
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"old live tail", "% "},
		Cursor:     LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
	})

	if store.State != TerminalLiveAttached || store.ExitReason != "" || !store.ExitedAt.IsZero() || len(store.Command) != 0 {
		t.Fatalf("running lifecycle should clear exited metadata, got %#v", store)
	}
	if len(store.Lines) != 2 || store.Lines[1] != "% " {
		t.Fatalf("fresh live snapshot should replace old tail projection, got %#v", store.Lines)
	}
	if !store.Cursor.Visible || store.Cursor.Row != 1 || store.Cursor.Col != 2 || store.Cursor.Shape != "bar" {
		t.Fatalf("fresh core cursor should be accepted after lifecycle clear, got %#v", store.Cursor)
	}
}

func TestTerminalSurfaceAttachProjectsCachedSnapshot(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"cached live"},
	})
	store = store.Attach("term-2", 100, 40)
	store = store.ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   4,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"cached update"},
	})
	store = store.Attach("term-1", 80, 24)

	if !store.Ready || store.TerminalID != "term-1" || store.Lines[0] != "cached update" {
		t.Fatalf("attach should immediately project cached live surface, got %#v", store)
	}
}

func TestTerminalSurfaceTracksMouseModesForPassthrough(t *testing.T) {
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Modes:      LiveTerminalModes{MouseTracking: true, MouseSGR: true},
	})
	if !store.Modes.MousePassthroughEnabled() || !store.SurfaceForTerminal("term-1").Modes.MouseSGR {
		t.Fatalf("expected mouse modes preserved on surface snapshot, got %#v", store)
	}
	store = store.Attach("term-2", 80, 24)
	if store.Modes.MousePassthroughEnabled() {
		t.Fatalf("attach switch must clear projected mouse modes, got %#v", store.Modes)
	}
	store = store.ApplySnapshot(LiveSurfaceSnapshot{TerminalID: "term-2", Modes: LiveTerminalModes{MouseNormal: true}})
	store = store.MarkExited("term-2", 0, "done")
	if store.Modes.MousePassthroughEnabled() {
		t.Fatalf("exited surface must clear mouse passthrough modes, got %#v", store.Modes)
	}
}

func TestTerminalSessionAttachResizeAndError(t *testing.T) {
	session := (TerminalSessionStore{}).Attach("term-1", 7, 80, 24)
	if !session.Attached || session.Channel != 7 || session.TerminalID != "term-1" || session.State != TerminalLiveAttached {
		t.Fatalf("unexpected attached session %#v", session)
	}
	session = session.Resize(100, 40)
	if session.Cols != 100 || session.Rows != 40 {
		t.Fatalf("unexpected resized session %#v", session)
	}
	session = session.SetError("boom")
	if session.LastError != "boom" || session.State != TerminalLiveError || session.Attached {
		t.Fatalf("expected session error, got %#v", session)
	}
}

func TestTerminalLiveLifecycleTracksAttachSwitchExitAndError(t *testing.T) {
	surface := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      []string{"old"},
	})
	surface = surface.Attach("term-2", 100, 40)
	if surface.TerminalID != "term-2" || surface.Ready || len(surface.Lines) != 0 || surface.State != TerminalLiveAttached {
		t.Fatalf("attach switch must clear stale surface projection, got %#v", surface)
	}
	surface = surface.MarkExited("term-2", 130, "closed")
	if surface.State != TerminalLiveExited || surface.ExitCode != 130 || surface.ExitReason != "closed" || surface.Cursor.Visible {
		t.Fatalf("expected exited surface lifecycle, got %#v", surface)
	}
	surface = surface.SetError("transport lost")
	if surface.State != TerminalLiveError || surface.Err != "transport lost" {
		t.Fatalf("expected surface error lifecycle, got %#v", surface)
	}

	session := (TerminalSessionStore{}).Attach("term-2", 3, 100, 40).MarkExited("term-2", 0, "done")
	if session.Attached || session.State != TerminalLiveExited || session.ExitReason != "done" || session.LastError != "" {
		t.Fatalf("expected exited session lifecycle, got %#v", session)
	}
}

func TestTerminalSessionTracksRequestedResizeAndRejectsStaleResults(t *testing.T) {
	session := (TerminalSessionStore{}).Attach("term-1", 7, 80, 24)
	session = session.RequestResize(40, 20)
	firstSeq := session.ResizeRequestSeq
	session = session.RequestResize(98, 36)
	if cols, rows := session.DesiredSize(); cols != 98 || rows != 36 {
		t.Fatalf("expected latest desired size, got %dx%d in %#v", cols, rows, session)
	}
	if !session.IsStaleResizeResult(firstSeq) {
		t.Fatalf("expected first resize result to be stale, got %#v", session)
	}
	next, applied := session.ApplyResizeResult(firstSeq, 40, 20)
	if applied || next.Cols != 80 || next.Rows != 24 || next.DesiredCols != 98 || next.DesiredRows != 36 {
		t.Fatalf("stale resize result must not mutate session, next=%#v applied=%v", next, applied)
	}
	next, applied = session.ApplyResizeResult(session.ResizeRequestSeq, 98, 36)
	if !applied || next.Cols != 98 || next.Rows != 36 || next.DesiredCols != 98 || next.DesiredRows != 36 {
		t.Fatalf("latest resize result must apply, next=%#v applied=%v", next, applied)
	}
}

func TestTerminalSessionTracksResizeOwner(t *testing.T) {
	session := (TerminalSessionStore{}).AttachWithResizeOwner("term-1", 7, 80, 24, "owner", "surface-1", "view-1")
	if session.ResizePolicy != "owner" || session.SurfaceID != "surface-1" || session.ViewID != "view-1" {
		t.Fatalf("expected resize owner metadata, got %#v", session)
	}
	if errored := session.SetError("boom"); errored.ResizePolicy != "" || errored.SurfaceID != "" || errored.ViewID != "" {
		t.Fatalf("error state must clear resize owner metadata, got %#v", errored)
	}
	if exited := session.MarkExited("term-1", 0, "done"); exited.ResizePolicy != "" || exited.SurfaceID != "" || exited.ViewID != "" {
		t.Fatalf("exit state must clear resize owner metadata, got %#v", exited)
	}
}

func TestTerminalSurfaceRefreshBackpressureCoalescesInFlightRequests(t *testing.T) {
	var store TerminalSurfaceStore
	var fetch bool
	store, fetch = store.RequestRefresh("term-1", 80, 24)
	if !fetch || !store.Refreshes["term-1"].InFlight || store.Refreshes["term-1"].Dirty {
		t.Fatalf("first refresh should start a fetch, store=%#v fetch=%v", store.Refreshes, fetch)
	}

	store, fetch = store.RequestRefresh("term-1", 96, 30)
	if fetch {
		t.Fatalf("in-flight refresh should not start a second fetch, store=%#v", store.Refreshes)
	}
	if refresh := store.Refreshes["term-1"]; !refresh.InFlight || !refresh.Dirty || refresh.Cols != 96 || refresh.Rows != 30 {
		t.Fatalf("in-flight refresh should keep latest dirty size, got %#v", refresh)
	}

	store = store.FinishRefresh("term-1")
	if refresh := store.Refreshes["term-1"]; refresh.InFlight || refresh.Dirty || refresh.Cols != 96 || refresh.Rows != 30 {
		t.Fatalf("finishing dirty fetch should keep a pending clean refresh, got %#v", refresh)
	}

	var cols, rows int
	store, cols, rows, fetch = store.ConsumeDirtyRefresh("term-1")
	if !fetch || cols != 96 || rows != 30 || !store.Refreshes["term-1"].InFlight {
		t.Fatalf("dirty refresh should schedule latest fetch, cols=%d rows=%d fetch=%v store=%#v", cols, rows, fetch, store.Refreshes)
	}

	store = store.FinishRefresh("term-1")
	if _, ok := store.Refreshes["term-1"]; ok {
		t.Fatalf("clean fetch completion should clear refresh state, store=%#v", store.Refreshes)
	}
}
