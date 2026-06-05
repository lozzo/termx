package state

import "testing"

func TestTerminalSurfaceApplySnapshotIsDetached(t *testing.T) {
	lines := []string{"one", "two"}
	screen := [][]LiveCell{{{Text: "one", Width: 3, FG: "ansi:2"}}}
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
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
	if !store.Cursor.Visible || store.Cursor.Row != 1 || store.Cursor.Col != 2 || store.Cursor.Shape != "bar" {
		t.Fatalf("expected detached cursor metadata, got %#v", store.Cursor)
	}
	if store.Lines[0] != "one" {
		t.Fatalf("expected detached lines, got %#v", store.Lines)
	}
	if store.Screen[0][0].Text != "one" || store.Screen[0][0].FG != "ansi:2" {
		t.Fatalf("expected detached live screen cells, got %#v", store.Screen)
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
