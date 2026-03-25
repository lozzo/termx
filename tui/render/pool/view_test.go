package pool

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func TestGroupTerminalsIntoVisibleParkedExited(t *testing.T) {
	model := samplePoolModel()

	grouped := GroupTerminals(model)
	if len(grouped.Visible) != 1 || grouped.Visible[0].TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected visible term-1, got %#v", grouped.Visible)
	}
	if len(grouped.Parked) != 1 || grouped.Parked[0].TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected parked term-2, got %#v", grouped.Parked)
	}
	if len(grouped.Exited) != 1 || grouped.Exited[0].TerminalID != types.TerminalID("term-3") {
		t.Fatalf("expected exited term-3, got %#v", grouped.Exited)
	}
}

func TestTerminalPoolViewShowsThreeColumns(t *testing.T) {
	view := Render(samplePoolModel(), 120, 30)
	for _, want := range []string{
		"TERMINALS",
		"LIVE PREVIEW",
		"DETAILS",
		"VISIBLE",
		"PARKED",
		"EXITED",
		"connections",
		"metadata first",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestTerminalPoolPreviewIsReadonlyLiveAttach(t *testing.T) {
	view := Render(samplePoolModel(), 120, 30)
	if !strings.Contains(view, "read-only live observe") {
		t.Fatalf("expected readonly preview copy, got:\n%s", view)
	}
}

func samplePoolModel() app.Model {
	model := app.NewModel()
	ws := workspace.NewTemporary("main")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.SlotState = types.PaneSlotLive
	pane.TerminalID = types.TerminalID("term-1")
	tab.TrackPane(pane)
	model.Workspace = ws
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool = app.TerminalPoolState{
		Query:              "",
		SelectedTerminalID: types.TerminalID("term-1"),
		PreviewTerminalID:  types.TerminalID("term-1"),
		PreviewReadonly:    true,
	}
	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-1"),
		Name:            "api-dev",
		Command:         []string{"bash", "-lc", "npm run dev"},
		Tags:            map[string]string{"team": "backend"},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
		LastInteraction: time.Unix(30, 0),
	}
	model.Terminals[types.TerminalID("term-2")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-2"),
		Name:            "worker-tail",
		Command:         []string{"bash", "-lc", "tail -f worker.log"},
		Tags:            map[string]string{"team": "ops"},
		State:           stateterminal.StateRunning,
		LastInteraction: time.Unix(20, 0),
	}
	model.Terminals[types.TerminalID("term-3")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-3"),
		Name:            "old-api",
		Command:         []string{"bash", "-lc", "npm run dev"},
		Tags:            map[string]string{"team": "legacy"},
		State:           stateterminal.StateExited,
		LastInteraction: time.Unix(10, 0),
	}
	model.Sessions[types.TerminalID("term-1")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Attached:   true,
		ReadOnly:   true,
		Preview:    true,
		Snapshot: &protocol.Snapshot{
			TerminalID: "term-1",
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{
					{
						{Content: "$", Width: 1},
						{Content: " ", Width: 1},
						{Content: "n", Width: 1},
						{Content: "p", Width: 1},
						{Content: "m", Width: 1},
					},
				},
			},
		},
	}
	return model
}
