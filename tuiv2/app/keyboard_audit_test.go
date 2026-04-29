package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func dispatchKeys(t *testing.T, model *Model, msgs ...tea.KeyMsg) {
	t.Helper()
	for _, msg := range msgs {
		dispatchKey(t, model, msg)
	}
}

func setupKeyboardMultiTabModel(t *testing.T) *Model {
	t.Helper()
	model := setupModel(t, modelOpts{})
	ws := model.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatal("expected current workspace")
	}
	if err := model.workbench.CreateTab(ws.Name, "tab-2", "tab 2"); err != nil {
		t.Fatalf("create tab-2: %v", err)
	}
	if err := model.workbench.CreateFirstPane("tab-2", "pane-2"); err != nil {
		t.Fatalf("create pane-2: %v", err)
	}
	if err := model.workbench.BindPaneTerminal("tab-2", "pane-2", "term-2"); err != nil {
		t.Fatalf("bind pane-2: %v", err)
	}
	term := model.runtime.Registry().GetOrCreate("term-2")
	term.Name = "logs"
	term.State = "running"
	term.Channel = 2
	binding := model.runtime.BindPane("pane-2")
	binding.Channel = 2
	binding.Connected = true
	if err := model.workbench.SwitchTab(ws.Name, 0); err != nil {
		t.Fatalf("switch back to tab-1: %v", err)
	}
	return model
}

func setupKeyboardMultiWorkspaceModel(t *testing.T) *Model {
	t.Helper()
	model := setupModel(t, modelOpts{})
	if err := model.workbench.CreateWorkspace("dev"); err != nil {
		t.Fatalf("create workspace dev: %v", err)
	}
	if ok := model.workbench.SwitchWorkspace("dev"); !ok {
		t.Fatal("switch workspace dev failed")
	}
	if err := model.workbench.CreateTab("dev", "tab-dev", "dev tab"); err != nil {
		t.Fatalf("create dev tab: %v", err)
	}
	if err := model.workbench.CreateFirstPane("tab-dev", "pane-dev"); err != nil {
		t.Fatalf("create dev pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal("tab-dev", "pane-dev", "term-dev"); err != nil {
		t.Fatalf("bind dev pane: %v", err)
	}
	term := model.runtime.Registry().GetOrCreate("term-dev")
	term.Name = "dev-shell"
	term.State = "running"
	term.Channel = 2
	binding := model.runtime.BindPane("pane-dev")
	binding.Channel = 2
	binding.Connected = true
	if ok := model.workbench.SwitchWorkspace("main"); !ok {
		t.Fatal("switch back to main failed")
	}
	return model
}

func setupKeyboardSharedTerminalModel(t *testing.T) *Model {
	t.Helper()
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	root := &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     workbench.NewLeaf("pane-1"),
		Second:    workbench.NewLeaf("pane-2"),
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
						"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
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

	if err := model.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("focus pane-2: %v", err)
	}
	return model
}

func setupKeyboardFloatingModel(t *testing.T) *Model {
	t.Helper()
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{{ID: "term-live", Name: "live", State: "running"}},
		},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-float": {TerminalID: "term-float", Size: protocol.Size{Cols: 48, Rows: 14}},
		},
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
	}
	model := setupModel(t, modelOpts{client: client})
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create float-1: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind float-1: %v", err)
	}
	model.runtime.Registry().GetOrCreate("term-float").Snapshot = &protocol.Snapshot{
		TerminalID: "term-float",
		Size:       protocol.Size{Cols: 48, Rows: 14},
	}
	binding := model.runtime.BindPane("float-1")
	binding.Role = runtime.BindingRoleOwner
	binding.Channel = 7
	binding.Connected = true
	if err := model.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("focus float-1: %v", err)
	}
	return model
}

