package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func BenchmarkHandleMouseDragMove(b *testing.B) {
	b.Run("same_cell", func(b *testing.B) {
		m := benchmarkFloatingDragModel(b)
		y := screenYForBodyY(m, 5)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.handleMouseDrag(15, y)
		}
	})

	b.Run("toggle_cell", func(b *testing.B) {
		m := benchmarkFloatingDragModel(b)
		y := screenYForBodyY(m, 5)
		xs := [2]int{15, 16}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.handleMouseDrag(xs[i&1], y)
		}
	})
}

func benchmarkFloatingDragModel(b *testing.B) *Model {
	b.Helper()

	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{Name: "main"})
	if err := wb.CreateTab("main", "tab-1", "tab 1"); err != nil {
		b.Fatalf("create tab: %v", err)
	}
	if err := wb.CreateFirstPane("tab-1", "pane-1"); err != nil {
		b.Fatalf("create first pane: %v", err)
	}
	if err := wb.CreateFloatingPane("tab-1", "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		b.Fatalf("create floating pane: %v", err)
	}

	m := New(shared.Config{}, wb, nil)
	m.width = 120
	m.height = 40
	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	return m
}
