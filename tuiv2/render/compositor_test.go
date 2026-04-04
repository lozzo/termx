package render

import (
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#111111", BG: "#f5f5f5", Reverse: true})+"h") {
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
	if !strings.Contains(frame, styleANSI(drawStyle{FG: "#111111", BG: "#f5f5f5", Reverse: true})+"h") {
		t.Fatalf("expected alternate-screen pane to use synthetic cursor highlight, got frame=%q cursor=%q", frame, coordinator.CursorSequence())
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
	if !cell.Style.Reverse {
		t.Fatalf("expected synthetic cursor to reverse the active cell style, got %#v", cell.Style)
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
	want := styleANSI(drawStyle{FG: "#111111", BG: "#f5f5f5", Reverse: true}) + " "
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
	want := styleANSI(drawStyle{FG: "#111111", BG: "#f5f5f5", Reverse: true}) + "h"
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
	if !cell.Style.Reverse {
		t.Fatalf("expected bar cursor to reverse active cell style, got %#v", cell.Style)
	}
	if cell.Style.FG != "#111111" || cell.Style.BG != "#f5f5f5" {
		t.Fatalf("expected bar cursor to force visible fallback colors, got %#v", cell.Style)
	}
}

func TestRenderFrameHidesBlinkingSyntheticCursorOffPhase(t *testing.T) {
	prevNow := blinkTimeNow
	blinkTimeNow = func() time.Time { return time.Unix(0, int64(CursorBlinkInterval)) }
	defer func() { blinkTimeNow = prevNow }()

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
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	highlight := styleANSI(drawStyle{FG: "#111111", BG: "#f5f5f5", Reverse: true}) + "h"
	if strings.Contains(frame, highlight) {
		t.Fatalf("expected blinking synthetic cursor to disappear during off phase, got %q", frame)
	}
}

func TestCoordinatorNeedsCursorTicksForBlinkingActivePane(t *testing.T) {
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
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "bar", Blink: true},
	}

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 20, 4), 20, 6)
	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	if !coordinator.NeedsCursorTicks() {
		t.Fatal("expected blinking active pane cursor to request render ticks")
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
	if !cell.Style.Reverse {
		t.Fatalf("expected cursor overlay to reverse the active cell style, got %#v", cell.Style)
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

func TestDrawPaneFrameMarksOverflowEdgesWithDashedBorder(t *testing.T) {
	canvas := newComposedCanvas(6, 4)
	rect := workbench.Rect{X: 0, Y: 0, W: 6, H: 4}

	drawPaneFrame(canvas, rect, "", paneBorderInfo{}, defaultUITheme(), paneOverflowHints{Right: true, Bottom: true}, false, false)

	lines := strings.Split(canvas.rawString(), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 rendered lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "┆") || !strings.Contains(lines[2], "┆") {
		t.Fatalf("expected dashed right border, got %q / %q", lines[1], lines[2])
	}
	if !strings.Contains(lines[3], "┄") {
		t.Fatalf("expected dashed bottom border, got %q", lines[3])
	}
}