func TestKeyboardAuditPaneModeBindings(t *testing.T) {
	t.Run("focus", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('l'))
		assertActivePane(t, model, "pane-2")
	})

	t.Run("split-vertical", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('%'))
		assertPaneCount(t, model, 2)
		assertMode(t, model, input.ModePicker)
	})

	t.Run("split-horizontal", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('"'))
		assertPaneCount(t, model, 2)
		assertMode(t, model, input.ModePicker)
	})

	t.Run("detach", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		term := model.runtime.Registry().Get("term-1")
		term.OwnerPaneID = "pane-1"
		term.BoundPaneIDs = []string{"pane-1"}
		binding := model.runtime.Binding("pane-1")
		binding.Role = runtime.BindingRoleOwner
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('d'))
		if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "" {
			t.Fatalf("expected detached pane, got %#v", pane)
		}
	})

	t.Run("reconnect", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('r'))
		assertMode(t, model, input.ModePicker)
	})

	t.Run("restart", func(t *testing.T) {
		client := &recordingBridgeClient{
			listResult:         &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell", State: "running"}}},
			attachResult:       &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}},
		}
		model := setupModel(t, modelOpts{client: client})
		exitCode := 23
		terminal := model.runtime.Registry().GetOrCreate("term-1")
		terminal.State = "exited"
		terminal.ExitCode = &exitCode
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('R'))
		if len(client.restartCalls) != 1 || client.restartCalls[0] != "term-1" {
			t.Fatalf("expected restart for term-1, got %#v", client.restartCalls)
		}
	})

	t.Run("owner", func(t *testing.T) {
		model := setupKeyboardSharedTerminalModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('a'))
		terminal := model.runtime.Registry().Get("term-1")
		if terminal.OwnerPaneID != "pane-2" {
			t.Fatalf("expected pane-2 to become owner, got %q", terminal.OwnerPaneID)
		}
	})

	t.Run("zoom", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('z'))
		if got := model.workbench.CurrentTab().ZoomedPaneID; got != "pane-1" {
			t.Fatalf("expected pane-1 zoomed, got %q", got)
		}
		assertMode(t, model, input.ModeNormal)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('z'))
		if got := model.workbench.CurrentTab().ZoomedPaneID; got != "" {
			t.Fatalf("expected zoom toggled off, got %q", got)
		}
		assertMode(t, model, input.ModeNormal)
	})

	t.Run("close", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('w'))
		assertPaneCount(t, model, 1)
	})

	t.Run("close-kill", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		client := model.runtime.Client().(*recordingBridgeClient)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlP), runeKeyMsg('X'))
		assertPaneCount(t, model, 1)
		if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
			t.Fatalf("expected kill call for term-1, got %#v", client.killCalls)
		}
	})
}

func TestKeyboardAuditResizeModeBindings(t *testing.T) {
	t.Run("small-resize", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		tab := model.workbench.CurrentTab()
		orig := tab.Root.Ratio
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlR), runeKeyMsg('l'))
		if got := tab.Root.Ratio; got == orig {
			t.Fatalf("expected ratio change, still %f", got)
		}
	})

	t.Run("large-resize", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		tab := model.workbench.CurrentTab()
		orig := tab.Root.Ratio
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlR), runeKeyMsg('L'))
		if got := tab.Root.Ratio; got <= orig {
			t.Fatalf("expected larger ratio, got %f from %f", got, orig)
		}
	})

	t.Run("balance", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlR), runeKeyMsg('l'))
		if tab.Root.Ratio == 0.5 {
			t.Fatal("expected precondition ratio change")
		}
		dispatchKey(t, model, runeKeyMsg('='))
		if got := tab.Root.Ratio; got != 0.5 {
			t.Fatalf("expected balanced ratio 0.5, got %f", got)
		}
	})

	t.Run("cycle-layout", func(t *testing.T) {
		model := setupTwoPaneModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlR), tea.KeyMsg{Type: tea.KeySpace})
		if got := tab.LayoutPreset; got != 1 {
			t.Fatalf("expected layout preset 1, got %d", got)
		}
	})

	t.Run("owner", func(t *testing.T) {
		model := setupKeyboardSharedTerminalModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlR), runeKeyMsg('a'))
		terminal := model.runtime.Registry().Get("term-1")
		if terminal.OwnerPaneID != "pane-2" {
			t.Fatalf("expected pane-2 to become owner, got %q", terminal.OwnerPaneID)
		}
	})
}

