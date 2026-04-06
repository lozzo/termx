package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestComposedCanvasDrawSnapshot(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}}},
	}
	canvas := newComposedCanvas(4, 2)
	canvas.drawSnapshot(snapshot)
	output := canvas.rawString()
	if !strings.Contains(output, "hi") {
		t.Fatalf("expected snapshot text in canvas output, got %q", output)
	}
}

func TestRenderPaneSnapshotUsesRect(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "a", Width: 1}, {Content: "b", Width: 1}},
			{{Content: "c", Width: 1}, {Content: "d", Width: 1}},
		}},
	}
	visibleRuntime := &runtime.VisibleRuntime{Terminals: []runtime.VisibleTerminal{{TerminalID: "term-1", Snapshot: snapshot}}}
	pane := workbench.VisiblePane{ID: "pane-1", TerminalID: "term-1", Rect: workbench.Rect{W: 2, H: 2}}
	lines := renderPaneSnapshot(workbench.Rect{W: 2, H: 2}, pane, visibleRuntime)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ab", "cd"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected pane snapshot output to contain %q, got %q", want, joined)
		}
	}
}

func TestComposedCanvasStyleANSI(t *testing.T) {
	canvas := newComposedCanvas(5, 1)
	canvas.set(0, 0, drawCell{Content: "A", Width: 1, Style: drawStyle{FG: "#ff0000", Bold: true}})
	canvas.set(1, 0, drawCell{Content: "B", Width: 1})
	output := canvas.String()
	if !strings.Contains(output, "A") || !strings.Contains(output, "B") {
		t.Fatalf("expected styled output, got %q", output)
	}
	// Should contain ANSI escape
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI escapes in styled output, got %q", output)
	}
}

func TestComposedCanvasDrawText(t *testing.T) {
	canvas := newComposedCanvas(10, 1)
	canvas.drawText(2, 0, "hello", drawStyle{FG: "#00ff00"})
	output := canvas.rawString()
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected 'hello' in canvas, got %q", output)
	}
}

func TestComposedCanvasDrawTextAdvancesByDisplayWidth(t *testing.T) {
	canvas := newComposedCanvas(8, 1)
	canvas.drawText(0, 0, "[界]X", drawStyle{FG: "#00ff00"})

	if got := canvas.cells[0][1].Content; got != "界" {
		t.Fatalf("expected wide glyph at x=1, got %q", got)
	}
	if !canvas.cells[0][2].Continuation {
		t.Fatalf("expected x=2 to be marked as continuation for wide glyph, got %#v", canvas.cells[0][2])
	}
	if got := canvas.cells[0][4].Content; got != "X" {
		t.Fatalf("expected trailing text to land after wide glyph width, got %q", got)
	}
}

func TestHexToRGB(t *testing.T) {
	rgb, ok := hexToRGB("#ff8000")
	if !ok {
		t.Fatal("expected valid hex")
	}
	if rgb[0] != 255 || rgb[1] != 128 || rgb[2] != 0 {
		t.Fatalf("unexpected rgb: %v", rgb)
	}
}

func TestItoa(t *testing.T) {
	if itoa(0) != "0" {
		t.Fatalf("itoa(0) = %q", itoa(0))
	}
	if itoa(42) != "42" {
		t.Fatalf("itoa(42) = %q", itoa(42))
	}
	if itoa(-7) != "-7" {
		t.Fatalf("itoa(-7) = %q", itoa(-7))
	}
}

