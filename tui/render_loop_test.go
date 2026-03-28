package tui

import "testing"

func TestNewRenderLoopHoldsRenderer(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}})
	terminalStore := NewTerminalStore()
	renderer := NewRenderer(workbench, terminalStore)

	loop := NewRenderLoop(renderer)

	if loop == nil {
		t.Fatal("expected render loop")
	}
	if loop.Renderer() != renderer {
		t.Fatal("expected render loop to hold renderer reference")
	}
}