func TestKeyboardAuditTabModeBindings(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('c'))
		assertTabCount(t, model, 2)
		assertMode(t, model, input.ModePicker)
	})

	t.Run("rename", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('r'))
		assertMode(t, model, input.ModePrompt)
		if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-tab" {
			t.Fatalf("expected rename-tab prompt, got %#v", model.modalHost.Prompt)
		}
	})

	t.Run("next", func(t *testing.T) {
		model := setupKeyboardMultiTabModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('n'))
		if got := model.workbench.CurrentTab().ID; got != "tab-2" {
			t.Fatalf("expected tab-2 after next, got %q", got)
		}
	})

	t.Run("prev", func(t *testing.T) {
		model := setupKeyboardMultiTabModel(t)
		if err := model.workbench.SwitchTab("main", 1); err != nil {
			t.Fatalf("switch to tab-2: %v", err)
		}
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('p'))
		if got := model.workbench.CurrentTab().ID; got != "tab-1" {
			t.Fatalf("expected tab-1 after prev, got %q", got)
		}
	})

	t.Run("jump", func(t *testing.T) {
		model := setupKeyboardMultiTabModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('2'))
		if got := model.workbench.CurrentTab().ID; got != "tab-2" {
			t.Fatalf("expected tab-2 after jump, got %q", got)
		}
	})

	t.Run("kill", func(t *testing.T) {
		model := setupKeyboardMultiTabModel(t)
		if err := model.workbench.SwitchTab("main", 1); err != nil {
			t.Fatalf("switch to tab-2: %v", err)
		}
		client := model.runtime.Client().(*recordingBridgeClient)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlT), runeKeyMsg('x'))
		assertTabCount(t, model, 1)
		if len(client.killCalls) != 1 || client.killCalls[0] != "term-2" {
			t.Fatalf("expected kill call for term-2, got %#v", client.killCalls)
		}
	})
}

func TestKeyboardAuditWorkspaceModeBindings(t *testing.T) {
	t.Run("picker", func(t *testing.T) {
		model := setupKeyboardMultiWorkspaceModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('f'))
		assertMode(t, model, input.ModeWorkspacePicker)
	})

	t.Run("create", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('c'))
		assertMode(t, model, input.ModePrompt)
		if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-workspace" {
			t.Fatalf("expected workspace prompt, got %#v", model.modalHost.Prompt)
		}
	})

	t.Run("rename", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('r'))
		assertMode(t, model, input.ModePrompt)
		if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-workspace" {
			t.Fatalf("expected rename-workspace prompt, got %#v", model.modalHost.Prompt)
		}
	})

	t.Run("delete", func(t *testing.T) {
		model := setupKeyboardMultiWorkspaceModel(t)
		if ok := model.workbench.SwitchWorkspace("dev"); !ok {
			t.Fatal("switch workspace dev failed")
		}
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('x'))
		if got := len(model.workbench.ListWorkspaces()); got != 1 {
			t.Fatalf("expected one workspace after delete, got %d", got)
		}
	})

	t.Run("next-prev", func(t *testing.T) {
		model := setupKeyboardMultiWorkspaceModel(t)
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('n'))
		if got := model.workbench.CurrentWorkspace().Name; got != "dev" {
			t.Fatalf("expected dev after next, got %q", got)
		}
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlW), runeKeyMsg('p'))
		if got := model.workbench.CurrentWorkspace().Name; got != "main" {
			t.Fatalf("expected main after prev, got %q", got)
		}
	})
}

