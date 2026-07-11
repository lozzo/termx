package core

import (
	"errors"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestWorkbenchStoreDefaultAndMutations(t *testing.T) {
	store := newWorkbenchStore()
	snapshot, err := store.get("")
	if err != nil {
		t.Fatalf("default snapshot: %v", err)
	}
	if snapshot.Version != 1 || snapshot.ActiveWorkspaceID != "workspace-main" {
		t.Fatalf("unexpected default snapshot: %+v", snapshot)
	}
	if len(snapshot.Workspaces) != 1 || len(snapshot.Workspaces[0].Tabs) != 1 || len(snapshot.Workspaces[0].Tabs[0].Panes) != 1 {
		t.Fatalf("default workbench tree is incomplete: %+v", snapshot)
	}

	result, change, err := store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationWorkspaceCreate,
		TargetID:        "workspace-work",
		Name:            "work",
		CheckVersion:    true,
		ExpectedVersion: snapshot.Version,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if result.Snapshot.Version != 2 || result.ResourceID != "workspace-work" || change.WorkspaceID != "workspace-work" {
		t.Fatalf("unexpected create result: result=%+v change=%+v", result, change)
	}

	result, _, err = store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationTabCreate,
		WorkspaceID:     "workspace-work",
		TargetID:        "tab-build",
		Name:            "build",
		CheckVersion:    true,
		ExpectedVersion: result.Snapshot.Version,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}

	result, _, err = store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneSplit,
		WorkspaceID:     "workspace-work",
		TabID:           "tab-build",
		PaneID:          result.Snapshot.Workspaces[1].Tabs[1].ActivePaneID,
		TargetID:        "pane-logs",
		Name:            "logs",
		SplitDirection:  protocol.WorkbenchSplitVertical,
		CheckVersion:    true,
		ExpectedVersion: result.Snapshot.Version,
	})
	if err != nil {
		t.Fatalf("split pane: %v", err)
	}
	tab := result.Snapshot.Workspaces[1].Tabs[1]
	if tab.ActivePaneID != "pane-logs" || len(tab.Panes) != 2 || tab.RootSplit.Direction != protocol.WorkbenchSplitVertical {
		t.Fatalf("unexpected split tab: %+v", tab)
	}

	result, _, err = store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneBindTerminal,
		WorkspaceID:     "workspace-work",
		TabID:           "tab-build",
		PaneID:          "pane-logs",
		TerminalID:      "terminal-1",
		CheckVersion:    true,
		ExpectedVersion: result.Snapshot.Version,
	})
	if err != nil {
		t.Fatalf("bind terminal: %v", err)
	}
	tab = result.Snapshot.Workspaces[1].Tabs[1]
	pane, ok := findWorkbenchPane(tab.Panes, "pane-logs")
	if !ok || pane.TerminalID != "terminal-1" || pane.Kind != protocol.WorkbenchPaneTerminalLive {
		t.Fatalf("terminal bind not persisted: %+v", tab.Panes)
	}

	_, _, err = store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneRename,
		WorkspaceID:     "workspace-work",
		TabID:           "tab-build",
		PaneID:          "pane-logs",
		Name:            "stale",
		CheckVersion:    true,
		ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrWorkbenchVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkbenchStoreDeleteKeepsValidActiveIDs(t *testing.T) {
	store := newWorkbenchStore()
	result, _, err := store.apply(protocol.WorkbenchMutateParams{Action: protocol.WorkbenchMutationPaneSplit, TargetID: "pane-two"})
	if err != nil {
		t.Fatalf("split pane: %v", err)
	}
	result, _, err = store.apply(protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneDelete,
		PaneID:          "pane-two",
		CheckVersion:    true,
		ExpectedVersion: result.Snapshot.Version,
	})
	if err != nil {
		t.Fatalf("delete pane: %v", err)
	}
	tab := result.Snapshot.Workspaces[0].Tabs[0]
	if tab.ActivePaneID != "pane-main" || len(tab.Panes) != 1 || tab.RootSplit.PaneID != "pane-main" {
		t.Fatalf("delete did not collapse tab correctly: %+v", tab)
	}
}
