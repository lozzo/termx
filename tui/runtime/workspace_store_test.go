package runtime

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func TestWorkspaceStoreRoundTripsWorkbenchState(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir() + "/workspace-state.json")
	original := sampleWorkbenchStateForRuntimeTest()

	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("save returned error: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load returned error: %v", err)
	}

	if !reflect.DeepEqual(persistedModelSnapshot(original), persistedModelSnapshot(loaded)) {
		t.Fatalf("workspace mismatch after round trip:\nwant: %#v\ngot: %#v", persistedModelSnapshot(original), persistedModelSnapshot(loaded))
	}
}

func TestRebindRestoredModelKeepsTerminalPoolPreviewAsLiveStream(t *testing.T) {
	client := &stubClient{
		attachResult: &protocol.AttachResult{Channel: 21, Mode: "observer"},
		snapshotByID: map[string][]*protocol.Snapshot{
			"term-2": {sampleSnapshotForRuntimeTest("term-2", "restored preview")},
		},
	}
	model := sampleWorkbenchStateForRuntimeTest()
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool.PreviewTerminalID = types.TerminalID("term-2")
	model.Pool.SelectedTerminalID = types.TerminalID("term-2")
	model.Pool.PreviewReadonly = true
	model.Pool.PreviewSubscriptionRevision = 7

	restored := RebindRestoredModel(context.Background(), client, model)
	if restored.PreviewStreamNext == nil {
		t.Fatal("expected restore to wire preview stream command")
	}
	if !client.hasAttachCall("term-2", "observer") {
		t.Fatalf("expected preview restore to attach term-2 as observer, got ids=%v modes=%v", client.attachIDs, client.attachModes)
	}

	stream, ok := client.streams[21]
	if !ok {
		t.Fatalf("expected preview stream to bind channel 21, got streams=%v", client.streams)
	}
	go func() {
		stream <- protocol.StreamFrame{Payload: []byte("tick")}
	}()

	nextCmd := restored.PreviewStreamNext
	if nextCmd == nil {
		t.Fatal("expected preview stream cmd to be set")
	}
	cmd := nextCmd()
	if cmd == nil {
		t.Fatal("expected preview stream tea cmd to be set")
	}
	msg := cmd()
	previewMsg, ok := msg.(app.PreviewStreamMessage)
	if !ok {
		t.Fatalf("expected preview stream message, got %T", msg)
	}
	if previewMsg.TerminalID != types.TerminalID("term-2") || previewMsg.Revision != 7 {
		t.Fatalf("expected restored preview binding revision 7 for term-2, got %#v", previewMsg)
	}
}

func sampleWorkbenchStateForRuntimeTest() app.Model {
	model := app.NewModel()
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool.Query = "ops"
	model.Pool.SelectedTerminalID = types.TerminalID("term-2")
	model.Pool.PreviewTerminalID = types.TerminalID("term-2")
	model.Pool.PreviewReadonly = true
	model.Pool.PreviewSubscriptionRevision = 3

	ws := workspace.NewTemporary("restored")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.TerminalID = types.TerminalID("term-1")
	pane.SlotState = types.PaneSlotLive
	tab.TrackPane(pane)
	tab.Title = "dev-shell"

	floatPane := workspace.PaneState{
		ID:         types.PaneID("float-1"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-2"),
		Rect:       types.Rect{X: 12, Y: 4, W: 48, H: 18},
	}
	tab.TrackPane(floatPane)
	tab.ActivePaneID = pane.ID
	model.Workspace = ws

	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-1"),
		Name:            "shell",
		Command:         []string{"/bin/sh"},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
		LastInteraction: time.Unix(10, 0).UTC(),
		LastOutputAt:    time.Unix(11, 0).UTC(),
	}
	model.Terminals[types.TerminalID("term-2")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-2"),
		Name:            "worker",
		Command:         []string{"bash", "-lc", "npm run dev"},
		Tags:            map[string]string{"role": "ops"},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     types.PaneID("float-1"),
		AttachedPaneIDs: []types.PaneID{types.PaneID("float-1")},
		LastInteraction: time.Unix(20, 0).UTC(),
		LastOutputAt:    time.Unix(21, 0).UTC(),
	}
	model.Sessions[types.TerminalID("term-1")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Attached:   true,
		Snapshot:   sampleSnapshotForRuntimeTest("term-1", "echo ready"),
	}
	model.Sessions[types.TerminalID("term-2")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-2"),
		Channel:    8,
		Attached:   true,
		ReadOnly:   true,
		Preview:    true,
		Snapshot:   sampleSnapshotForRuntimeTest("term-2", "tail -f app.log"),
	}
	return model
}

func sampleSnapshotForRuntimeTest(terminalID string, line string) *protocol.Snapshot {
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{
				{Content: "$"},
				{Content: " "},
				{Content: line},
			}},
		},
		Timestamp: time.Unix(30, 0).UTC(),
	}
}

func persistedModelSnapshot(model app.Model) any {
	return struct {
		Screen    app.Screen
		Focus     app.FocusTarget
		Pool      app.TerminalPoolState
		Workspace *workspace.WorkspaceState
		Terminals map[types.TerminalID]stateterminal.Metadata
		Sessions  map[types.TerminalID]app.TerminalSession
	}{
		Screen:    model.Screen,
		Focus:     model.FocusTarget,
		Pool:      model.Pool,
		Workspace: model.Workspace,
		Terminals: model.Terminals,
		Sessions:  model.Sessions,
	}
}
