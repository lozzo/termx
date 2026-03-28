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

func TestRenderLoopInvalidateRenderMarksModelDirty(t *testing.T) {
	loop := NewRenderLoop(NewRenderer(nil, nil))
	model := &Model{}
	loop.bindModel(model)

	loop.Invalidate()

	if !model.renderDirty {
		t.Fatal("expected render loop invalidate to mark model dirty")
	}
}

func TestRenderLoopScheduleRenderMarksPendingWhenBatching(t *testing.T) {
	loop := NewRenderLoop(NewRenderer(nil, nil))
	model := &Model{renderBatching: true}
	loop.bindModel(model)

	loop.Schedule()

	if !model.renderPending.Load() {
		t.Fatal("expected render loop to mark render pending")
	}
}
