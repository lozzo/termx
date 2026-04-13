package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestBuildStatusHintsHidesUnavailablePaneActionsForUnconnectedPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	vm := render.WithRenderTermSize(render.AdaptRenderVMWithSize(wb, model.runtime, 80, 18), 80, 20)
	vm = render.WithRenderStatus(vm, "", "", string(input.ModePane))

	hints := model.buildStatusHints(vm)
	assertHintsContain(t, hints, "r RECONNECT", "z ZOOM")
	assertHintsOmit(t, hints, "d DETACH", "a OWNER", "X CLOSE+KILL")
}

func TestBuildStatusHintsShowsOwnerActionForSharedFollower(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
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
	})
	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	ownerBinding := rt.BindPane("pane-1")
	ownerBinding.Role = runtime.BindingRoleOwner
	ownerBinding.Connected = true
	followerBinding := rt.BindPane("pane-2")
	followerBinding.Role = runtime.BindingRoleFollower
	followerBinding.Connected = true

	model := New(shared.Config{}, wb, rt)
	vm := render.WithRenderTermSize(render.AdaptRenderVMWithSize(wb, rt, 120, 18), 120, 20)
	vm = render.WithRenderStatus(vm, "", "", string(input.ModePane))

	hints := model.buildStatusHints(vm)
	assertHintsContain(t, hints, "a OWNER", "d DETACH")
}

func TestBuildStatusHintsWorkspacePickerFollowSelectedItemKind(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	vm := render.RenderVM{
		TermSize: render.TermSize{Width: 180, Height: 20},
		Status: render.RenderStatusVM{
			InputMode: string(input.ModeWorkspacePicker),
		},
		Workbench: &workbench.VisibleWorkbench{WorkspaceName: "main"},
		Overlay: render.RenderOverlayVM{
			Kind: render.VisibleOverlayWorkspacePicker,
			WorkspacePicker: &modal.WorkspacePickerState{
				Items: []modal.WorkspacePickerItem{
					{Kind: modal.WorkspacePickerItemWorkspace, Name: "main", WorkspaceName: "main"},
					{Kind: modal.WorkspacePickerItemTab, Name: "backend", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, Depth: 1},
					{Kind: modal.WorkspacePickerItemPane, Name: "vim", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, PaneID: "pane-1", Depth: 2},
				},
				Filtered: []modal.WorkspacePickerItem{
					{Kind: modal.WorkspacePickerItemWorkspace, Name: "main", WorkspaceName: "main"},
					{Kind: modal.WorkspacePickerItemTab, Name: "backend", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, Depth: 1},
					{Kind: modal.WorkspacePickerItemPane, Name: "vim", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, PaneID: "pane-1", Depth: 2},
				},
			},
		},
	}

	vm.Overlay.WorkspacePicker.Selected = 0
	hints := model.buildStatusHints(vm)
	assertHintsContain(t, hints, "Ctrl-R RENAME", "Ctrl-X REMOVE")
	assertHintsOmit(t, hints, "Ctrl-D DETACH", "Ctrl-Z ZOOM")

	vm.Overlay.WorkspacePicker.Selected = 2
	hints = model.buildStatusHints(vm)
	assertHintsContain(t, hints, "Ctrl-X REMOVE", "Ctrl-D DETACH", "Ctrl-Z ZOOM")
	assertHintsOmit(t, hints, "Ctrl-R RENAME", "Ctrl-N NEW")
}

func TestBuildStatusHintsFloatingModeShowsOnlyCreateWithoutActiveFloatingPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	vm := render.WithRenderTermSize(render.AdaptRenderVMWithSize(wb, model.runtime, 80, 18), 80, 20)
	vm = render.WithRenderStatus(vm, "", "", string(input.ModeFloating))

	hints := model.buildStatusHints(vm)
	assertHintsContain(t, hints, "N NEW FLOAT")
	assertHintsOmit(t, hints, "h/j/k/l MOVE", "H/J/K/L RESIZE", "x CLOSE", "v TOGGLE", "a OWNER")
}

func assertHintsContain(t *testing.T, hints []string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		found := false
		for _, hint := range hints {
			if hint == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected hint %q in %#v", want, hints)
		}
	}
}

func assertHintsOmit(t *testing.T, hints []string, unwanted ...string) {
	t.Helper()
	for _, want := range unwanted {
		for _, hint := range hints {
			if hint == want {
				t.Fatalf("did not expect hint %q in %#v", want, hints)
			}
		}
	}
}
