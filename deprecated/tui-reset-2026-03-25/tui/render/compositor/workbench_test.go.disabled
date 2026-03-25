package compositor

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render/surface"
)

func TestComposeWorkbenchDrawsTwoTiledPanes(t *testing.T) {
	view := newSplitWorkbenchProjection()
	lines := ComposeWorkbench(view).Lines()

	if !strings.Contains(strings.Join(lines, "\n"), "pane-left") {
		t.Fatal("expected left pane title")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "pane-right") {
		t.Fatal("expected right pane title")
	}
}

func TestComposeWorkbenchMarksActivePaneAndCursor(t *testing.T) {
	lines := ComposeWorkbench(newSplitWorkbenchProjection()).Lines()
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "* pane-left") {
		t.Fatalf("expected active pane marker, got %q", joined)
	}
	if !strings.Contains(joined, "█") {
		t.Fatalf("expected cursor placeholder, got %q", joined)
	}
}

func newSplitWorkbenchProjection() View {
	return View{
		Width:  32,
		Height: 8,
		Panes: []Pane{
			{
				ID:     types.PaneID("pane-left"),
				Rect:   types.Rect{X: 0, Y: 0, W: 16, H: 8},
				Active: true,
				Surface: surface.Pane{
					Title: "pane-left",
					Body:  []string{"left body"},
					Cursor: surface.Cursor{
						Visible: true,
						Row:     0,
						Col:     0,
					},
				},
			},
			{
				ID:     types.PaneID("pane-right"),
				Rect:   types.Rect{X: 16, Y: 0, W: 16, H: 8},
				Active: false,
				Surface: surface.Pane{
					Title: "pane-right",
					Body:  []string{"right body"},
				},
			},
		},
	}
}
