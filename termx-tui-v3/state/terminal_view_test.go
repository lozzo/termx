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

func TestTerminalViewStoreTransfersResizeOwnerWithinTerminal(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))
	store = store.BindPane(NewPaneTerminalView("pane-3", "term-2", 9, 100, 30, TerminalResizeRoleOwner, "surface", "view-3", true))

	store = store.TransferPaneResizeOwner("pane-2")
	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	third, _ := store.PaneBinding("pane-3")
	if first.ResizeRole != TerminalResizeRoleFollower || first.CanResize {
		t.Fatalf("previous owner should become follower, got %#v", first)
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize {
		t.Fatalf("target pane should become owner, got %#v", second)
	}
	if third.ResizeRole != TerminalResizeRoleOwner || !third.CanResize {
		t.Fatalf("unrelated terminal owner should be preserved, got %#v", third)
	}
}

func TestTerminalViewStoreResizeRequestRequiresOwnerAndGuardsStaleResults(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	unchanged, follower := store.RequestPaneResize("pane-2", 50, 14)
	if follower.Allowed || follower.Changed || follower.Reason != "not-owner" {
		t.Fatalf("follower must not request PTY resize, decision=%#v", follower)
	}
	if got, _ := unchanged.PaneBinding("pane-2"); got.DesiredCols != 40 || got.DesiredRows != 12 || got.RequestSeq != 0 {
		t.Fatalf("follower resize must not mutate binding, got %#v", got)
	}

	next, owner := store.RequestPaneResize("pane-1", 100, 30)
	if !owner.Allowed || !owner.Changed || owner.Seq != 1 {
		t.Fatalf("owner resize should advance request seq, decision=%#v", owner)
	}
	stale, applied := next.ApplyResizeResult("view-1", 0, 90, 20, "")
	if !applied {
		t.Fatalf("zero seq bootstrap result should apply")
	}
	current, _ := stale.PaneBinding("pane-1")
	if current.DesiredCols != 90 || current.DesiredRows != 20 {
		t.Fatalf("bootstrap result should update desired size, got %#v", current)
	}
	stale, newer := stale.RequestPaneResize("pane-1", 110, 32)
	if !newer.Changed || newer.Seq != 2 {
		t.Fatalf("newer owner resize should advance pending seq, decision=%#v", newer)
	}
	stale, applied = stale.ApplyResizeResult("view-1", 1, 80, 24, "")
	if applied {
		t.Fatalf("old result should be stale after newer request")
	}
	current, _ = stale.PaneBinding("pane-1")
	if current.DesiredCols != 110 || current.DesiredRows != 32 {
		t.Fatalf("stale result must not overwrite desired size, got %#v", current)
	}
}