func TestKeyboardAuditFloatingModeBindings(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlO), runeKeyMsg('n'))
		tab := model.workbench.CurrentTab()
		if len(tab.Floating) != 1 {
			t.Fatalf("expected one floating pane, got %d", len(tab.Floating))
		}
		assertMode(t, model, input.ModePicker)
	})

	t.Run("move-resize-center", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		initial := findFloating(tab, "float-1")
		origX, origW := initial.Rect.X, initial.Rect.W
		dispatchKey(t, model, runeKeyMsg('l'))
		dispatchKey(t, model, runeKeyMsg('L'))
		dispatchKey(t, model, runeKeyMsg('c'))
		updated := findFloating(tab, "float-1")
		if updated.Rect.X <= origX {
			t.Fatalf("expected float moved right, got %d from %d", updated.Rect.X, origX)
		}
		if updated.Rect.W <= origW {
			t.Fatalf("expected float width increased, got %d from %d", updated.Rect.W, origW)
		}
	})

	t.Run("collapse", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 20, Y: 8, W: 30, H: 10}); err != nil {
			t.Fatalf("create float-2: %v", err)
		}
		if err := model.workbench.FocusPane(tab.ID, "float-2"); err != nil {
			t.Fatalf("focus float-2: %v", err)
		}
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('m'))
		if got := model.workbench.FloatingState(tab.ID, "float-2"); got == nil || got.Display != workbench.FloatingDisplayCollapsed {
			t.Fatalf("expected float-2 collapsed, got %#v", got)
		}
	})

	t.Run("overview", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('o'))
		assertMode(t, model, input.ModeFloatingOverview)
	})

	t.Run("overview-summon", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		if err := model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 20, Y: 8, W: 30, H: 10}); err != nil {
			t.Fatalf("create float-2: %v", err)
		}
		if err := model.workbench.FocusPane(tab.ID, "float-2"); err != nil {
			t.Fatalf("focus float-2: %v", err)
		}
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('m'))
		dispatchKey(t, model, runeKeyMsg('o'))
		assertMode(t, model, input.ModeFloatingOverview)
		slot := 0
		if model.modalHost == nil || model.modalHost.FloatingOverview == nil {
			t.Fatal("expected floating overview state")
		}
		for _, item := range model.modalHost.FloatingOverview.Items {
			if item.PaneID == "float-2" {
				slot = item.ShortcutSlot
				break
			}
		}
		if slot == 0 {
			t.Fatalf("expected float-2 overview slot, got %#v", model.modalHost.FloatingOverview.Items)
		}
		dispatchKey(t, model, runeKeyMsg(rune('0'+slot)))
		if got := model.workbench.FloatingState(tab.ID, "float-2"); got == nil || got.Display != workbench.FloatingDisplayExpanded {
			t.Fatalf("expected float-2 restored, got %#v", got)
		}
		if got := tab.ActivePaneID; got != "float-2" {
			t.Fatalf("expected float-2 focused after summon, got %q", got)
		}
	})

	t.Run("toggle-visibility", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('v'))
		if tab.FloatingVisible {
			t.Fatal("expected floating hidden after toggle")
		}
		dispatchKey(t, model, runeKeyMsg('v'))
		if !tab.FloatingVisible {
			t.Fatal("expected floating visible after second toggle")
		}
	})

	t.Run("fit-once", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('='))
		if got := model.workbench.FloatingState(tab.ID, "float-1"); got == nil || got.Rect.W != 50 || got.Rect.H != 16 {
			t.Fatalf("expected fit-once rect 50x16, got %#v", got)
		}
	})

	t.Run("toggle-autofit", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('s'))
		if got := model.workbench.FloatingState(tab.ID, "float-1"); got == nil || got.FitMode != workbench.FloatingFitAuto {
			t.Fatalf("expected auto-fit mode, got %#v", got)
		}
	})

	t.Run("picker", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('f'))
		assertMode(t, model, input.ModePicker)
	})

	t.Run("close", func(t *testing.T) {
		model := setupKeyboardFloatingModel(t)
		tab := model.workbench.CurrentTab()
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlO))
		dispatchKey(t, model, runeKeyMsg('x'))
		if got := len(tab.Floating); got != 0 {
			t.Fatalf("expected no floating panes after close, got %d", got)
		}
	})
}