func TestRenderFrameUsesSyntheticCursorForActiveShellPane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if strings.Contains(coordinator.CursorSequence(), "\x1b[?25h") {
		t.Fatalf("expected shell pane to keep host cursor hidden, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"})+"h") {
		t.Fatalf("expected shell pane to use synthetic cursor highlight, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
}

func TestRenderFrameHidesHostCursorWhenActivePaneCursorInvisible(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: false},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if !strings.Contains(coordinator.CursorSequence(), "\x1b[?25l") {
		t.Fatalf("expected host cursor hide escape, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
	if strings.Contains(coordinator.CursorSequence(), "\x1b[?25h") {
		t.Fatalf("expected no host cursor show escape when terminal cursor is invisible, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
}

func TestRenderFrameUsesSyntheticCursorForAlternateScreenPane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	frame := coordinator.RenderFrame()

	if strings.Contains(coordinator.CursorSequence(), "\x1b[?25h") {
		t.Fatalf("expected alternate-screen pane to keep host cursor hidden, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"})+"h") {
		t.Fatalf("expected alternate-screen pane to use synthetic cursor highlight, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
	}
}

func TestRenderFrameImmersiveZoomHidesChromeAndOtherPanes(t *testing.T) {
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
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "shell"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				repeatCells("zoom-pane"),
				repeatCells("only-this"),
			},
		},
		Cursor: protocol.CursorState{Visible: false},
	}
	rt.Registry().GetOrCreate("term-2").Name = "logs"
	rt.Registry().Get("term-2").State = "running"
	rt.Registry().Get("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				repeatCells("hidden-pane"),
			},
		},
		Cursor: protocol.CursorState{Visible: false},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 40, 10), 40, 10)
	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())

	if strings.Contains(frame, "tab 1") || strings.Contains(frame, "terminals:") || strings.Contains(frame, "float:") {
		t.Fatalf("expected immersive zoom to hide top/bottom chrome:\n%s", frame)
	}
	if strings.Contains(frame, "┌") || strings.Contains(frame, "│") || strings.Contains(frame, "┘") {
		t.Fatalf("expected immersive zoom to hide pane borders:\n%s", frame)
	}
	if !strings.Contains(frame, "zoom-pane") || !strings.Contains(frame, "only-this") {
		t.Fatalf("expected zoomed pane content to remain visible:\n%s", frame)
	}
	if strings.Contains(frame, "hidden-pane") || strings.Contains(frame, "logs") {
		t.Fatalf("expected non-zoomed pane content to disappear:\n%s", frame)
	}
	if got := len(strings.Split(frame, "\n")); got != 10 {
		t.Fatalf("expected immersive zoom frame height 10, got %d lines:\n%s", got, frame)
	}
}

func TestProjectPaneCursorDrawsSyntheticCursorForPlainShell(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Content != "h" {
		t.Fatalf("expected cursor overlay to preserve cell content, got %#v", cell)
	}
	if cell.Style.Reverse || cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected synthetic cursor to force explicit white cursor colors, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected synthetic cursor path to keep host cursor hidden")
	}
}

func TestRenderFrameUsesHighContrastSyntheticCursorOnBlankShellCell(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 1, Visible: true, Shape: "block"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	want := styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"}) + " "
	if !strings.Contains(frame, want) {
		t.Fatalf("expected blank shell cursor cell to use high-contrast synthetic cursor %q, got %q", want, frame)
	}
}

func TestRenderFrameUsesHighContrastSyntheticCursorOnTextCellWithDefaultColors(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	want := styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"}) + "h"
	if !strings.Contains(frame, want) {
		t.Fatalf("expected text cursor cell to use high-contrast synthetic cursor %q, got %q", want, frame)
	}
}

func TestProjectPaneCursorUsesVisibleBarCursorStyleOnTextCell(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "bar"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Style.Reverse {
		t.Fatalf("expected bar cursor to avoid reverse-video fallback, got %#v", cell.Style)
	}
	if cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected bar cursor to force visible fallback colors, got %#v", cell.Style)
	}
}

func TestRenderFrameHidesBlinkingSyntheticCursorOffPhase(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block", Blink: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	_ = coordinator.RenderFrame()
	coordinator.AdvanceCursorBlink()
	frame := coordinator.RenderFrame()
	highlight := styleANSI(drawStyle{FG: "#000000", BG: "#ffffff"}) + "h"
	if strings.Contains(frame, highlight) {
		t.Fatalf("expected blinking synthetic cursor to disappear during off phase, got %q", frame)
	}
}

func TestCoordinatorNeedsCursorTicksForVisibleActivePane(t *testing.T) {
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
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "bar"},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	if !coordinator.NeedsCursorTicks() {
		t.Fatal("expected visible active pane cursor to request render ticks")
	}
}

func TestProjectPaneCursorDrawsSyntheticCursorOnCanvas(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
		Modes:  protocol.TerminalModes{AlternateScreen: true},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 0)

	cell := canvas.cells[1][1]
	if cell.Content != "h" {
		t.Fatalf("expected cursor overlay to preserve cell content, got %#v", cell)
	}
	if cell.Style.Reverse || cell.Style.FG != "#000000" || cell.Style.BG != "#ffffff" {
		t.Fatalf("expected cursor overlay to use explicit white cursor colors, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected synthetic cursor overlay to avoid host cursor movement")
	}
}

func TestProjectPaneCursorHidesWhenViewingScrollback(t *testing.T) {
	canvas := newComposedCanvas(6, 3)
	rect := workbench.Rect{X: 1, Y: 1, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	fillRect(canvas, rect, blankDrawCell())
	canvas.drawSnapshotInRect(rect, snapshot)
	projectPaneCursor(canvas, rect, snapshot, 1)

	cell := canvas.cells[1][1]
	if cell.Style.Reverse {
		t.Fatalf("expected scrollback view to avoid synthetic cursor highlight, got %#v", cell.Style)
	}
	if canvas.cursorVisible {
		t.Fatalf("expected scrollback view to keep host cursor hidden")
	}
}

func TestDrawSnapshotWithOffsetMarksPanelAreaOutsideTerminalWithDots(t *testing.T) {
	canvas := newComposedCanvas(4, 3)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 3}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 2, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}, {Content: "i", Width: 1}}},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "hi··\n····\n····" {
		t.Fatalf("expected dot markers outside terminal extent, got %q", got)
	}
}

