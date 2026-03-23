package tui

import "testing"

func TestTerminalConnectionSnapshotReportsOwnerAndFollower(t *testing.T) {
	tabs := []*Tab{
		{
			Panes: map[string]*Pane{
				"pane-a": {
					ID: "pane-a",
					Viewport: &Viewport{
						TerminalID:     "term-1",
						ResizeAcquired: true,
					},
				},
			},
		},
		{
			Panes: map[string]*Pane{
				"pane-b": {
					ID: "pane-b",
					Viewport: &Viewport{
						TerminalID: "term-1",
					},
				},
			},
		},
	}

	snapshot, ok := buildTerminalConnectionSnapshot(tabs, "term-1")
	if !ok {
		t.Fatal("expected connection snapshot")
	}
	if got := snapshot.PaneCount(); got != 2 {
		t.Fatalf("expected 2 panes, got %d", got)
	}
	if got := snapshot.ResolvedOwnerID(); got != "pane-a" {
		t.Fatalf("expected pane-a owner, got %q", got)
	}
	if got := snapshot.StatusForPane("pane-a"); got != "owner" {
		t.Fatalf("expected pane-a owner status, got %q", got)
	}
	if got := snapshot.StatusForPane("pane-b"); got != "follower" {
		t.Fatalf("expected pane-b follower status, got %q", got)
	}
}

func TestTerminalConnectionSnapshotFallsBackToStableOwnerOrder(t *testing.T) {
	tabs := []*Tab{
		{
			Panes: map[string]*Pane{
				"pane-b": {
					ID: "pane-b",
					Viewport: &Viewport{
						TerminalID: "term-1",
					},
				},
				"pane-a": {
					ID: "pane-a",
					Viewport: &Viewport{
						TerminalID: "term-1",
					},
				},
			},
		},
	}

	snapshot, ok := buildTerminalConnectionSnapshot(tabs, "term-1")
	if !ok {
		t.Fatal("expected connection snapshot")
	}
	if got := snapshot.ResolvedOwnerID(); got != "pane-a" {
		t.Fatalf("expected stable fallback owner pane-a, got %q", got)
	}
	if got := snapshot.PreferredOwnerID("pane-a"); got != "pane-b" {
		t.Fatalf("expected preferred fallback to pane-b when excluding pane-a, got %q", got)
	}
}

func TestEnsureTerminalResizeOwnerNormalizesDuplicateOwners(t *testing.T) {
	tabs := []*Tab{
		{
			Panes: map[string]*Pane{
				"pane-a": {
					ID: "pane-a",
					Viewport: &Viewport{
						TerminalID:     "term-1",
						ResizeAcquired: true,
					},
				},
			},
		},
		{
			Panes: map[string]*Pane{
				"pane-b": {
					ID: "pane-b",
					Viewport: &Viewport{
						TerminalID:     "term-1",
						ResizeAcquired: true,
					},
				},
			},
		},
	}

	ensureTerminalResizeOwner(tabs, "term-1", "pane-b")

	if !tabs[1].Panes["pane-b"].ResizeAcquired {
		t.Fatal("expected pane-b to remain owner")
	}
	if tabs[0].Panes["pane-a"].ResizeAcquired {
		t.Fatal("expected pane-a owner flag cleared")
	}
}

func TestTerminalConnectionSnapshotPreferredOwnerReturnsEmptyWhenOnlyExcludedPaneRemains(t *testing.T) {
	tabs := []*Tab{
		{
			Panes: map[string]*Pane{
				"pane-a": {
					ID: "pane-a",
					Viewport: &Viewport{
						TerminalID:     "term-1",
						ResizeAcquired: true,
					},
				},
			},
		},
	}

	snapshot, ok := buildTerminalConnectionSnapshot(tabs, "term-1")
	if !ok {
		t.Fatal("expected connection snapshot")
	}
	if got := snapshot.PreferredOwnerID("pane-a"); got != "" {
		t.Fatalf("expected no preferred owner when only excluded pane remains, got %q", got)
	}
}
