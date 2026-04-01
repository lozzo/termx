package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func makeTestState() VisibleRenderState {
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
	return WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
}

func TestRenderFrameNonEmpty(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if frame == "" {
		t.Fatal("RenderFrame() returned empty string")
	}
}

func TestRenderFrameContainsWorkspaceName(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if !strings.Contains(frame, "main") {
		t.Fatalf("frame missing workspace name:\n%s", frame)
	}
}

func TestRenderFrameContainsTabInfo(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if !strings.Contains(frame, "tab 1") {
		t.Fatalf("frame missing tab info:\n%s", frame)
	}
}

func TestRenderFrameContainsPaneBorder(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	// Pane border should contain the title "shell"
	if !strings.Contains(frame, "shell") {
		t.Fatalf("frame missing pane title 'shell':\n%s", frame)
	}
	// Should have box drawing characters
	if !strings.Contains(frame, "┌") || !strings.Contains(frame, "┘") {
		t.Fatalf("frame missing pane border box characters:\n%s", frame)
	}
}

func TestRenderFrameNilCoordinator(t *testing.T) {
	var c *Coordinator
	if got := c.RenderFrame(); got != "" {
		t.Fatalf("nil coordinator must return empty string, got %q", got)
	}
}

func TestRenderFrameNoState(t *testing.T) {
	c := NewCoordinator(func() VisibleRenderState { return VisibleRenderState{} })
	frame := c.RenderFrame()
	if !strings.Contains(frame, "tuiv2") {
		t.Fatalf("empty state frame should contain fallback 'tuiv2', got %q", frame)
	}
}

func TestRenderFrameHasTabBarAndStatusBar(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	lines := strings.Split(frame, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (tab bar + body + status bar), got %d", len(lines))
	}
	// First line should be tab bar with workspace name
	if !strings.Contains(lines[0], "main") {
		t.Fatalf("first line should be tab bar with workspace, got %q", lines[0])
	}
	// Last line should be status bar
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "ws:main") && !strings.Contains(lastLine, "W WORKSPACE") {
		t.Fatalf("last line should be status bar, got %q", lastLine)
	}
}

func TestRenderBodyZoomedPaneOccupiesWholeBody(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ZoomedPaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	if !strings.Contains(body, "left") {
		t.Fatalf("expected zoomed pane title in body:\n%s", body)
	}
	if strings.Contains(body, "right") {
		t.Fatalf("expected non-zoomed pane to be hidden:\n%s", body)
	}
	if strings.Count(body, "┌") != 1 {
		t.Fatalf("expected exactly one pane frame when zoomed:\n%s", body)
	}
}

func TestRenderBodyScrollbackOffsetShowsOlderRows(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			ScrollOffset: 1,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 40, 8), 40, 10)
	state.Runtime = &VisibleRuntimeStateProxy{Terminals: []runtime.VisibleTerminal{{
		TerminalID: "term-1",
		Snapshot: &protocol.Snapshot{
			Scrollback: [][]protocol.Cell{{{Content: "A", Width: 1}}},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "B", Width: 1}}, {{Content: "C", Width: 1}}}},
		},
	}}}

	body := renderBody(state, 40, 8)
	if !strings.Contains(body, "A") {
		t.Fatalf("expected scrollback row to be visible when offset > 0:\n%s", body)
	}
}

func TestRenderBodyDrawsFloatingPanesOnTop(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{PaneID: "pane-2", Rect: workbench.Rect{X: 10, Y: 4, W: 24, H: 6}, Z: 1}},
		}},
	})
	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 100, 28), 100, 30)

	body := renderBody(state, 100, 28)
	for _, want := range []string{"base", "float"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q:\n%s", want, body)
		}
	}
}
