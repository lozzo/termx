package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
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
