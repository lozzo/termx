package workbenchcodec

import (
	"reflect"
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/termx-core/workbenchdoc"
)

func TestExportImportRoundTrip(t *testing.T) {
	original := sampleWorkbench()

	exported := ExportWorkbench(original)
	restored := ImportDoc(exported)
	reexported := ExportWorkbench(restored)

	if !reflect.DeepEqual(exported, reexported) {
		t.Fatalf("workbench doc round-trip mismatch:\nexported=%#v\nreexported=%#v", exported, reexported)
	}
}

func TestPaneTerminalBindings(t *testing.T) {
	doc := &workbenchdoc.Doc{
		CurrentWorkspace: "main",
		WorkspaceOrder:   []string{"main"},
		Workspaces: map[string]*workbenchdoc.Workspace{
			"main": {
				Name: "main",
				Tabs: []*workbenchdoc.Tab{
					{
						ID: "tab-1",
						Panes: map[string]*workbenchdoc.Pane{
							"pane-1": {ID: "pane-1", TerminalID: "term-1"},
							"pane-2": {ID: "pane-2"},
							"pane-3": nil,
						},
					},
				},
			},
		},
	}

	got := PaneTerminalBindings(doc)
	want := map[string]string{"pane-1": "term-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected pane terminal bindings:\n got=%#v\nwant=%#v", got, want)
	}
}

func sampleWorkbench() *workbench.Workbench {
	wb := workbench.NewWorkbench()
	ws := &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:   "tab-1",
				Name: "editor",
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitVertical,
					Ratio:     0.5,
					First:     &workbench.LayoutNode{PaneID: "pane-1"},
					Second:    &workbench.LayoutNode{PaneID: "pane-2"},
				},
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
					"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
				},
				FloatingVisible: true,
				ActivePaneID:    "pane-2",
				ZoomedPaneID:    "",
				ScrollOffset:    3,
				LayoutPreset:    2,
				Floating: []*workbench.FloatingState{
					{
						PaneID:      "pane-2",
						Rect:        workbench.Rect{X: 10, Y: 4, W: 40, H: 12},
						Z:           2,
						Display:     workbench.FloatingDisplayExpanded,
						FitMode:     workbench.FloatingFitAuto,
						RestoreRect: workbench.Rect{X: 8, Y: 3, W: 32, H: 10},
						AutoFitCols: 120,
						AutoFitRows: 30,
					},
				},
			},
		},
	}
	wb.AddWorkspace("main", ws)
	_ = wb.SwitchWorkspace("main")
	return wb
}
