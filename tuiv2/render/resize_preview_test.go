package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderPipelineShowsNonAltResizePreviewReflowRows(t *testing.T) {
	snapshot := renderPreviewSnapshot("term-1", 20, 6, []string{
		"COL_A",
		"COL_B",
		"COL_C",
	})
	frame := renderSinglePaneSnapshotForResizePreview(t, snapshot, false)

	for _, want := range []string{"COL_A", "COL_B", "COL_C"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected rendered preview frame to contain %q, got:\n%s", want, frame)
		}
	}
}

func TestRenderPipelineShowsExpandedResizePreviewRestore(t *testing.T) {
	snapshot := renderPreviewSnapshot("term-1", 64, 3, []string{
		"COL_A                 COL_B                 COL_C",
	})
	frame := renderSinglePaneSnapshotForResizePreview(t, snapshot, false)

	if !strings.Contains(frame, "COL_A                 COL_B                 COL_C") {
		t.Fatalf("expected rendered expanded preview to restore original row, got:\n%s", frame)
	}
}

func TestRenderPipelineKeepsAltResizePreviewCroppedGrid(t *testing.T) {
	snapshot := renderPreviewSnapshot("term-1", 3, 2, []string{"ABC", "UVW"})
	snapshot.Screen.IsAlternateScreen = true
	snapshot.Modes.AlternateScreen = true
	frame := renderSinglePaneSnapshotForResizePreview(t, snapshot, true)

	if !strings.Contains(frame, "ABC") || !strings.Contains(frame, "UVW") {
		t.Fatalf("expected rendered alt preview to contain cropped grid rows, got:\n%s", frame)
	}
	if strings.Contains(frame, "DEF") || strings.Contains(frame, "XYZ") {
		t.Fatalf("expected rendered alt preview not to reflow cropped source cells, got:\n%s", frame)
	}
}

func renderSinglePaneSnapshotForResizePreview(t *testing.T, snapshot *protocol.Snapshot, zoomed bool) string {
	t.Helper()
	wb := workbench.NewWorkbench()
	tab := &workbench.TabState{
		ID:           "tab-1",
		Name:         "tab 1",
		ActivePaneID: "pane-1",
		Panes: map[string]*workbench.PaneState{
			"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
		},
		Root: workbench.NewLeaf("pane-1"),
	}
	if zoomed {
		tab.ZoomedPaneID = "pane-1"
	}
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs:      []*workbench.TabState{tab},
	})

	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Name = "shell"
	terminal.State = "running"
	terminal.Snapshot = snapshot
	terminal.PreferSnapshot = true
	terminal.SnapshotVersion = 1
	terminal.SurfaceVersion = 1

	width := int(snapshot.Size.Cols) + 2
	height := int(snapshot.Size.Rows) + 4
	if zoomed {
		width = int(snapshot.Size.Cols)
		height = int(snapshot.Size.Rows) + 2
	}
	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, width, height-2), width, height)
	return xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
}

func renderPreviewSnapshot(terminalID string, cols, rows uint16, lines []string) *protocol.Snapshot {
	grid := make([][]protocol.Cell, rows)
	for row := range grid {
		grid[row] = make([]protocol.Cell, cols)
		for col := range grid[row] {
			grid[row][col] = protocol.Cell{Content: " ", Width: 1}
		}
	}
	for row, line := range lines {
		if row >= len(grid) {
			break
		}
		for col, ch := range line {
			if col >= int(cols) {
				break
			}
			grid[row][col] = protocol.Cell{Content: string(ch), Width: 1}
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: cols, Rows: rows},
		Screen:     protocol.ScreenData{Cells: grid},
		Cursor:     protocol.CursorState{Visible: false},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}
