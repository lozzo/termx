package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
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
	snapshots map[string]*protocol.Snapshot
	err       error
}

func (c stubRuntimeSnapshotClient) Snapshot(_ context.Context, terminalID string, _, _ int) (*protocol.Snapshot, error) {
	if c.err != nil {
		return nil, c.err
	}
	return cloneSnapshot(c.snapshots[terminalID]), nil
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
