package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestResizePendingPaneResizesClearsMissingPaneEntries(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.markPendingPaneResize("tab-1", "pane-missing", "term-1")

	cmd := model.resizePendingPaneResizesCmd()
	if cmd != nil {
		drainCmd(t, model, cmd, 20)
	}
	if _, ok := model.pendingPaneResizes["pane-missing"]; ok {
		t.Fatalf("expected missing pane pending resize to be cleared, got %#v", model.pendingPaneResizes)
	}
}

func TestResizePendingPaneResizesKeepsPendingUntilSizeSatisfied(t *testing.T) {
	client := &recordingBridgeClient{}
	model := setupModel(t, modelOpts{client: client})
	model.width = 120
	model.height = 40

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	terminal := model.runtime.Registry().GetOrCreate(pane.TerminalID)
	terminal.Tags = map[string]string{terminalmeta.SizeLockTag: terminalmeta.SizeLockLock}

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	model.markPendingPaneResize(tab.ID, pane.ID, pane.TerminalID)

	cmd := model.resizePendingPaneResizesCmd()
	if cmd == nil {
		t.Fatal("expected pending resize command")
	}
	drainCmd(t, model, cmd, 20)
	if _, ok := model.pendingPaneResizes[pane.ID]; !ok {
		t.Fatalf("expected locked terminal to keep pending resize, got %#v", model.pendingPaneResizes)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected locked terminal not to issue resize, got %#v", client.resizes)
	}

	delete(terminal.Tags, terminalmeta.SizeLockTag)
	cmd = model.resizePendingPaneResizesCmd()
	if cmd == nil {
		t.Fatal("expected retry resize command after unlock")
	}
	drainCmd(t, model, cmd, 20)
	if _, ok := model.pendingPaneResizes[pane.ID]; ok {
		t.Fatalf("expected pending resize cleared after satisfied resize, got %#v", model.pendingPaneResizes)
	}
	if len(client.resizes) == 0 {
		t.Fatal("expected unlocked retry to issue resize")
	}
}

func TestTerminalAlreadySizedIgnoresProvisionalPreviewSnapshot(t *testing.T) {
	client := &recordingBridgeClient{}
	model := setupModel(t, modelOpts{client: client, width: 80, height: 24})
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	terminal := model.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	terminal.VTerm = localvterm.New(98, 28, 100, nil)
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: pane.TerminalID,
		Size:       protocol.Size{Cols: 78, Rows: 20},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "p", Width: 1}}}},
	}
	terminal.PreferSnapshot = true
	terminal.ResizePreviewSource = &protocol.Snapshot{TerminalID: pane.TerminalID, Size: protocol.Size{Cols: 98, Rows: 28}}

	if model.terminalAlreadySized(pane.TerminalID, 78, 20) {
		t.Fatal("expected provisional preview snapshot not to satisfy terminal size")
	}

	service := model.layoutResizeService()
	if err := service.ensurePaneTerminalSize(context.Background(), pane.ID, pane.TerminalID, workbench.Rect{W: 80, H: 22}); err != nil {
		t.Fatalf("ensure pane size: %v", err)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected resize despite matching preview snapshot size, got %#v", client.resizes)
	}
}

func TestSyncActivePaneTabSwitchTakeoverMarksPendingWhenResizeStillUnsatisfied(t *testing.T) {
	client := &recordingBridgeClient{}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "shared", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-2"),
					},
				},
			},
		},
	})
	model.width = 120
	model.height = 40

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Tags = map[string]string{terminalmeta.SizeLockTag: terminalmeta.SizeLockLock}
	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true

	if cmd := model.switchTabByIndexMouse(1); cmd == nil {
		t.Fatal("expected switch-tab command")
	} else {
		drainCmd(t, model, cmd, 20)
	}
	if _, ok := model.pendingPaneResizes["pane-2"]; !ok {
		t.Fatalf("expected unsatisfied tab-switch resize to remain pending, got %#v", model.pendingPaneResizes)
	}
}
