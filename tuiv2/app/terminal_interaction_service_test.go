package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestTerminalInteractionServiceResolveTargetHonorsRequestOverrides(t *testing.T) {
	model := setupModel(t, modelOpts{})
	service := model.terminalInteractionService()
	if service == nil {
		t.Fatal("expected terminal interaction service")
	}

	overrideRect := workbench.Rect{X: 3, Y: 4, W: 50, H: 12}
	target, ok := service.resolveTarget(terminalInteractionRequest{
		PaneID:     "pane-1",
		TerminalID: "term-override",
		Rect:       overrideRect,
	})
	if !ok {
		t.Fatal("expected target resolution to succeed")
	}
	if target.paneID != "pane-1" {
		t.Fatalf("expected pane-1 target, got %#v", target)
	}
	if target.terminalID != "term-override" {
		t.Fatalf("expected terminal override, got %#v", target)
	}
	if target.rect != overrideRect {
		t.Fatalf("expected rect override %#v, got %#v", overrideRect, target.rect)
	}
}

func TestTerminalInteractionServiceShouldAcquireLocalOwnershipRequiresSharedVisibleCursor(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-2",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
					},
					Root: &workbench.LayoutNode{
						Direction: workbench.SplitVertical,
						Ratio:     0.5,
						First:     workbench.NewLeaf("pane-1"),
						Second:    workbench.NewLeaf("pane-2"),
					},
				}},
			},
		},
	})
	service := model.terminalInteractionService()
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Cursor:     protocol.CursorState{Visible: true},
	}
	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner
	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	target := terminalInteractionTarget{
		paneID:     "pane-2",
		terminalID: "term-1",
		rect:       workbench.Rect{W: 40, H: 20},
	}
	req := terminalInteractionRequest{ImplicitInteractiveOwner: true}
	if !service.shouldAcquireLocalOwnership(req, target) {
		t.Fatal("expected visible shared cursor to trigger local ownership acquire")
	}

	terminal.Snapshot.Cursor.Visible = false
	if service.shouldAcquireLocalOwnership(req, target) {
		t.Fatal("expected hidden cursor to skip local ownership acquire")
	}
}

func TestTerminalInteractionServiceSyncExplicitSessionTakeoverAcquiresLeaseAndResizes(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-2",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: &workbench.LayoutNode{
						Direction: workbench.SplitVertical,
						Ratio:     0.5,
						First:     workbench.NewLeaf("pane-1"),
						Second:    workbench.NewLeaf("pane-2"),
					},
				}},
			},
		},
	})
	model.sessionID = "session-main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "session-main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	_ = model.workbench.FocusPane("tab-1", "pane-2")

	service := model.terminalInteractionService()
	req := terminalInteractionRequest{PaneID: "pane-2", ResizeIfNeeded: true, ExplicitTakeover: true}
	target, ok := service.resolveTarget(req)
	if !ok {
		t.Fatal("expected target resolution to succeed")
	}
	if err := service.sync(context.Background(), req, target); err != nil {
		t.Fatalf("sync explicit takeover: %v", err)
	}

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected one lease acquire, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-2" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 to become owner, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected roles to swap after lease acquire, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected one resize after explicit takeover, got %#v", client.resizes)
	}
	if client.resizes[0].channel != 2 {
		t.Fatalf("expected pane-2 channel resize, got %#v", client.resizes[0])
	}
	if lease := model.sessionLeases["term-1"]; lease.ViewID != "view-local" || lease.PaneID != "pane-2" {
		t.Fatalf("expected local lease stored after acquire, got %#v", lease)
	}
}

func TestTerminalInteractionServiceShouldAcquireSessionLeaseForExplicitAndImplicitPaths(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.sessionID = "session-main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "session-main", ViewID: "view-remote", PaneID: "pane-1"},
	}
	service := model.terminalInteractionService()
	target := terminalInteractionTarget{paneID: "pane-1", terminalID: "term-1"}

	if !service.shouldAcquireSessionLease(terminalInteractionRequest{ExplicitTakeover: true}, target) {
		t.Fatal("expected explicit takeover to require session lease acquire")
	}
	if !service.shouldAcquireSessionLease(terminalInteractionRequest{ImplicitSessionLease: true}, target) {
		t.Fatal("expected implicit same-pane remote lease to require acquire")
	}
	if service.shouldAcquireSessionLease(terminalInteractionRequest{}, target) {
		t.Fatal("expected plain interaction not to acquire session lease")
	}
}

func TestTerminalInteractionServiceResizeIfNeededForcesPendingOwnerResize(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client})
	service := model.terminalInteractionService()
	target, ok := service.resolveTarget(terminalInteractionRequest{PaneID: "pane-1"})
	if !ok {
		t.Fatal("expected target resolution to succeed")
	}
	rect, ok := model.activePaneContentRect()
	if !ok {
		t.Fatal("expected active pane content rect")
	}
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: uint16(rect.W), Rows: uint16(rect.H)},
	}
	terminal.PendingOwnerResize = true

	if err := service.resizeIfNeeded(context.Background(), target); err != nil {
		t.Fatalf("resize with pending owner force: %v", err)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected forced resize despite matching size, got %#v", client.resizes)
	}
}
