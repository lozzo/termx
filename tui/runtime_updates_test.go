package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app/intent"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRuntimeTerminalStoreAppliesOutputFrameToSnapshot(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 4, Rows: 1},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})

	if err := store.WriteOutput(types.TerminalID("term-1"), []byte("!")); err != nil {
		t.Fatalf("expected output frame to apply, got %v", err)
	}
	snapshot, ok := store.Snapshot(types.TerminalID("term-1"))
	if !ok || snapshot == nil {
		t.Fatal("expected updated snapshot")
	}
	if got := snapshotRowString(snapshot.Screen.Cells[0]); !strings.Contains(got, "hi!") {
		t.Fatalf("expected snapshot row to contain incremental output, got %q", got)
	}
}

func TestRuntimeUpdateHandlerWithoutStreamsOrEventsHasNoInitCommand(t *testing.T) {
	handler := NewRuntimeUpdateHandler(RuntimeSessions{}, NewRuntimeTerminalStore(RuntimeSessions{}), nil)
	defer handler.Stop()

	if cmd := handler.InitCmd(); cmd != nil {
		t.Fatalf("expected nil init cmd without runtime sources, got %v", cmd)
	}
}

func TestRuntimeUpdateHandlerConsumesStreamFrameAndSchedulesNextListen(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 4, Rows: 1},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})
	stream := make(chan protocol.StreamFrame, 1)
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("!")}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Stream:     stream,
			},
		},
	}, store, nil)
	defer handler.Stop()

	initCmd := handler.InitCmd()
	if initCmd == nil {
		t.Fatal("expected init cmd")
	}
	msg := initCmd()
	handled, nextCmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled {
		t.Fatal("expected runtime message to be handled")
	}
	if nextCmd == nil {
		t.Fatal("expected next listen command")
	}
	snapshot, _ := store.Snapshot(types.TerminalID("term-1"))
	if got := snapshotRowString(snapshot.Screen.Cells[0]); !strings.Contains(got, "ok!") {
		t.Fatalf("expected store to apply stream output, got %q", got)
	}
}

func TestRuntimeUpdateHandlerTurnsReadErrorEventIntoNotice(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalReadError,
		TerminalID: "term-1",
		ReadError:  &protocol.TerminalReadErrorData{Error: "pty read failed"},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled {
		t.Fatal("expected event message to be handled")
	}
	if cmd == nil {
		t.Fatal("expected feedback command")
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Notices) != 1 || !strings.Contains(feedback.Notices[0].Text, "pty read failed") {
		t.Fatalf("unexpected feedback notices: %+v", feedback.Notices)
	}
}

func TestRuntimeUpdateHandlerRefreshesSnapshotAfterSyncLost(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 4, Rows: 1},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "l"}, {Content: "d"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
				},
			},
		},
	})
	stream := make(chan protocol.StreamFrame, 1)
	stream <- protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(32)}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Stream:     stream,
			},
		},
	}, store, stubRuntimeSnapshotClient{
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 6, Rows: 1},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "n"}, {Content: "e"}, {Content: "w"}}},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
			},
		},
	})
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected sync lost to be handled with recovery cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one snapshot refresh msg, got %#v", msgs)
	}
	handled, _ = handler.HandleMessage(newAppStateForRuntimeUpdate(), msgs[0])
	if !handled {
		t.Fatal("expected refresh msg to be handled")
	}
	snapshot, _ := store.Snapshot(types.TerminalID("term-1"))
	if got := snapshotRowString(snapshot.Screen.Cells[0]); !strings.Contains(got, "new") {
		t.Fatalf("expected refreshed snapshot after sync lost, got %q", got)
	}
	status, _ := store.Status(types.TerminalID("term-1"))
	if status.SyncLost {
		t.Fatalf("expected sync lost flag to clear after refresh, got %+v", status)
	}
}

func TestRuntimeUpdateHandlerWindowResizeResizesActiveTerminalAndUpdatesStore(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 80, Rows: 24},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})
	client := &stubRuntimeSnapshotClient{}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{}, store, client)
	defer handler.Stop()

	handled, cmd := handler.HandleMessage(connectedRunAppState(), tea.WindowSizeMsg{Width: 120, Height: 40})
	if !handled {
		t.Fatal("expected window resize to be handled")
	}
	if cmd == nil {
		t.Fatal("expected window resize command")
	}
	if msgs := runCmdMessages(cmd); len(msgs) != 0 {
		t.Fatalf("expected successful resize to return no feedback msg, got %#v", msgs)
	}
	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected one resize call, got %d", len(client.resizeCalls))
	}
	if client.resizeCalls[0].channel != 7 || client.resizeCalls[0].cols != 120 || client.resizeCalls[0].rows != 40 {
		t.Fatalf("unexpected resize call payload: %+v", client.resizeCalls[0])
	}
	status, ok := store.Status(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected runtime status after resize")
	}
	if status.Size.Cols != 120 || status.Size.Rows != 40 {
		t.Fatalf("expected store size updated after resize, got %+v", status.Size)
	}
}

