package render

import (
	"strings"
	"testing"

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
	return AdaptVisibleState(wb, rt)
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
	if !strings.Contains(frame, "workspace: main") {
		t.Fatalf("frame missing workspace name:\n%s", frame)
	}
}

func TestRenderFrameContainsTabInfo(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if !strings.Contains(frame, "tab tab-1 (tab 1)") {
		t.Fatalf("frame missing tab info:\n%s", frame)
	}
}

func TestRenderFrameContainsPaneInfo(t *testing.T) {
	state := makeTestState()
	c := NewCoordinator(func() VisibleRenderState { return state })
	frame := c.RenderFrame()
	if !strings.Contains(frame, "pane pane-1 title=shell terminal=term-1") {
		t.Fatalf("frame missing pane info:\n%s", frame)
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
