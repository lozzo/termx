package state

import "testing"

func TestTerminalSurfaceApplySnapshotIsDetached(t *testing.T) {
	lines := []string{"one", "two"}
	store := (TerminalSurfaceStore{}).ApplySnapshot(LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      lines,
		Title:      "shell",
		Cursor:     LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
	})
	lines[0] = "mutated"

	if store.TerminalID != "term-1" || store.Cols != 80 || store.Rows != 24 || store.Title != "shell" || !store.Ready {
		t.Fatalf("unexpected surface store %#v", store)
	}
	if !store.Cursor.Visible || store.Cursor.Row != 1 || store.Cursor.Col != 2 || store.Cursor.Shape != "bar" {
		t.Fatalf("expected detached cursor metadata, got %#v", store.Cursor)
	}
	if store.Lines[0] != "one" {
		t.Fatalf("expected detached lines, got %#v", store.Lines)
	}
}

func TestTerminalSessionAttachResizeAndError(t *testing.T) {
	session := (TerminalSessionStore{}).Attach("term-1", 7, 80, 24)
	if !session.Attached || session.Channel != 7 || session.TerminalID != "term-1" {
		t.Fatalf("unexpected attached session %#v", session)
	}
	session = session.Resize(100, 40)
	if session.Cols != 100 || session.Rows != 40 {
		t.Fatalf("unexpected resized session %#v", session)
	}
	session = session.SetError("boom")
	if session.LastError != "boom" {
		t.Fatalf("expected session error, got %#v", session)
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