func TestRuntimeUpdateHandlerWindowResizeFailureReturnsNotice(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 80, Rows: 24},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})
	client := &stubRuntimeSnapshotClient{resizeErr: errors.New("resize failed")}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{}, store, client)
	defer handler.Stop()

	handled, cmd := handler.HandleMessage(connectedRunAppState(), tea.WindowSizeMsg{Width: 120, Height: 40})
	if !handled {
		t.Fatal("expected window resize to be handled")
	}
	if cmd == nil {
		t.Fatal("expected resize failure command")
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Notices) != 1 || !strings.Contains(feedback.Notices[0].Text, "resize failed") {
		t.Fatalf("unexpected resize feedback notices: %+v", feedback.Notices)
	}
	status, ok := store.Status(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected runtime status after failed resize")
	}
	if status.Size.Cols != 80 || status.Size.Rows != 24 {
		t.Fatalf("expected failed resize to keep previous size, got %+v", status.Size)
	}
}

func TestRuntimeUpdateHandlerWindowResizeFollowerDoesNotResizeSharedTerminal(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    7,
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 80, Rows: 24},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})
	client := &stubRuntimeSnapshotClient{}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{}, store, client)
	defer handler.Stop()

	handled, cmd := handler.HandleMessage(runtimeStateWithFollowerActivePane(), tea.WindowSizeMsg{Width: 120, Height: 40})
	if !handled {
		t.Fatal("expected window resize to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected follower resize to be ignored without command, got %v", cmd)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected follower pane to skip resize call, got %+v", client.resizeCalls)
	}
	status, ok := store.Status(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected runtime status after ignored resize")
	}
	if status.Size.Cols != 80 || status.Size.Rows != 24 {
		t.Fatalf("expected ignored resize to keep previous size, got %+v", status.Size)
	}
}

func TestRuntimeUpdateHandlerTypeClosedFeedsProgramExitedIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {TerminalID: types.TerminalID("term-1")},
		},
	})
	stream := make(chan protocol.StreamFrame, 1)
	stream <- protocol.StreamFrame{Type: protocol.TypeClosed, Payload: protocol.EncodeClosedPayload(9)}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Stream:     stream,
			},
		},
	}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected closed frame to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one exit intent, got %+v", feedback.Intents)
	}
	exitIntent, ok := feedback.Intents[0].(intent.TerminalProgramExitedIntent)
	if !ok {
		t.Fatalf("expected TerminalProgramExitedIntent, got %T", feedback.Intents[0])
	}
	if exitIntent.TerminalID != types.TerminalID("term-1") || exitIntent.ExitCode != 9 {
		t.Fatalf("unexpected exit intent payload: %+v", exitIntent)
	}
}

func TestRuntimeUpdateHandlerRemovedEventFeedsTerminalRemovedIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-1",
		Removed:    &protocol.TerminalRemovedData{Reason: "server_shutdown"},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected removed event to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one removed intent, got %+v", feedback.Intents)
	}
	removedIntent, ok := feedback.Intents[0].(intent.TerminalRemovedIntent)
	if !ok {
		t.Fatalf("expected TerminalRemovedIntent, got %T", feedback.Intents[0])
	}
	if removedIntent.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("unexpected removed intent payload: %+v", removedIntent)
	}
}

func TestRuntimeUpdateHandlerCreatedEventFeedsRegisterTerminalIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalCreated,
		TerminalID: "term-2",
		Created: &protocol.TerminalCreatedData{
			Name:    "build-log",
			Command: []string{"tail", "-f", "build.log"},
			Size:    protocol.Size{Cols: 120, Rows: 40},
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected created event to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one register terminal intent, got %+v", feedback.Intents)
	}
	registerIntent, ok := feedback.Intents[0].(intent.RegisterTerminalIntent)
	if !ok {
		t.Fatalf("expected RegisterTerminalIntent, got %T", feedback.Intents[0])
	}
	if registerIntent.TerminalID != types.TerminalID("term-2") || registerIntent.Name != "build-log" {
		t.Fatalf("unexpected register terminal payload: %+v", registerIntent)
	}
	if registerIntent.State != types.TerminalRunStateRunning {
		t.Fatalf("expected created terminal to default running, got %+v", registerIntent)
	}
	if len(registerIntent.Command) != 3 || registerIntent.Command[0] != "tail" {
		t.Fatalf("expected created command to pass through, got %+v", registerIntent.Command)
	}
}

