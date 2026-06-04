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
	})
	lines[0] = "mutated"

	if store.TerminalID != "term-1" || store.Cols != 80 || store.Rows != 24 || store.Title != "shell" {
		t.Fatalf("unexpected surface store %#v", store)
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
