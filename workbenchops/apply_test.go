package workbenchops

import (
	"testing"

	"github.com/lozzow/termx/workbenchdoc"
)

func TestApplyIsAtomic(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbenchdoc.Tab{{
					ID:           "tab-1",
					Name:         "1",
					Root:         workbenchdoc.NewLeaf("pane-1"),
					Panes:        map[string]*workbenchdoc.Pane{"pane-1": {ID: "pane-1"}},
					ActivePaneID: "pane-1",
				}},
			},
		},
	}
	_, err := Apply(doc, []Op{
		{Kind: OpSplitPane, TabID: "tab-1", PaneID: "pane-1", NewPaneID: "pane-2", Direction: workbenchdoc.SplitVertical},
		{Kind: OpBindTerminal, TabID: "tab-1", PaneID: "missing", TerminalID: "term-1"},
	})
	if err == nil {
		t.Fatal("expected apply error")
	}
	tab := doc.Workspaces["main"].Tabs[0]
	if tab.Panes["pane-2"] != nil {
		t.Fatal("expected original doc to remain unchanged")
	}
}

func TestApplySplitAndBindTerminal(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbenchdoc.Tab{{
					ID:           "tab-1",
					Name:         "1",
					Root:         workbenchdoc.NewLeaf("pane-1"),
					Panes:        map[string]*workbenchdoc.Pane{"pane-1": {ID: "pane-1"}},
					ActivePaneID: "pane-1",
				}},
			},
		},
	}
	next, err := Apply(doc, []Op{
		{Kind: OpSplitPane, TabID: "tab-1", PaneID: "pane-1", NewPaneID: "pane-2", Direction: workbenchdoc.SplitVertical},
		{Kind: OpBindTerminal, TabID: "tab-1", PaneID: "pane-2", TerminalID: "term-2"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tab := next.Workspaces["main"].Tabs[0]
	if tab.Panes["pane-2"] == nil {
		t.Fatal("expected new pane")
	}
	if got := tab.Panes["pane-2"].TerminalID; got != "term-2" {
		t.Fatalf("expected terminal binding term-2, got %q", got)
	}
	if tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected active pane pane-2, got %q", tab.ActivePaneID)
	}
}

func TestApplyWorkspaceRenameUpdatesCurrentAndOrder(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {Name: "main", ActiveTab: -1},
		},
	}
	next, err := Apply(doc, []Op{{Kind: OpRenameWorkspace, WorkspaceName: "main", NewName: "dev"}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if next.CurrentWorkspace != "dev" {
		t.Fatalf("expected current workspace dev, got %q", next.CurrentWorkspace)
	}
	if len(next.WorkspaceOrder) != 1 || next.WorkspaceOrder[0] != "dev" {
		t.Fatalf("unexpected workspace order: %#v", next.WorkspaceOrder)
	}
	if next.Workspaces["dev"] == nil {
		t.Fatal("expected renamed workspace entry")
	}
}

func TestApplyCreateTabRejectsDuplicateNameInWorkspace(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbenchdoc.Tab{{
					ID:           "tab-1",
					Name:         "1",
					Root:         workbenchdoc.NewLeaf("pane-1"),
					Panes:        map[string]*workbenchdoc.Pane{"pane-1": {ID: "pane-1"}},
					ActivePaneID: "pane-1",
				}},
			},
		},
	}

	if _, err := Apply(doc, []Op{{Kind: OpCreateTab, WorkspaceName: "main", TabID: "tab-2", TabName: "1"}}); err == nil {
		t.Fatal("expected duplicate tab name error")
	}
}

func TestApplyRenameTabRejectsDuplicateNameInWorkspace(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbenchdoc.Tab{
					{
						ID:           "tab-1",
						Name:         "one",
						Root:         workbenchdoc.NewLeaf("pane-1"),
						Panes:        map[string]*workbenchdoc.Pane{"pane-1": {ID: "pane-1"}},
						ActivePaneID: "pane-1",
					},
					{
						ID:           "tab-2",
						Name:         "two",
						Root:         workbenchdoc.NewLeaf("pane-2"),
						Panes:        map[string]*workbenchdoc.Pane{"pane-2": {ID: "pane-2"}},
						ActivePaneID: "pane-2",
					},
				},
			},
		},
	}

	if _, err := Apply(doc, []Op{{Kind: OpRenameTab, TabID: "tab-1", NewName: "two"}}); err == nil {
		t.Fatal("expected duplicate tab name error")
	}
}
