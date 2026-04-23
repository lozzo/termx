package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var benchmarkFrameSink string
var benchmarkLinesSink []string

func BenchmarkComposedCanvasSmallEdit(b *testing.B) {
	canvas := newComposedCanvas(200, 2)
	canvas.drawText(0, 0, strings.Repeat("x", 200), drawStyle{FG: "#cccccc"})
	canvas.drawText(0, 1, strings.Repeat("y", 200), drawStyle{FG: "#cccccc"})
	benchmarkFrameSink = canvas.contentString()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x := 80 + (i % 20)
		canvas.set(x, 0, drawCell{Content: "Z", Width: 1, Style: drawStyle{FG: "#ff0000"}})
		benchmarkFrameSink = canvas.contentString()
	}
}

func BenchmarkComposedCanvasCachedContentLinesDirtyRow(b *testing.B) {
	canvas := newComposedCanvas(200, 24)
	for y := 0; y < 24; y++ {
		canvas.drawText(0, y, strings.Repeat(string(rune('a'+(y%26))), 200), drawStyle{FG: "#cccccc"})
	}
	benchmarkLinesSink = canvas.cachedContentLines()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := i % 24
		x := 48 + (i % 32)
		canvas.set(x, row, drawCell{Content: "Z", Width: 1, Style: drawStyle{FG: "#ff0000"}})
		benchmarkLinesSink = canvas.cachedContentLines()
	}
}

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

func BenchmarkCoordinatorRenderFrameOverlapInvalidated(b *testing.B) {
	cases := []struct {
		name       string
		floatRects []workbench.Rect
	}{
		{name: "tiled_only"},
		{name: "one_floating", floatRects: []workbench.Rect{{X: 10, Y: 4, W: 56, H: 16}}},
		{name: "two_floating_overlap", floatRects: []workbench.Rect{
			{X: 8, Y: 4, W: 56, H: 16},
			{X: 24, Y: 6, W: 56, H: 16},
		}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			state, _ := benchmarkFloatingState(b, 160, 48, tc.floatRects)
			coordinator := NewCoordinator(func() VisibleRenderState { return state })
			benchmarkFrameSink = coordinator.RenderFrame()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				coordinator.Invalidate()
				benchmarkFrameSink = coordinator.RenderFrame()
			}
		})
	}
}

