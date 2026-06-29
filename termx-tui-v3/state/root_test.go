package state

import "testing"

func TestRootWithoutCopyHistorySessionDropsCurrentWindowReferences(t *testing.T) {
	viewID := TerminalPaneViewID(DefaultPaneID)
	root := Root{
		History: HistoryStore{
			PaneID:      DefaultPaneID,
			ViewID:      viewID,
			TerminalID:  "term-1",
			Token:       "tok-1",
			SourceLines: []HistoryLogicalLine{{Text: "history", LineID: 1}},
			Rows:        []HistoryRow{{Text: "history", LineID: 1}},
		},
		CopyMode: CopyModeStore{
			Active:     true,
			PaneID:     DefaultPaneID,
			ViewID:     viewID,
			TerminalID: "term-1",
			BoundToken: "tok-1",
		},
	}
	root = root.WithCopyHistorySession(viewID, root.History, root.CopyMode)

	root = root.WithoutCopyHistorySession(viewID)

	if root.History.TerminalID != "" || root.History.Token != "" || len(root.History.Rows) != 0 || len(root.History.SourceLines) != 0 {
		t.Fatalf("current history must not retain released copy window, got %#v", root.History)
	}
	if root.CopyMode.Active || root.CopyMode.TerminalID != "" || root.CopyMode.BoundToken != "" {
		t.Fatalf("current copy mode must be cleared with released view, got %#v", root.CopyMode)
	}
	if _, ok := root.HistoryByView[viewID]; ok {
		t.Fatalf("history session map must drop released view, got %#v", root.HistoryByView)
	}
	if _, ok := root.CopyModeByView[viewID]; ok {
		t.Fatalf("copy session map must drop released view, got %#v", root.CopyModeByView)
	}
	if ids := root.CopyHistoryTerminalIDs(); len(ids) != 0 {
		t.Fatalf("released copy history must not leak terminal ids, got %#v", ids)
	}
}
