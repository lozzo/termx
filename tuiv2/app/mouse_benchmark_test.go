package app

import (
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	tuiruntime "github.com/lozzow/termx/tuiv2/runtime"
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

func BenchmarkFloatingDragEndToEndView(b *testing.B) {
	m := benchmarkFloatingDragViewModel(b)
	y := screenYForBodyY(m, 10)
	dragXs := [...]int{21, 22, 23, 24, 25, 24, 23, 22}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(tea.MouseMsg{
			X:      dragXs[i%len(dragXs)] + 2,
			Y:      y,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		})
		m = next.(*Model)
		m.render.Invalidate()
		_ = m.View()
	}
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

func benchmarkFloatingDragViewModel(tb testing.TB) *Model {
	tb.Helper()

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
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := tuiruntime.New(nil)
	base := rt.Registry().GetOrCreate("term-1")
	base.Name = "base"
	base.State = "running"
	base.Channel = 1
	base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 118, 34, "#444444")
	baseBinding := rt.BindPane("pane-1")
	baseBinding.Channel = 1
	baseBinding.Connected = true

	m := New(shared.Config{}, wb, rt)
	m.width = 120
	m.height = 40

	tab := m.workbench.CurrentTab()
	if tab == nil {
		tb.Fatal("expected current tab")
	}
	startRect := workbench.Rect{X: 18, Y: 7, W: 54, H: 16}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", startRect); err != nil {
		tb.Fatalf("create floating pane: %v", err)
	}
	if err := m.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		tb.Fatalf("bind floating terminal: %v", err)
	}
	floatTerminal := rt.Registry().GetOrCreate("term-float")
	floatTerminal.Name = "float"
	floatTerminal.State = "running"
	floatTerminal.Channel = 2
	floatTerminal.Snapshot = cursorWriterStyledSnapshot("term-float", 51, 14)
	floatBinding := rt.BindPane("float-1")
	floatBinding.Channel = 2
	floatBinding.Connected = true
	if err := m.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		tb.Fatalf("focus floating pane: %v", err)
	}

	writer := newOutputCursorWriter(benchmarkDiscardTTY{})
	writer.SetInteractiveFlushHint(m.runtime.RecentLocalInput)
	m.SetFrameWriter(writer)
	m.SetCursorWriter(writer)
	m.render.Invalidate()
	_ = m.View()

	next, _ := m.Update(tea.MouseMsg{
		X:      startRect.X + 2,
		Y:      screenYForBodyY(m, startRect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = next.(*Model)
	m.render.Invalidate()
	_ = m.View()
	return m
}

type benchmarkDiscardTTY struct{}

func (benchmarkDiscardTTY) Write(p []byte) (int, error) {
	return len(p), nil
}

func (benchmarkDiscardTTY) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (benchmarkDiscardTTY) Close() error {
	return nil
}

func (benchmarkDiscardTTY) Fd() uintptr {
	return 1
}