func BenchmarkCoordinatorRenderFrameTwoPaneScrollIncremental(b *testing.B) {
	state := benchmarkState(b, 2, 160, 48)
	activeTab := state.Workbench.Tabs[state.Workbench.ActiveTab]
	if len(activeTab.Panes) != 2 {
		b.Fatalf("expected two panes, got %#v", activeTab.Panes)
	}
	leftContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[0].Rect, activeTab.Panes[0].SharedLeft, activeTab.Panes[0].SharedTop)
	if !ok {
		b.Fatalf("expected left content rect from %#v", activeTab.Panes[0].Rect)
	}
	rightContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[1].Rect, activeTab.Panes[1].SharedLeft, activeTab.Panes[1].SharedTop)
	if !ok {
		b.Fatalf("expected right content rect from %#v", activeTab.Panes[1].Rect)
	}

	leftA := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 1)
	leftB := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 2)
	right := benchmarkScrollSurface("right", rightContent.W, rightContent.H, 200)

	for i := range state.Runtime.Terminals {
		switch state.Runtime.Terminals[i].TerminalID {
		case "term-1":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = leftA
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		case "term-2":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = right
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		}
	}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	benchmarkFrameSink = coordinator.RenderFrame()

	version := uint64(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		version++
		surface := leftB
		scroll := 1
		if i%2 != 0 {
			surface = leftA
			scroll = -1
		}
		state.Runtime.Terminals[0].Surface = surface
		state.Runtime.Terminals[0].SurfaceVersion = version
		state.Runtime.Terminals[0].ScreenUpdate = runtime.VisibleScreenUpdateSummary{
			SurfaceVersion: version,
			ScreenScroll:   scroll,
		}
		coordinator.Invalidate()
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func BenchmarkCoordinatorRenderFrameTwoPaneScrollFullPaneFallback(b *testing.B) {
	state := benchmarkState(b, 2, 160, 48)
	activeTab := state.Workbench.Tabs[state.Workbench.ActiveTab]
	if len(activeTab.Panes) != 2 {
		b.Fatalf("expected two panes, got %#v", activeTab.Panes)
	}
	leftContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[0].Rect, activeTab.Panes[0].SharedLeft, activeTab.Panes[0].SharedTop)
	if !ok {
		b.Fatalf("expected left content rect from %#v", activeTab.Panes[0].Rect)
	}
	rightContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[1].Rect, activeTab.Panes[1].SharedLeft, activeTab.Panes[1].SharedTop)
	if !ok {
		b.Fatalf("expected right content rect from %#v", activeTab.Panes[1].Rect)
	}

	leftA := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 1)
	leftB := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 2)
	right := benchmarkScrollSurface("right", rightContent.W, rightContent.H, 200)

	for i := range state.Runtime.Terminals {
		switch state.Runtime.Terminals[i].TerminalID {
		case "term-1":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = leftA
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		case "term-2":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = right
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		}
	}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	benchmarkFrameSink = coordinator.RenderFrame()

	version := uint64(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		version++
		surface := leftB
		if i%2 != 0 {
			surface = leftA
		}
		state.Runtime.Terminals[0].Surface = surface
		state.Runtime.Terminals[0].SurfaceVersion = version
		state.Runtime.Terminals[0].ScreenUpdate = runtime.VisibleScreenUpdateSummary{
			SurfaceVersion: version,
			ChangedRows:    []int{0, maxInt(0, leftContent.H-1)},
		}
		coordinator.Invalidate()
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func BenchmarkCoordinatorRenderFrameTwoPaneCursorMoveIncremental(b *testing.B) {
	state := benchmarkState(b, 2, 160, 48)
	activeTab := state.Workbench.Tabs[state.Workbench.ActiveTab]
	if len(activeTab.Panes) != 2 {
		b.Fatalf("expected two panes, got %#v", activeTab.Panes)
	}
	leftContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[0].Rect, activeTab.Panes[0].SharedLeft, activeTab.Panes[0].SharedTop)
	if !ok {
		b.Fatalf("expected left content rect from %#v", activeTab.Panes[0].Rect)
	}
	rightContent, ok := workbench.FramedPaneContentRect(activeTab.Panes[1].Rect, activeTab.Panes[1].SharedLeft, activeTab.Panes[1].SharedTop)
	if !ok {
		b.Fatalf("expected right content rect from %#v", activeTab.Panes[1].Rect)
	}

	leftA := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 1)
	leftB := benchmarkScrollSurface("left", leftContent.W, leftContent.H, 1)
	leftB.cursor.Row = maxInt(0, leftA.cursor.Row-1)
	leftB.cursor.Col = minInt(leftContent.W-1, leftA.cursor.Col+1)
	right := benchmarkScrollSurface("right", rightContent.W, rightContent.H, 200)

	for i := range state.Runtime.Terminals {
		switch state.Runtime.Terminals[i].TerminalID {
		case "term-1":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = leftA
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		case "term-2":
			state.Runtime.Terminals[i].Snapshot = nil
			state.Runtime.Terminals[i].Surface = right
			state.Runtime.Terminals[i].SurfaceVersion = 1
			state.Runtime.Terminals[i].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		}
	}

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	benchmarkFrameSink = coordinator.RenderFrame()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		surface := leftB
		if i%2 != 0 {
			surface = leftA
		}
		state.Runtime.Terminals[0].Surface = surface
		state.Runtime.Terminals[0].SurfaceVersion = 1
		state.Runtime.Terminals[0].ScreenUpdate = runtime.VisibleScreenUpdateSummary{SurfaceVersion: 1}
		coordinator.Invalidate()
		benchmarkFrameSink = coordinator.RenderFrame()
	}
}

func BenchmarkCoordinatorRenderFrameFloatingDrag(b *testing.B) {
	cases := []struct {
		name       string
		floatRects []workbench.Rect
		positions  []workbench.Rect
	}{
		{
			name:       "one_floating",
			floatRects: []workbench.Rect{{X: 10, Y: 4, W: 56, H: 16}},
			positions: []workbench.Rect{
				{X: 10, Y: 4, W: 56, H: 16},
				{X: 11, Y: 4, W: 56, H: 16},
				{X: 12, Y: 4, W: 56, H: 16},
				{X: 13, Y: 4, W: 56, H: 16},
				{X: 14, Y: 4, W: 56, H: 16},
			},
		},
		{
			name: "two_floating_overlap",
			floatRects: []workbench.Rect{
				{X: 8, Y: 4, W: 56, H: 16},
				{X: 24, Y: 6, W: 56, H: 16},
			},
			positions: []workbench.Rect{
				{X: 8, Y: 4, W: 56, H: 16},
				{X: 9, Y: 4, W: 56, H: 16},
				{X: 10, Y: 4, W: 56, H: 16},
				{X: 11, Y: 4, W: 56, H: 16},
				{X: 12, Y: 4, W: 56, H: 16},
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			state, floatIndexes := benchmarkFloatingState(b, 160, 48, tc.floatRects)
			movingIndex := floatIndexes["float-1"]
			coordinator := NewCoordinator(func() VisibleRenderState { return state })
			benchmarkFrameSink = coordinator.RenderFrame()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state.Workbench.FloatingPanes[movingIndex].Rect = tc.positions[i%len(tc.positions)]
				coordinator.Invalidate()
				benchmarkFrameSink = coordinator.RenderFrame()
			}
		})
	}
}