func TestKeyboardAuditDisplayModeBindings(t *testing.T) {
	t.Run("movement-and-selection", func(t *testing.T) {
		model := setupModel(t, modelOpts{width: 40, height: 8})
		seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charl", "delta", "echoo"})
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
		assertMode(t, model, input.ModeDisplay)
		dispatchKey(t, model, runeKeyMsg('g'))
		if got := model.copyMode.Cursor.Row; got != 0 {
			t.Fatalf("expected cursor at top, got row %d", got)
		}
		dispatchKey(t, model, tea.KeyMsg{Type: tea.KeySpace})
		if model.copyMode.Mark == nil {
			t.Fatal("expected mark after space")
		}
		dispatchKey(t, model, runeKeyMsg('l'))
		if got := model.copyMode.Cursor.Col; got != 1 {
			t.Fatalf("expected cursor col 1, got %d", got)
		}
		dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnd})
		if got := model.copyMode.Cursor.Col; got == 0 {
			t.Fatalf("expected end-of-line move, got col %d", got)
		}
		dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyHome})
		if got := model.copyMode.Cursor.Col; got != 0 {
			t.Fatalf("expected start-of-line move, got col %d", got)
		}
	})

	t.Run("copy-and-exit", func(t *testing.T) {
		model := setupModel(t, modelOpts{width: 40, height: 8})
		seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charl", "delta", "echoo"})
		writer := &recordingControlWriter{}
		model.SetCursorWriter(writer)
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
		dispatchKeys(t, model, runeKeyMsg('g'), tea.KeyMsg{Type: tea.KeySpace}, runeKeyMsg('l'), runeKeyMsg('l'), runeKeyMsg('y'))
		if len(writer.controls) != 1 {
			t.Fatalf("expected one clipboard write after y, got %#v", writer.controls)
		}
		assertMode(t, model, input.ModeDisplay)
		dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		assertMode(t, model, input.ModeNormal)
	})

	t.Run("page-and-halfpage", func(t *testing.T) {
		model := setupModel(t, modelOpts{width: 40, height: 8})
		seedCopyModeSnapshot(t, model, []string{"s0", "s1", "s2", "s3", "s4", "s5"}, []string{"n0", "n1", "n2", "n3"})
		dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
		before := model.copyMode.Cursor.Row
		dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
		if got := model.copyMode.Cursor.Row; got >= before {
			t.Fatalf("expected page-up to move cursor upward, before=%d after=%d", before, got)
		}
		before = model.copyMode.Cursor.Row
		dispatchKey(t, model, runeKeyMsg('d'))
		if got := model.copyMode.Cursor.Row; got <= before {
			t.Fatalf("expected half-page-down to move cursor downward, before=%d after=%d", before, got)
		}
		dispatchKey(t, model, runeKeyMsg('G'))
		if got := model.copyMode.Cursor.Row; got == 0 {
			t.Fatalf("expected bottom jump, got row %d", got)
		}
	})

	t.Run("paste-history-and-zoom", func(t *testing.T) {
		model := setupModel(t, modelOpts{width: 80, height: 12})
		seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
		model.yankBuffer = "hello\nworld"
		model.pushClipboardHistory("first entry", "pane-1")
		prevReader := systemClipboardReader
		systemClipboardReader = func() (string, error) { return "clip-text", nil }
		defer func() { systemClipboardReader = prevReader }()

		dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
		dispatchKey(t, model, runeKeyMsg('z'))
		if got := model.workbench.CurrentTab().ZoomedPaneID; got != "pane-1" {
			t.Fatalf("expected zoomed pane-1, got %q", got)
		}
		assertMode(t, model, input.ModeNormal)

		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlV), runeKeyMsg('h'))
		assertMode(t, model, input.ModePicker)

		model = setupModel(t, modelOpts{width: 80, height: 12})
		seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
		model.yankBuffer = "hello\nworld"
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlV), runeKeyMsg('p'))
		client := model.runtime.Client().(*recordingBridgeClient)
		if len(client.inputCalls) != 1 || string(client.inputCalls[0].data) != "hello\nworld" {
			t.Fatalf("expected paste-buffer input, got %#v", client.inputCalls)
		}

		model = setupModel(t, modelOpts{width: 80, height: 12})
		seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlV), runeKeyMsg('P'))
		client = model.runtime.Client().(*recordingBridgeClient)
		if len(client.inputCalls) != 1 || string(client.inputCalls[0].data) != "clip-text" {
			t.Fatalf("expected clipboard paste input, got %#v", client.inputCalls)
		}
	})
}

func TestKeyboardAuditGlobalBindings(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlG), runeKeyMsg('?'))
		assertMode(t, model, input.ModeHelp)
	})

	t.Run("terminal-manager", func(t *testing.T) {
		client := &recordingBridgeClient{
			listResult:         &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "t1", Name: "shell", State: "running"}}},
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		}
		model := setupModel(t, modelOpts{client: client})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlG), runeKeyMsg('t'))
		assertMode(t, model, input.ModeTerminalManager)
	})

	t.Run("quit", func(t *testing.T) {
		model := setupModel(t, modelOpts{})
		dispatchKeys(t, model, ctrlKey(tea.KeyCtrlG), runeKeyMsg('q'))
		if !model.quitting {
			t.Fatal("expected quitting flag after Ctrl-G q")
		}
	})
}
