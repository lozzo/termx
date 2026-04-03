package render

import (
	"fmt"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var benchmarkFrameSink string

func BenchmarkCoordinatorRenderFrameFourPanesCached(b *testing.B) {
	coordinator := benchmarkCoordinator(b, 4, 160, 48)
	benchmarkFrameSink = coordinator.RenderFrame()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func BenchmarkCoordinatorRenderFrameFourPanesInvalidated(b *testing.B) {
	coordinator := benchmarkCoordinator(b, 4, 160, 48)
	benchmarkFrameSink = coordinator.RenderFrame()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coordinator.Invalidate()
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func BenchmarkCoordinatorRenderFrameFourPanesActiveSwitch(b *testing.B) {
	state := benchmarkState(b, 4, 160, 48)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	benchmarkFrameSink = coordinator.RenderFrame()
	tab := &state.Workbench.Tabs[state.Workbench.ActiveTab]
	paneIDs := make([]string, 0, len(tab.Panes))
	for _, pane := range tab.Panes {
		paneIDs = append(paneIDs, pane.ID)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tab.ActivePaneID = paneIDs[i%len(paneIDs)]
		coordinator.Invalidate()
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func benchmarkCoordinator(tb testing.TB, paneCount, width, height int) *Coordinator {
	tb.Helper()
	state := benchmarkState(tb, paneCount, width, height)
	return NewCoordinator(func() VisibleRenderState { return state })
}

func benchmarkState(tb testing.TB, paneCount, width, height int) VisibleRenderState {
	tb.Helper()
	if paneCount < 1 {
		paneCount = 1
	}

	wb := workbench.NewWorkbench()
	tab := &workbench.TabState{
		ID:           "tab-1",
		Name:         "tab 1",
		ActivePaneID: "pane-1",
		Panes:        make(map[string]*workbench.PaneState, paneCount),
	}
	ids := make([]string, 0, paneCount)
	for i := 0; i < paneCount; i++ {
		paneID := fmt.Sprintf("pane-%d", i+1)
		terminalID := fmt.Sprintf("term-%d", i+1)
		tab.Panes[paneID] = &workbench.PaneState{
			ID:         paneID,
			Title:      fmt.Sprintf("pane %d", i+1),
			TerminalID: terminalID,
		}
		ids = append(ids, paneID)
	}
	tab.Root = buildBenchmarkLayout(ids)
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs:      []*workbench.TabState{tab},
	})

	rt := runtime.New(nil)
	for i := 0; i < paneCount; i++ {
		terminalID := fmt.Sprintf("term-%d", i+1)
		terminal := rt.Registry().GetOrCreate(terminalID)
		terminal.Name = fmt.Sprintf("worker-%d", i+1)
		terminal.State = "running"
		terminal.Snapshot = benchmarkSnapshot(terminalID, width, height, i)
	}

	state := AdaptVisibleStateWithSize(wb, rt, width, height-2)
	state = WithTermSize(state, width, height)
	return state
}

func benchmarkSnapshot(terminalID string, width, height, seed int) *protocol.Snapshot {
	rows := maxInt(1, height-4)
	cols := maxInt(1, width/2)
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		line := fmt.Sprintf("term=%s row=%02d seed=%02d", terminalID, y, seed)
		row := make([]protocol.Cell, 0, cols)
		for _, ch := range line {
			if len(row) >= cols {
				break
			}
			row = append(row, protocol.Cell{Content: string(ch), Width: 1})
		}
		for len(row) < cols {
			row = append(row, protocol.Cell{Content: " ", Width: 1})
		}
		screen = append(screen, row)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Row: 0, Col: 0, Visible: true},
	}
}

func buildBenchmarkLayout(ids []string) *workbench.LayoutNode {
	switch len(ids) {
	case 0:
		return nil
	case 1:
		return workbench.NewLeaf(ids[0])
	case 2:
		return &workbench.LayoutNode{
			Direction: workbench.SplitVertical,
			Ratio:     0.5,
			First:     workbench.NewLeaf(ids[0]),
			Second:    workbench.NewLeaf(ids[1]),
		}
	default:
		mid := len(ids) / 2
		return &workbench.LayoutNode{
			Direction: workbench.SplitHorizontal,
			Ratio:     0.5,
			First:     buildBenchmarkLayout(ids[:mid]),
			Second:    buildBenchmarkLayout(ids[mid:]),
		}
	}
}
