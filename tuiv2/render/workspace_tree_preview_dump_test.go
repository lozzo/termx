package render

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestWorkbenchNavigatorPanePreviewKeepsRightGutterClear(t *testing.T) {
	frame := workbenchNavigatorPreviewFrame(t, 2)
	assertRightGutterClear(t, frame, 9, 49)
}

func TestWorkbenchNavigatorTabPreviewKeepsRightGutterClear(t *testing.T) {
	frame := workbenchNavigatorPreviewFrame(t, 1)
	assertRightGutterClear(t, frame, 9, 49)
}

func workbenchNavigatorPreviewFrame(t *testing.T, selected int) string {
	t.Helper()
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "123", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Name = "123"
	terminal.State = "running"
	terminal.Snapshot = snapshotWithRightEdgeMarks("term-1", 104, 40)

	items := []modal.WorkspacePickerItem{
		{Kind: modal.WorkspacePickerItemWorkspace, Name: "main", WorkspaceName: "main", Current: true, Active: true, TabCount: 1, PaneCount: 1},
		{Kind: modal.WorkspacePickerItemTab, Name: "1", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, Depth: 1, Active: true, PaneCount: 1},
		{Kind: modal.WorkspacePickerItemPane, Name: "123", WorkspaceName: "main", TabID: "tab-1", TabName: "1", PaneID: "pane-1", TerminalID: "term-1", Depth: 2, Active: true, State: "running", Role: "owner"},
	}
	vm := WithRenderTermSize(AdaptRenderVMWithSize(wb, rt, 220, 58), 220, 60)
	vm = WithRenderStatus(vm, "", "", string(input.ModeWorkspacePicker))
	vm = AttachRenderWorkspacePicker(vm, &modal.WorkspacePickerState{Title: "Workbench Navigator", Items: items, Selected: selected})
	return xansi.Strip(NewCoordinatorWithVM(func() RenderVM { return vm }).RenderFrame())
}

func snapshotWithRightEdgeMarks(id string, cols, rows int) *protocol.Snapshot {
	cells := make([][]protocol.Cell, rows)
	for y := 0; y < rows; y++ {
		row := make([]protocol.Cell, cols)
		for x := range row {
			row[x] = protocol.Cell{Content: " ", Width: 1}
		}
		text := fmt.Sprintf("row %02d", y)
		for x, r := range text {
			row[x] = protocol.Cell{Content: string(r), Width: 1}
		}
		if y%3 == 0 {
			row[7] = protocol.Cell{Content: "§", Width: 1}
		}
		if y%3 == 1 {
			row[7] = protocol.Cell{Content: "♻️", Width: 2}
			row[8] = protocol.Cell{}
		}
		if y%3 == 2 {
			row[7] = protocol.Cell{Content: "界", Width: 2}
			row[8] = protocol.Cell{}
		}
		for _, x := range []int{cols - 3, cols - 1} {
			if x >= 0 && x < cols {
				row[x] = protocol.Cell{Content: "│", Width: 1}
			}
		}
		cells[y] = row
	}
	return &protocol.Snapshot{
		TerminalID: id,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:     protocol.ScreenData{Cells: cells},
	}
}

func assertRightGutterClear(t *testing.T, frame string, startY, endY int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	for y := startY; y < endY && y < len(lines); y++ {
		runes := []rune(lines[y])
		last := -1
		for x, r := range runes {
			if r != ' ' {
				last = x
			}
		}
		if last < 0 {
			continue
		}
		start := last - 8
		if start < 0 {
			start = 0
		}
		tail := string(runes[start : last+1])
		if strings.TrimSpace(tail) != "│" {
			t.Fatalf("preview right gutter contains terminal border-like content at row %d: %q", y, tail)
		}
	}
}
