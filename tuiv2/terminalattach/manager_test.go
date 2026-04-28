package terminalattach

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const testSnapshotLimit = 500

func TestManagerExecuteBindsWorkbenchAndRuntimeOnSuccess(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 11, Mode: "collaborator"},
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			Name:  "demo",
			State: "running",
		}}},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			},
		},
	}
	wb := testWorkbenchWithPane("")
	rt := runtime.New(client)
	orch := orchestrator.New(wb)

	manager := NewManager(wb, rt, orch)
	msg, err := manager.Execute(context.Background(), Request{
		TabID:      "tab-1",
		PaneID:     "pane-1",
		TerminalID: "term-1",
		Mode:       "collaborator",
		Limit:      testSnapshotLimit,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if msg.TabID != "tab-1" || msg.PaneID != "pane-1" || msg.TerminalID != "term-1" || msg.Channel != 11 {
		t.Fatalf("unexpected attach result: %#v", msg)
	}
	pane := wb.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" || pane.Title != "demo" {
		t.Fatalf("expected workbench pane bound and titled from runtime metadata, got %#v", pane)
	}
	if binding := rt.Binding("pane-1"); binding == nil || !binding.Connected || binding.Channel != 11 {
		t.Fatalf("expected runtime binding connected after attach, got %#v", binding)
	}
}

func TestManagerExecuteRollsBackRuntimeAndWorkbenchOnLateFailure(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	wb := testWorkbenchWithPane("term-1")
	rt := runtime.New(client)
	oldTerminal := rt.Registry().GetOrCreate("term-1")
	oldTerminal.OwnerPaneID = "pane-1"
	oldTerminal.ControlPaneID = "pane-1"
	oldTerminal.BoundPaneIDs = []string{"pane-1"}
	oldTerminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}}},
	}
	rt.RefreshSnapshotFromVTerm("term-1")
	oldBinding := rt.BindPane("pane-1")
	oldBinding.Channel = 7
	oldBinding.Connected = true
	oldBinding.Role = runtime.BindingRoleOwner
	orch := orchestrator.New(wb)

	manager := NewManager(wb, rt, orch)
	_, err := manager.Execute(context.Background(), Request{
		PaneID:     "pane-1",
		TerminalID: "term-new",
		Mode:       "collaborator",
		Limit:      testSnapshotLimit,
		PreviousSelection: Selection{
			WorkspaceName: "main",
			ActiveTabID:   "tab-1",
			FocusedPaneID: "pane-1",
		},
	})
	if err == nil {
		t.Fatal("expected late attach failure")
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Channel != 7 || !binding.Connected || binding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected pane binding restored after late attach failure, got %#v", binding)
	}
	if oldTerminal.OwnerPaneID != "pane-1" || oldTerminal.ControlPaneID != "pane-1" || len(oldTerminal.BoundPaneIDs) != 1 || oldTerminal.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected old terminal control restored after late attach failure, got %#v", oldTerminal)
	}
	pane := wb.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected workbench binding to remain on original terminal, got %#v", pane)
	}
}

func TestManagerExecutePreparedSplitTargetRollsBackWorkbenchOnLateFailure(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	wb := testWorkbenchWithPane("")
	rt := runtime.New(client)
	orch := orchestrator.New(wb)
	selection := Selection{WorkspaceName: "main", ActiveTabID: "tab-1", FocusedPaneID: "pane-1"}
	tabID, paneID, err := orch.PrepareSplitAttachTarget("pane-1")
	if err != nil {
		t.Fatalf("PrepareSplitAttachTarget: %v", err)
	}

	manager := NewManager(wb, rt, orch)
	_, err = manager.Execute(context.Background(), Request{
		TabID:                 tabID,
		PaneID:                paneID,
		TerminalID:            "term-new",
		Mode:                  "collaborator",
		Limit:                 testSnapshotLimit,
		CleanupPreparedTarget: true,
		PreviousSelection:     selection,
	})
	if err == nil {
		t.Fatal("expected late attach failure")
	}
	tab := wb.CurrentTab()
	if tab == nil || tab.ID != "tab-1" {
		t.Fatalf("expected rollback to restore original tab, got %#v", tab)
	}
	if len(tab.Panes) != 1 || tab.Panes["pane-1"] == nil {
		t.Fatalf("expected rollback to remove prepared split pane, got %#v", tab.Panes)
	}
	if tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected rollback to restore original focused pane, got %#v", tab)
	}
}