func TestRuntimeUpdateHandlerStateChangedExitedFeedsProgramExitedIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	exitCode := 13
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalStateChanged,
		TerminalID: "term-1",
		StateChanged: &protocol.TerminalStateChangedData{
			OldState: "running",
			NewState: "exited",
			ExitCode: &exitCode,
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected state changed event to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one exit intent, got %+v", feedback.Intents)
	}
	exitIntent, ok := feedback.Intents[0].(intent.TerminalProgramExitedIntent)
	if !ok {
		t.Fatalf("expected TerminalProgramExitedIntent, got %T", feedback.Intents[0])
	}
	if exitIntent.TerminalID != types.TerminalID("term-1") || exitIntent.ExitCode != 13 {
		t.Fatalf("unexpected exit intent payload: %+v", exitIntent)
	}
}

func TestRuntimeUpdateHandlerStateChangedStoppedFeedsSyncStateIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalStateChanged,
		TerminalID: "term-1",
		StateChanged: &protocol.TerminalStateChangedData{
			OldState: "running",
			NewState: "stopped",
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected stopped state change to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one sync state intent, got %+v", feedback.Intents)
	}
	syncIntent, ok := feedback.Intents[0].(intent.SyncTerminalStateIntent)
	if !ok {
		t.Fatalf("expected SyncTerminalStateIntent, got %T", feedback.Intents[0])
	}
	if syncIntent.TerminalID != types.TerminalID("term-1") || syncIntent.State != types.TerminalRunStateStopped || syncIntent.ExitCode != nil {
		t.Fatalf("unexpected sync state payload: %+v", syncIntent)
	}
}

func TestRuntimeUpdateHandlerStateChangedRunningFeedsSyncStateIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalStateChanged,
		TerminalID: "term-1",
		StateChanged: &protocol.TerminalStateChangedData{
			OldState: "stopped",
			NewState: "running",
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected running state change to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one sync state intent, got %+v", feedback.Intents)
	}
	syncIntent, ok := feedback.Intents[0].(intent.SyncTerminalStateIntent)
	if !ok {
		t.Fatalf("expected SyncTerminalStateIntent, got %T", feedback.Intents[0])
	}
	if syncIntent.TerminalID != types.TerminalID("term-1") || syncIntent.State != types.TerminalRunStateRunning || syncIntent.ExitCode != nil {
		t.Fatalf("unexpected sync state payload: %+v", syncIntent)
	}
}

func TestRuntimeUpdateHandlerStateChangedExitedWithoutCodeFeedsSyncStateIntent(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalStateChanged,
		TerminalID: "term-1",
		StateChanged: &protocol.TerminalStateChangedData{
			OldState: "running",
			NewState: "exited",
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected exited-without-code state change to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Intents) != 1 {
		t.Fatalf("expected one sync state intent, got %+v", feedback.Intents)
	}
	syncIntent, ok := feedback.Intents[0].(intent.SyncTerminalStateIntent)
	if !ok {
		t.Fatalf("expected SyncTerminalStateIntent, got %T", feedback.Intents[0])
	}
	if syncIntent.TerminalID != types.TerminalID("term-1") || syncIntent.State != types.TerminalRunStateExited || syncIntent.ExitCode != nil {
		t.Fatalf("unexpected sync state payload: %+v", syncIntent)
	}
}

func TestRuntimeUpdateHandlerResizedEventUpdatesStoreSnapshotSize(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Snapshot: &protocol.Snapshot{
					TerminalID: "term-1",
					Size:       protocol.Size{Cols: 80, Rows: 24},
					Screen: protocol.ScreenData{
						Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
					},
					Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
				},
			},
		},
	})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
		Resized: &protocol.TerminalResizedData{
			OldSize: protocol.Size{Cols: 80, Rows: 24},
			NewSize: protocol.Size{Cols: 120, Rows: 40},
		},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled {
		t.Fatal("expected resized event to be handled")
	}
	if cmd == nil {
		t.Fatal("expected next listen cmd after resized event")
	}
	snapshot, ok := store.Snapshot(types.TerminalID("term-1"))
	if !ok || snapshot == nil {
		t.Fatal("expected resized snapshot to remain available")
	}
	if snapshot.Size.Cols != 120 || snapshot.Size.Rows != 40 {
		t.Fatalf("expected resized snapshot size 120x40, got %+v", snapshot.Size)
	}
	status, ok := store.Status(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected runtime status after resize")
	}
	if status.Size.Cols != 120 || status.Size.Rows != 40 {
		t.Fatalf("expected runtime status size 120x40, got %+v", status.Size)
	}
}

