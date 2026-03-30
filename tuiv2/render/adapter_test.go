package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestAdaptVisibleStateProjectsWorkbenchAndRuntime(t *testing.T) {
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

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"

	state := AdaptVisibleState(wb, rt)
	if state.Workbench == nil || state.Workbench.WorkspaceName != "main" {
		t.Fatalf("unexpected workbench projection: %#v", state.Workbench)
	}
	if len(state.Workbench.Tabs) != 1 || len(state.Workbench.Tabs[0].Panes) != 1 {
		t.Fatalf("unexpected pane projection: %#v", state.Workbench)
	}
	if state.Runtime == nil || len(state.Runtime.Terminals) != 1 {
		t.Fatalf("unexpected runtime projection: %#v", state.Runtime)
	}
	if state.Runtime.Terminals[0].TerminalID != "term-1" {
		t.Fatalf("unexpected runtime terminal: %#v", state.Runtime.Terminals[0])
	}
}
