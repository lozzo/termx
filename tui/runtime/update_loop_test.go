package runtime

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
)

func TestUpdateLoopConvertsDaemonEventsIntoMessages(t *testing.T) {
	events := make(chan protocol.Event, 1)
	loop := NewUpdateLoop(events)

	events <- protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-1",
		Timestamp:  time.Unix(1, 0),
	}

	msg, ok := loop.Next()
	if !ok {
		t.Fatal("expected one converted message")
	}
	if msg.Event.TerminalID != "term-1" || msg.Event.Type != protocol.EventTerminalRemoved {
		t.Fatalf("unexpected update message: %#v", msg)
	}
}

func TestUpdateLoopSchedulesDebouncedWorkspaceSaveForWorkspaceMutation(t *testing.T) {
	saver := &recordingWorkspaceSaveScheduler{}
	loop := NewUpdateLoop(nil, saver)
	before := sampleWorkbenchStateForRuntimeTest()
	after := before.Apply(app.IntentSplitVertical)

	if cmd := loop.ObserveModelTransition(before, before); cmd != nil {
		t.Fatalf("expected no save command for identical state, got %#v", cmd)
	}
	if len(saver.scheduled) != 0 {
		t.Fatalf("expected identical state to skip save, got %d schedules", len(saver.scheduled))
	}

	loop.ObserveModelTransition(before, after)
	if len(saver.scheduled) != 1 {
		t.Fatalf("expected one save schedule, got %d", len(saver.scheduled))
	}
	if got := saver.scheduled[0].Workspace.ActiveTab().PaneCount(); got != 3 {
		t.Fatalf("expected scheduled model to keep new pane, got %d panes", got)
	}
}

type recordingWorkspaceSaveScheduler struct {
	scheduled []app.Model
	flushed   []app.Model
}

func (s *recordingWorkspaceSaveScheduler) Schedule(model app.Model) tea.Cmd {
	s.scheduled = append(s.scheduled, model)
	return nil
}

func (s *recordingWorkspaceSaveScheduler) Flush(_ context.Context, model app.Model) error {
	s.flushed = append(s.flushed, model)
	return nil
}