func TestRuntimeUpdateHandlerCollaboratorsRevokedMarksObserverOnlyAndNotice(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {TerminalID: types.TerminalID("term-1")},
		},
	})
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{
		Type:                 protocol.EventCollaboratorsRevoked,
		TerminalID:           "term-1",
		CollaboratorsRevoked: &protocol.CollaboratorsRevokedData{},
	}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{EventStream: events}, store, nil)
	defer handler.Stop()

	msg := handler.InitCmd()()
	handled, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	if !handled || cmd == nil {
		t.Fatalf("expected revoked event to be handled with feedback cmd, handled=%v cmd=%v", handled, cmd)
	}
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msgs[0])
	}
	if len(feedback.Notices) != 1 || !strings.Contains(feedback.Notices[0].Text, "observer") {
		t.Fatalf("expected revoke notice, got %+v", feedback.Notices)
	}
	status, ok := store.Status(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected runtime status after revoke")
	}
	if !status.ObserverOnly {
		t.Fatalf("expected observer-only status after revoke, got %+v", status)
	}
}

func TestRuntimeUpdateHandlerReportsRefreshFailureAsNotice(t *testing.T) {
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {TerminalID: types.TerminalID("term-1")},
		},
	})
	stream := make(chan protocol.StreamFrame, 1)
	stream <- protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(32)}
	handler := NewRuntimeUpdateHandler(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Stream:     stream,
			},
		},
	}, store, stubRuntimeSnapshotClient{err: errors.New("snapshot boom")})
	defer handler.Stop()

	msg := handler.InitCmd()()
	_, cmd := handler.HandleMessage(newAppStateForRuntimeUpdate(), msg)
	msgs := runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one snapshot refresh msg, got %#v", msgs)
	}
	_, cmd = handler.HandleMessage(newAppStateForRuntimeUpdate(), msgs[0])
	msgs = runCmdMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one refresh failure feedback msg, got %#v", msgs)
	}
	feedback, ok := msgs[0].(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected refresh failure feedback, got %#v", msgs[0])
	}
	if len(feedback.Notices) == 0 || !strings.Contains(feedback.Notices[0].Text, "snapshot boom") {
		t.Fatalf("unexpected failure notices: %+v", feedback.Notices)
	}
}

func snapshotRowString(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func newAppStateForRuntimeUpdate() types.AppState {
	return types.AppState{}
}

type stubRuntimeSnapshotClient struct {
	snapshots   map[string]*protocol.Snapshot
	err         error
	resizeCalls []runtimeResizeCall
	resizeErr   error
}

func (c stubRuntimeSnapshotClient) Snapshot(_ context.Context, terminalID string, _, _ int) (*protocol.Snapshot, error) {
	if c.err != nil {
		return nil, c.err
	}
	return cloneSnapshot(c.snapshots[terminalID]), nil
}

func (c *stubRuntimeSnapshotClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	c.resizeCalls = append(c.resizeCalls, runtimeResizeCall{
		channel: channel,
		cols:    cols,
		rows:    rows,
	})
	if c.resizeErr != nil {
		return c.resizeErr
	}
	return nil
}

func runCmdMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msgValue := msg.(type) {
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, next := range msgValue {
			msgs = append(msgs, runCmdMessagesWithTimeout(next, 20*time.Millisecond)...)
		}
		return msgs
	case nil:
		return nil
	default:
		return []tea.Msg{msgValue}
	}
}

func runCmdMessagesWithTimeout(cmd tea.Cmd, timeout time.Duration) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msgCh := make(chan tea.Msg, 1)
	go func() {
		msgCh <- cmd()
	}()
	select {
	case msg := <-msgCh:
		switch msgValue := msg.(type) {
		case tea.BatchMsg:
			var msgs []tea.Msg
			for _, next := range msgValue {
				msgs = append(msgs, runCmdMessagesWithTimeout(next, timeout)...)
			}
			return msgs
		case nil:
			return nil
		default:
			return []tea.Msg{msgValue}
		}
	case <-time.After(timeout):
		return nil
	}
}