func BenchmarkCoordinatorRenderFrameFloatingDragSize(b *testing.B) {
	cases := []struct {
		name string
		rect workbench.Rect
	}{
		{name: "small", rect: workbench.Rect{X: 10, Y: 4, W: 28, H: 10}},
		{name: "medium", rect: workbench.Rect{X: 10, Y: 4, W: 56, H: 16}},
		{name: "large", rect: workbench.Rect{X: 10, Y: 4, W: 96, H: 28}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			state, floatIndexes := benchmarkFloatingState(b, 160, 48, []workbench.Rect{tc.rect})
			movingIndex := floatIndexes["float-1"]
			coordinator := NewCoordinator(func() VisibleRenderState { return state })
			benchmarkFrameSink = coordinator.RenderFrame()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next := state.Workbench.FloatingPanes[movingIndex].Rect
				next.X = tc.rect.X + (i % 4)
				state.Workbench.FloatingPanes[movingIndex].Rect = next
				coordinator.Invalidate()
				benchmarkFrameSink = coordinator.RenderFrame()
			}
		})
	}
}

func BenchmarkCoordinatorRenderFrameFloatingDragContentComplexity(b *testing.B) {
	cases := []struct {
		name   string
		styled bool
	}{
		{name: "plain_shell", styled: false},
		{name: "styled_codex", styled: true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			rect := workbench.Rect{X: 10, Y: 4, W: 56, H: 16}
			state, floatIndexes := benchmarkFloatingState(b, 160, 48, []workbench.Rect{rect})
			movingIndex := floatIndexes["float-1"]
			for i := range state.Runtime.Terminals {
				if state.Runtime.Terminals[i].TerminalID != "term-2" {
					continue
				}
				if tc.styled {
					state.Runtime.Terminals[i].Snapshot = benchmarkStyledSnapshot(state.Runtime.Terminals[i].TerminalID, rect.W-3, rect.H-2)
				} else {
					state.Runtime.Terminals[i].Snapshot = benchmarkSnapshot(state.Runtime.Terminals[i].TerminalID, rect.W-1, rect.H+2, 7)
				}
				break
			}

			coordinator := NewCoordinator(func() VisibleRenderState { return state })
			benchmarkFrameSink = coordinator.RenderFrame()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next := state.Workbench.FloatingPanes[movingIndex].Rect
				next.X = rect.X + (i % 4)
				state.Workbench.FloatingPanes[movingIndex].Rect = next
				coordinator.Invalidate()
				benchmarkFrameSink = coordinator.RenderFrame()
			}
		})
	}
}

func BenchmarkRenderBodyFrameFloatingDragPreview(b *testing.B) {
	cases := []struct {
		name       string
		floatRects []workbench.Rect
		positions  []workbench.Rect
	}{
		{
			name:       "base_only",
			floatRects: []workbench.Rect{{X: 10, Y: 4, W: 56, H: 16}},
			positions: []workbench.Rect{
				{X: 10, Y: 4, W: 56, H: 16},
				{X: 11, Y: 4, W: 56, H: 16},
				{X: 12, Y: 4, W: 56, H: 16},
				{X: 13, Y: 4, W: 56, H: 16},
				{X: 14, Y: 4, W: 56, H: 16},
			},
		},
		{
			name: "overlap_with_second_float",
			floatRects: []workbench.Rect{
				{X: 8, Y: 4, W: 56, H: 16},
				{X: 24, Y: 6, W: 56, H: 16},
			},
			positions: []workbench.Rect{
				{X: 8, Y: 4, W: 56, H: 16},
				{X: 9, Y: 4, W: 56, H: 16},
				{X: 10, Y: 4, W: 56, H: 16},
				{X: 11, Y: 4, W: 56, H: 16},
				{X: 12, Y: 4, W: 56, H: 16},
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			state, _ := benchmarkFloatingState(b, 160, 48, tc.floatRects)
			var previewSnapshot *protocol.Snapshot
			for i := range state.Runtime.Terminals {
				if state.Runtime.Terminals[i].TerminalID == "term-2" {
					previewSnapshot = state.Runtime.Terminals[i].Snapshot
					break
				}
			}
			if previewSnapshot == nil {
				b.Fatal("expected preview snapshot for term-2")
			}

			coordinator := &Coordinator{}
			vm := WithRenderFloatingDragPreview(RenderVMFromVisibleState(state), "float-1", tc.positions[0], previewSnapshot)
			benchmarkFrameSink = renderBodyFrameWithCoordinatorVM(coordinator, vm, state.TermSize.Width, FrameBodyHeight(state.TermSize.Height)).Content()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vm := WithRenderFloatingDragPreview(RenderVMFromVisibleState(state), "float-1", tc.positions[i%len(tc.positions)], previewSnapshot)
				benchmarkFrameSink = renderBodyFrameWithCoordinatorVM(coordinator, vm, state.TermSize.Width, FrameBodyHeight(state.TermSize.Height)).Content()
			}
		})
	}
}

