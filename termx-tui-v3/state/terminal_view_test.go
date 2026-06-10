package state

import "testing"

func TestTerminalViewStoreKeepsMultipleViewsForSameTerminal(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	bindings := store.BindingsForTerminal("term-1")
	if len(bindings) != 2 {
		t.Fatalf("expected two view bindings for one terminal, got %#v", bindings)
	}
	first, ok := store.PaneBinding("pane-1")
	if !ok || first.Channel != 7 || first.ResizeRole != TerminalResizeRoleOwner {
		t.Fatalf("unexpected owner binding %#v ok=%v", first, ok)
	}
	second, ok := store.PaneBinding("pane-2")
	if !ok || second.Channel != 8 || second.ResizeRole != TerminalResizeRoleFollower {
		t.Fatalf("unexpected follower binding %#v ok=%v", second, ok)
	}
}

func TestTerminalViewStoreDetachAndKillHaveDifferentScope(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	store = store.DetachPane("pane-1")
	if _, ok := store.PaneBinding("pane-1"); ok {
		t.Fatal("detached pane should lose its view binding")
	}
	if _, ok := store.PaneBinding("pane-2"); !ok {
		t.Fatal("sibling pane binding should survive detach")
	}

	store = store.RemoveTerminal("term-1")
	if bindings := store.BindingsForTerminal("term-1"); len(bindings) != 0 {
		t.Fatalf("kill terminal should remove all view bindings, got %#v", bindings)
	}
}
