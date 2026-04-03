package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
)

func TestTerminalPoolPageRendersGroupedListLivePreviewAndRelationships(t *testing.T) {
	state := makeTestState()
	state.TerminalPool = &modal.TerminalManagerState{
		Title:    "Terminal Pool",
		Footer:   "[Enter] here  [Ctrl-T] tab  [Ctrl-O] float  [Ctrl-E] edit  [Ctrl-K] kill  [Esc] close",
		Selected: 0,
		Items: []modal.PickerItem{
			{
				TerminalID:  "term-1",
				Name:        "shell",
				State:       "visible",
				Command:     "bash -lc htop",
				Location:    "main/tab 1/pane-1",
				Description: "running · 2 panes bound",
				Observed:    true,
			},
			{
				TerminalID:  "term-2",
				Name:        "logs",
				State:       "parked",
				Command:     "tail -f /tmp/app.log",
				Location:    "main/tab 2/pane-2",
				Description: "running · 1 pane bound",
			},
			{
				TerminalID:  "term-3",
				Name:        "job",
				State:       "exited",
				Description: "exited (23) · 0 panes bound",
			},
		},
	}
	state.Runtime = &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{
			{
				TerminalID:   "term-1",
				Name:         "shell",
				State:        "running",
				OwnerPaneID:  "pane-1",
				BoundPaneIDs: []string{"pane-1", "pane-9"},
				Snapshot: &protocol.Snapshot{
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{
							{{Content: "l", Width: 1}, {Content: "i", Width: 1}, {Content: "v", Width: 1}, {Content: "e", Width: 1}, {Content: " ", Width: 1}, {Content: "p", Width: 1}, {Content: "r", Width: 1}, {Content: "e", Width: 1}, {Content: "v", Width: 1}, {Content: "i", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}},
						},
					},
				},
			},
		},
	}
	state = WithTermSize(state, 120, 22)
	state = WithStatus(state, "", "", "terminal-manager")

	frame := xansi.Strip(NewCoordinator(func() VisibleRenderState { return state }).RenderFrame())
	for _, want := range []string{
		"VISIBLE",
		"PARKED",
		"EXITED",
		"PREVIEW",
		"DETAIL",
		"live preview",
		"owner pane: pane-1",
		"bound panes: 2",
		"command: bash -lc htop",
		"location: main/tab 1/pane-1",
		"running · 2 panes bound",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected terminal pool frame to contain %q:\n%s", want, frame)
		}
	}
}