func benchmarkCoordinator(tb testing.TB, paneCount, width, height int) *Coordinator {
	tb.Helper()
	state := benchmarkState(tb, paneCount, width, height)
	return NewCoordinator(func() VisibleRenderState { return state })
}

func benchmarkScrollSurface(prefix string, cols, rows, start int) *spriteTestSurface {
	cols = maxInt(1, cols)
	rows = maxInt(1, rows)
	screen := make([][]protocol.Cell, 0, rows)
	for y := 0; y < rows; y++ {
		line := fmt.Sprintf("%s-%03d %s", prefix, start+y, strings.Repeat("x", cols))
		screen = append(screen, protocolRowFromText(line[:cols]))
	}
	cursorRow := maxInt(0, rows-2)
	cursorCol := minInt(cols-1, 12)
	return &spriteTestSurface{
		size:   protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		screen: screen,
		cursor: protocol.CursorState{Row: cursorRow, Col: cursorCol, Visible: true},
		modes:  protocol.TerminalModes{AlternateScreen: true},
	}
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

	state := AdaptVisibleStateWithSize(wb, rt, width, FrameBodyHeight(height))
	state = WithTermSize(state, width, height)
	return state
}

func benchmarkFloatingState(tb testing.TB, width, height int, floatRects []workbench.Rect) (VisibleRenderState, map[string]int) {
	tb.Helper()

	wb := workbench.NewWorkbench()
	tab := &workbench.TabState{
		ID:              "tab-1",
		Name:            "tab 1",
		ActivePaneID:    "pane-1",
		FloatingVisible: true,
		Panes: map[string]*workbench.PaneState{
			"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
		},
		Root: workbench.NewLeaf("pane-1"),
	}
	for i, rect := range floatRects {
		paneID := fmt.Sprintf("float-%d", i+1)
		terminalID := fmt.Sprintf("term-%d", i+2)
		tab.Panes[paneID] = &workbench.PaneState{
			ID:         paneID,
			Title:      paneID,
			TerminalID: terminalID,
		}
		tab.Floating = append(tab.Floating, &workbench.FloatingState{
			PaneID: paneID,
			Rect:   rect,
			Z:      i,
		})
	}
	if len(floatRects) > 0 {
		tab.ActivePaneID = "float-1"
	}

	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs:      []*workbench.TabState{tab},
	})

	rt := runtime.New(nil)
	for i := 0; i <= len(floatRects); i++ {
		terminalID := fmt.Sprintf("term-%d", i+1)
		terminal := rt.Registry().GetOrCreate(terminalID)
		terminal.Name = fmt.Sprintf("worker-%d", i+1)
		terminal.State = "running"
		terminal.Snapshot = benchmarkSnapshot(terminalID, width, height, i)
	}

	state := AdaptVisibleStateWithSize(wb, rt, width, FrameBodyHeight(height))
	state = WithTermSize(state, width, height)

	indexes := make(map[string]int, len(state.Workbench.FloatingPanes))
	for i, pane := range state.Workbench.FloatingPanes {
		indexes[pane.ID] = i
	}
	return state, indexes
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

func benchmarkStyledSnapshot(terminalID string, cols, rows int) *protocol.Snapshot {
	cols = maxInt(1, cols)
	rows = maxInt(1, rows)
	screen := make([][]protocol.Cell, 0, rows)
	palette := []protocol.CellStyle{
		{FG: "#f8fafc", BG: "#0f172a", Bold: true},
		{FG: "#fde68a", BG: "#111827"},
		{FG: "#93c5fd", BG: "#0b1220"},
		{FG: "#86efac", BG: "#111827", Underline: true},
	}
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, 0, cols)
		for x := 0; x < cols; x++ {
			style := palette[(x+y)%len(palette)]
			ch := 'a' + rune((x+y)%26)
			if x%9 == 0 {
				ch = ' '
			}
			row = append(row, protocol.Cell{
				Content: string(ch),
				Width:   1,
				Style:   style,
			})
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
