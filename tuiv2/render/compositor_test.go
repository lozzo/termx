package render

import (
	"strings"
	"testing"

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

func TestRenderFrameProjectsActivePaneCursor(t *testing.T) {
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
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()

	if !strings.Contains(frame, "\x1b[?25h\x1b[3;2H") {
		t.Fatalf("expected active pane cursor escape in frame, got %q", frame)
	}
	if cursorIdx := strings.Index(frame, "\x1b[?25h\x1b[3;2H"); cursorIdx <= strings.LastIndex(frame, "ws:main") {
		t.Fatalf("expected cursor escape to be emitted after the status bar, got %q", frame)
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
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()

	if !strings.Contains(frame, "\x1b[?25l") {
		t.Fatalf("expected host cursor hide escape in frame, got %q", frame)
	}
	if strings.Contains(frame, "\x1b[?25h") {
		t.Fatalf("expected no host cursor show escape when terminal cursor is invisible, got %q", frame)
	}
}