func TestManagerExecuteRestoresOriginalFocusOnOrdinaryAttachLateFailure(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: "term-old", Title: "old"},
				"pane-2": {ID: "pane-2", Title: "empty"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	rt := runtime.New(client)
	orch := orchestrator.New(wb)

	manager := NewManager(wb, rt, orch)
	_, err := manager.Execute(context.Background(), Request{
		TabID:      "tab-1",
		PaneID:     "pane-2",
		TerminalID: "term-new",
		Mode:       "collaborator",
		Limit:      testSnapshotLimit,
		PreviousSelection: Selection{
			WorkspaceName: "main",
			ActiveTabID:   "tab-1",
			FocusedPaneID: "pane-1",
		},
	})
	if err == nil {
		t.Fatal("expected late attach failure")
	}
	tab := wb.CurrentTab()
	if tab == nil || tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected rollback to restore original active pane, got %#v", tab)
	}
}

func TestManagerExecuteLateFailureRestoresSharedTerminalFollowerTitles(t *testing.T) {
	client := &fakeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-shared",
			Name:  "shared-new",
			State: "running",
		}}},
		snapshotByID: map[string]*protocol.Snapshot{
			"term-shared": {
				TerminalID: "term-shared",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shared-old", TerminalID: "term-shared"},
				"pane-2": {ID: "pane-2", Title: "empty"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	rt := runtime.New(client)
	orch := orchestrator.New(wb)

	manager := NewManager(wb, rt, orch)
	_, err := manager.Execute(context.Background(), Request{
		TabID:      "tab-1",
		PaneID:     "pane-2",
		TerminalID: "term-shared",
		Mode:       "collaborator",
		Limit:      testSnapshotLimit,
		PreviousSelection: Selection{
			WorkspaceName: "main",
			ActiveTabID:   "tab-1",
			FocusedPaneID: "pane-1",
		},
	})
	if err == nil {
		t.Fatal("expected late attach failure")
	}
	tab := wb.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if pane := tab.Panes["pane-1"]; pane == nil || pane.Title != "shared-old" {
		t.Fatalf("expected rollback to restore follower pane title, got %#v", pane)
	}
}

type fakeClient struct {
	attachResult *protocol.AttachResult
	attachErr    error
	listResult   *protocol.ListResult
	snapshotByID map[string]*protocol.Snapshot
	snapshotErr  error
}

var _ bridge.Client = (*fakeClient)(nil)

func (c *fakeClient) Close() error { return nil }

func (c *fakeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return nil, nil
}

func (c *fakeClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (c *fakeClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (c *fakeClient) List(context.Context) (*protocol.ListResult, error) {
	if c.listResult == nil {
		return &protocol.ListResult{}, nil
	}
	return c.listResult, nil
}

func (c *fakeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, nil
}

func (c *fakeClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	if c.attachErr != nil {
		return nil, c.attachErr
	}
	return c.attachResult, nil
}

func (c *fakeClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	return c.snapshotByID[terminalID], nil
}

func (c *fakeClient) Input(context.Context, uint16, []byte) error { return nil }

func (c *fakeClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }

func (c *fakeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *fakeClient) Kill(context.Context, string) error { return nil }

func (c *fakeClient) Restart(context.Context, string) error { return nil }

func (c *fakeClient) CreateSession(context.Context, protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}

func (c *fakeClient) ListSessions(context.Context) (*protocol.ListSessionsResult, error) {
	return &protocol.ListSessionsResult{}, nil
}

func (c *fakeClient) GetSession(context.Context, string) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}

func (c *fakeClient) AttachSession(context.Context, protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}

func (c *fakeClient) DetachSession(context.Context, string, string) error { return nil }

func (c *fakeClient) ApplySession(context.Context, protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}

func (c *fakeClient) ReplaceSession(context.Context, protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	return &protocol.SessionSnapshot{}, nil
}

func (c *fakeClient) UpdateSessionView(context.Context, protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	return &protocol.ViewInfo{}, nil
}

func (c *fakeClient) AcquireSessionLease(context.Context, protocol.AcquireSessionLeaseParams) (*protocol.LeaseInfo, error) {
	return nil, nil
}

func (c *fakeClient) ReleaseSessionLease(context.Context, protocol.ReleaseSessionLeaseParams) error {
	return nil
}

func testWorkbenchWithPane(terminalID string) *workbench.Workbench {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: terminalID},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	return wb
}
