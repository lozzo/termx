package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestFloatingResizeUpdatesPTYSize(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1":     {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
			"term-float": {TerminalID: "term-float", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	tab := model.workbench.CurrentTab()

	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 12}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating terminal: %v", err)
	}
	model.runtime.Registry().GetOrCreate("term-float").Name = "float"
	model.runtime.Registry().Get("term-float").State = "running"
	model.runtime.Registry().Get("term-float").Channel = 7
	binding := model.runtime.BindPane("float-1")
	binding.Channel = 7
	binding.Connected = true

	if !model.workbench.ResizeFloatingPane(tab.ID, "float-1", 52, 18) {
		t.Fatal("expected floating resize to succeed")
	}

	drainCmd(t, model, model.resizeVisiblePanesCmd(), 20)

	var floatCall *resizeCall
	for i := range client.resizes {
		if client.resizes[i].channel == 7 {
			floatCall = &client.resizes[i]
			break
		}
	}
	if floatCall == nil {
		t.Fatalf("expected resize call for floating pane, got %#v", client.resizes)
	}
	if floatCall.cols != 50 || floatCall.rows != 16 {
		t.Fatalf("expected floating PTY resize to 50x16, got %dx%d", floatCall.cols, floatCall.rows)
	}
}

func TestFloatingFollowerResizeDoesNotUpdateSharedPTYSize(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	tab := model.workbench.CurrentTab()

	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 12}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-1"); err != nil {
		t.Fatalf("bind shared floating terminal: %v", err)
	}

	bodyRect := workbench.Rect{W: maxInt(1, model.width), H: maxInt(1, model.height-2)}
	visible := model.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatal("expected visible tiled pane")
	}
	ownerPane := visible.Tabs[visible.ActiveTab].Panes[0]
	ownerCols := uint16(maxInt(2, ownerPane.Rect.W-2))
	ownerRows := uint16(maxInt(2, ownerPane.Rect.H-2))

	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected shared terminal runtime")
	}
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "float-1"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: ownerCols, Rows: ownerRows},
	}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("float-1")
	followerBinding.Channel = 7
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	if !model.workbench.ResizeFloatingPane(tab.ID, "float-1", 52, 18) {
		t.Fatal("expected floating resize to succeed")
	}

	drainCmd(t, model, model.resizeVisiblePanesCmd(), 20)

	if len(client.resizes) != 0 {
		t.Fatalf("expected shared follower resize to avoid PTY resize, got %#v", client.resizes)
	}
}
