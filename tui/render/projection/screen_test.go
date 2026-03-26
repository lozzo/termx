package projection

import (
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureworkbench "github.com/lozzow/termx/tui/features/workbench"
)

func TestProjectWorkbenchIncludesPaneStateAndSnapshotBody(t *testing.T) {
	model := sampleProjectionModel()

	screen := Project(model, 120, 40)
	if screen.Screen != app.ScreenWorkbench {
		t.Fatalf("expected workbench screen, got %q", screen.Screen)
	}
	if len(screen.Panes) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(screen.Panes))
	}
	if screen.Panes[0].Body == "" {
		t.Fatal("expected projected live pane body")
	}
	if screen.Panes[1].Status != "exited" {
		t.Fatalf("expected exited status, got %q", screen.Panes[1].Status)
	}
	if screen.Panes[2].Status != "unconnected" {
		t.Fatalf("expected unconnected status, got %q", screen.Panes[2].Status)
	}
}

func TestProjectCarriesOverlayKind(t *testing.T) {
	model := sampleProjectionModel()
	model.Overlay = featureoverlay.State{Active: featureoverlay.ActiveState{Kind: featureoverlay.KindConnectPicker}}

	screen := Project(model, 120, 40)
	if screen.OverlayKind != featureoverlay.KindConnectPicker {
		t.Fatalf("expected connect picker overlay, got %q", screen.OverlayKind)
	}
}

func sampleProjectionModel() app.Model {
	ws := coreworkspace.New("main")
	tab := ws.ActiveTab()
	tab.TrackPane(coreworkspace.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-live"),
	})
	tab.TrackPane(coreworkspace.PaneState{
		ID:         types.PaneID("pane-exited"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotExited,
		TerminalID: types.TerminalID("term-exited"),
	})
	tab.TrackPane(coreworkspace.PaneState{
		ID:        types.PaneID("pane-empty"),
		Kind:      types.PaneKindTiled,
		SlotState: types.PaneSlotUnconnected,
	})
	tab.ActivePaneID = types.PaneID("pane-1")

	return app.Model{
		WorkspaceName: "main",
		Screen:        app.ScreenWorkbench,
		Workbench: featureworkbench.State{
			Workspace: ws,
			Terminals: map[types.TerminalID]coreterminal.Metadata{
				types.TerminalID("term-live"): {
					ID:              types.TerminalID("term-live"),
					Name:            "shell-live",
					State:           coreterminal.StateRunning,
					OwnerPaneID:     types.PaneID("pane-1"),
					AttachedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
				},
				types.TerminalID("term-exited"): {
					ID:              types.TerminalID("term-exited"),
					Name:            "shell-exited",
					State:           coreterminal.StateExited,
					OwnerPaneID:     types.PaneID("pane-exited"),
					AttachedPaneIDs: []types.PaneID{types.PaneID("pane-exited")},
				},
			},
			Sessions: map[types.TerminalID]featureworkbench.SessionState{
				types.TerminalID("term-live"): {
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-live",
						Timestamp:  time.Unix(10, 0),
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "hello from shell"}},
							},
						},
					},
				},
				types.TerminalID("term-exited"): {
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-exited",
						Timestamp:  time.Unix(20, 0),
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "process done"}},
							},
						},
					},
				},
			},
		},
	}
}
