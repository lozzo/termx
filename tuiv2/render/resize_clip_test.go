package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestFillRectClipsBlankFastPathToCanvas(t *testing.T) {
	canvas := newComposedCanvas(5, 2)
	fillRect(canvas, workbench.Rect{X: 3, Y: 0, W: 5, H: 1}, blankDrawCell())

	if got := canvas.rawString(); got == "" {
		t.Fatal("expected canvas output after clipped fill")
	}
}

func TestRenderBodyClipsFloatingPaneAfterViewportShrink(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root: workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{
				PaneID: "pane-2",
				Rect:   workbench.Rect{X: 8, Y: 2, W: 24, H: 6},
				Z:      1,
			}},
		}},
	})

	body := xansi.Strip(renderBody(WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 12, 6), 12, 8), 12, 6))
	if lines := strings.Split(body, "\n"); len(lines) != 6 {
		t.Fatalf("expected body height to remain bounded after clipping, got %d lines:\n%s", len(lines), body)
	}
}

func TestDrawSnapshotInRectClipsWideCellAtPaneEdge(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	snapshot := &protocol.Snapshot{
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "A", Width: 1},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
			}},
		},
	}

	canvas.drawSnapshotInRect(workbench.Rect{X: 0, Y: 0, W: 2, H: 1}, snapshot)

	if got := canvas.cells[0][0].Content; got != "A" {
		t.Fatalf("expected leading cell to render, got %q", got)
	}
	if got := canvas.cells[0][1].Content; got != " " {
		t.Fatalf("expected wide cell at pane edge to be clipped, got %#v", canvas.cells[0][1])
	}
	if got := canvas.cells[0][2].Content; got != " " || canvas.cells[0][2].Continuation {
		t.Fatalf("expected no continuation spill outside pane rect, got %#v", canvas.cells[0][2])
	}
}

func TestDrawSnapshotWithOffsetClipsWideCellAtPaneEdge(t *testing.T) {
	canvas := newComposedCanvas(4, 1)
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 3, Rows: 1},
		Scrollback: [][]protocol.Cell{{
			{Content: "A", Width: 1},
			{Content: "好", Width: 2},
			{Content: "", Width: 0},
		}},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "Z", Width: 1},
			}},
		},
	}

	drawSnapshotWithOffset(canvas, workbench.Rect{X: 0, Y: 0, W: 2, H: 1}, snapshot, 1, uiTheme{panelBorder: "#6b7280"})

	if got := canvas.cells[0][0].Content; got != "A" {
		t.Fatalf("expected scrollback row to render, got %q", got)
	}
	if got := canvas.cells[0][1].Content; got != " " {
		t.Fatalf("expected clipped wide scrollback cell to leave pane edge blank, got %#v", canvas.cells[0][1])
	}
	if got := canvas.cells[0][2].Content; got != " " || canvas.cells[0][2].Continuation {
		t.Fatalf("expected no scrollback continuation spill outside pane rect, got %#v", canvas.cells[0][2])
	}
}