func TestDrawSnapshotWithOffsetUsesSnapshotHeightWhenRenderedRowsLagAfterShrink(t *testing.T) {
	canvas := newComposedCanvas(4, 3)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 3}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 4, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{{Content: "a", Width: 1}},
				{{Content: "b", Width: 1}},
				{{Content: "c", Width: 1}},
			},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "a\n····\n····" {
		t.Fatalf("expected resized terminal height to blank stale lower rows with dots, got %q", got)
	}
}

func TestDrawSnapshotWithOffsetUsesSnapshotWidthWhenRenderedColsLagAfterShrink(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	rect := workbench.Rect{X: 0, Y: 0, W: 4, H: 1}
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 1, Rows: 1},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "a", Width: 1},
				{Content: "b", Width: 1},
				{Content: "c", Width: 1},
			}},
		},
	}

	fillRect(canvas, rect, blankDrawCell())
	drawSnapshotWithOffset(canvas, rect, snapshot, 0, defaultUITheme())

	if got := canvas.rawString(); got != "a···" {
		t.Fatalf("expected resized terminal width to blank stale right-side cells with dots, got %q", got)
	}
}

func TestActiveEntryCursorTargetUsesFramelessRectForZoomedPane(t *testing.T) {
	entries := []paneRenderEntry{{
		PaneID:     "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 10, H: 4},
		Frameless:  true,
		Active:     true,
	}}
	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 10, Rows: 4},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
			},
		}},
	}

	rect, snapshot, ok := activeEntryCursorTarget(entries, runtimeState)
	if !ok || snapshot == nil {
		t.Fatalf("expected active cursor target, got rect=%#v snapshot=%#v ok=%v", rect, snapshot, ok)
	}
	if rect != (workbench.Rect{X: 0, Y: 0, W: 10, H: 4}) {
		t.Fatalf("expected frameless zoom cursor rect to stay unshifted, got %#v", rect)
	}
}

func TestDrawPaneFrameMarksOverflowWithStableCornerIndicators(t *testing.T) {
	canvas := newComposedCanvas(6, 4)
	rect := workbench.Rect{X: 0, Y: 0, W: 6, H: 4}

	drawPaneFrame(canvas, rect, "", paneBorderInfo{}, defaultUITheme(), paneOverflowHints{Right: true, Bottom: true}, false, false)

	lines := strings.Split(canvas.rawString(), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 rendered lines, got %d", len(lines))
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("expected top-right corner to remain solid, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[3], "└") || !strings.HasSuffix(lines[3], "┘") {
		t.Fatalf("expected bottom corners to remain solid, got %q", lines[3])
	}
	if !strings.HasSuffix(lines[2], ">") {
		t.Fatalf("expected right overflow marker near bottom-right edge, got %q", lines[2])
	}
	if !strings.Contains(lines[3], "v┘") {
		t.Fatalf("expected bottom overflow marker before bottom-right corner, got %q", lines[3])
	}
}

func TestDrawPaneFrameHighlightsOverflowMarkersForActivePane(t *testing.T) {
	canvas := newComposedCanvas(8, 5)
	rect := workbench.Rect{X: 0, Y: 0, W: 8, H: 5}
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)

	drawPaneFrame(canvas, rect, "", paneBorderInfo{}, theme, paneOverflowHints{Right: true, Bottom: true}, true, false)

	rightMarker := canvas.cells[rect.Y+rect.H-2][rect.X+rect.W-1]
	bottomMarker := canvas.cells[rect.Y+rect.H-1][rect.X+rect.W-2]
	borderCell := canvas.cells[rect.Y][rect.X+rect.W-1]

	if rightMarker.Style.FG == "" || bottomMarker.Style.FG == "" {
		t.Fatalf("expected active overflow markers to use explicit colors, got right=%#v bottom=%#v", rightMarker.Style, bottomMarker.Style)
	}
	if rightMarker.Style.FG == borderCell.Style.FG {
		t.Fatalf("expected active right marker color to differ from border, both %q", rightMarker.Style.FG)
	}
	if bottomMarker.Style.FG == borderCell.Style.FG {
		t.Fatalf("expected active bottom marker color to differ from border, both %q", bottomMarker.Style.FG)
	}
	if !rightMarker.Style.Bold || !bottomMarker.Style.Bold {
		t.Fatalf("expected active overflow markers to be bold, got right=%#v bottom=%#v", rightMarker.Style, bottomMarker.Style)
	}
}
