package surface

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestBuildPaneSurfaceReturnsWaitingCopyForWaitingPane(t *testing.T) {
	pane := types.PaneState{ID: "pane-wait", SlotState: types.PaneSlotWaiting}

	got := BuildPaneSurface(types.AppState{}, pane, nil, 20, 5)
	if len(got.Body) == 0 || got.Body[0] != "waiting slot" {
		t.Fatalf("unexpected waiting body: %+v", got.Body)
	}
}

func TestBuildPaneSurfaceReturnsSnapshotRowsForConnectedPane(t *testing.T) {
	store := stubTerminalStore{
		rows: map[types.TerminalID][]string{
			types.TerminalID("term-1"): {"top", "bottom"},
		},
	}
	pane := types.PaneState{
		ID:         "pane-live",
		TerminalID: "term-1",
		SlotState:  types.PaneSlotConnected,
	}

	got := BuildPaneSurface(types.AppState{}, pane, store, 20, 4)
	if len(got.Body) < 2 {
		t.Fatalf("expected snapshot rows, got %+v", got.Body)
	}
	if got.Body[0] != "top" || got.Body[1] != "bottom" {
		t.Fatalf("unexpected connected body: %+v", got.Body)
	}
}

func TestBuildPaneSurfaceReturnsCopyForEmptyAndExitedPane(t *testing.T) {
	cases := []struct {
		name string
		pane types.PaneState
		want string
	}{
		{
			name: "empty",
			pane: types.PaneState{ID: "pane-empty", SlotState: types.PaneSlotEmpty},
			want: "empty pane",
		},
		{
			name: "exited",
			pane: types.PaneState{ID: "pane-exit", SlotState: types.PaneSlotExited},
			want: "process exited",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildPaneSurface(types.AppState{}, tc.pane, nil, 20, 4)
			if len(got.Body) == 0 || got.Body[0] != tc.want {
				t.Fatalf("unexpected body: %+v", got.Body)
			}
		})
	}
}

type stubTerminalStore struct {
	rows map[types.TerminalID][]string
}

func (s stubTerminalStore) Snapshot(terminalID types.TerminalID) (*protocol.Snapshot, bool) {
	rows, ok := s.rows[terminalID]
	if !ok {
		return nil, false
	}
	screen := make([][]protocol.Cell, 0, len(rows))
	for _, row := range rows {
		screen = append(screen, []protocol.Cell{{Content: row, Width: len(row)}})
	}
	return &protocol.Snapshot{
		TerminalID: string(terminalID),
		Screen: protocol.ScreenData{
			Cells: screen,
		},
		Cursor: protocol.CursorState{Visible: true, Row: len(rows) - 1, Col: 0},
	}, true
}
