package workbench

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func TestWorkbenchViewShowsTopbarPaneTitleAndActionBar(t *testing.T) {
	view := Render(sampleWorkbenchState(), 120, 20)
	if !strings.Contains(view, "[main]") {
		t.Fatal("expected workspace label in top bar")
	}
	if !strings.Contains(view, "shell-dev") {
		t.Fatal("expected pane title")
	}
	if !strings.Contains(view, "owner") {
		t.Fatal("expected pane status metadata")
	}
	if !strings.Contains(view, "<c-p> pane") {
		t.Fatal("expected action bar")
	}
}

func TestUnconnectedPaneShowsActionableEmptyState(t *testing.T) {
	view := Render(sampleUnconnectedWorkbenchState(), 80, 20)
	if !strings.Contains(view, "connect existing terminal") {
		t.Fatal("expected empty-state action text")
	}
	if !strings.Contains(view, "create new terminal") {
		t.Fatal("expected create action text")
	}
	if !strings.Contains(view, "open terminal pool") {
		t.Fatal("expected manager action text")
	}
}

func TestWorkbenchViewRendersLivePaneBodyFromSessionSnapshot(t *testing.T) {
	view := Render(sampleLivePaneWorkbenchState(), 120, 20)
	if !strings.Contains(view, "hello from shell") {
		t.Fatal("expected live pane body content")
	}
}

func sampleWorkbenchState() app.Model {
	model := sampleLivePaneWorkbenchState()
	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:      types.TerminalID("term-1"),
		Name:    "shell-dev",
		Command: []string{"/bin/sh"},
		State:   stateterminal.StateRunning,
	}
	return model
}

func sampleUnconnectedWorkbenchState() app.Model {
	model := app.NewModel()
	ws := workspace.NewTemporary("main")
	model.Workspace = ws
	return model
}

func sampleLivePaneWorkbenchState() app.Model {
	model := app.NewModel()
	ws := workspace.NewTemporary("main")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.SlotState = types.PaneSlotLive
	pane.TerminalID = types.TerminalID("term-1")
	tab.TrackPane(pane)
	model.Workspace = ws
	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:      types.TerminalID("term-1"),
		Name:    "shell-dev",
		Command: []string{"/bin/sh"},
		State:   stateterminal.StateRunning,
	}
	model.Sessions[types.TerminalID("term-1")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Attached:   true,
		Snapshot: &protocol.Snapshot{
			TerminalID: "term-1",
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{
					{
						{Content: "h", Width: 1}, {Content: "e", Width: 1}, {Content: "l", Width: 1}, {Content: "l", Width: 1}, {Content: "o", Width: 1},
						{Content: " ", Width: 1},
						{Content: "f", Width: 1}, {Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "m", Width: 1},
						{Content: " ", Width: 1},
						{Content: "s", Width: 1}, {Content: "h", Width: 1}, {Content: "e", Width: 1}, {Content: "l", Width: 1}, {Content: "l", Width: 1},
					},
				},
			},
		},
	}
	return model
}
