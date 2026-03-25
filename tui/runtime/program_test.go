package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
)

type intentExecutorFunc func(context.Context, app.Model, app.Intent) (app.Model, tea.Cmd, error)

func (f intentExecutorFunc) ExecuteIntent(ctx context.Context, model app.Model, intent app.Intent) (app.Model, tea.Cmd, error) {
	return f(ctx, model, intent)
}

type quitModel struct{}

func (quitModel) Init() tea.Cmd {
	return tea.Quit
}

func (m quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (quitModel) View() string {
	return ""
}

func TestProgramRunnerRunsTeaModel(t *testing.T) {
	runner := NewProgramRunner()
	if err := runner.Run(quitModel{}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestProgramRunnerFlushesWorkspaceSaveOnExit(t *testing.T) {
	runner := NewProgramRunner()
	saver := &recordingWorkspaceSaveScheduler{}
	model := WrapModelWithWorkspacePersistence(quitAfterInitModel{model: sampleWorkbenchStateForRuntimeTest()}, NewUpdateLoop(nil, saver))

	if err := runner.Run(model, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(saver.flushed) != 1 {
		t.Fatalf("expected one flush on exit, got %d", len(saver.flushed))
	}
	if saver.flushed[0].Workspace == nil || saver.flushed[0].Workspace.ID != "restored" {
		t.Fatalf("expected flushed workspace to preserve restored state, got %#v", saver.flushed[0].Workspace)
	}
}

func TestRuntimeWrapperTranslatesDaemonEventsIntoUIState(t *testing.T) {
	t.Run("remote remove notice", func(t *testing.T) {
		model := livePaneModelForRuntimeTest()
		wrapped := WrapModelWithWorkspacePersistence(model, NewUpdateLoop(nil))

		next, _ := wrapped.Update(UpdateMessage{Event: protocol.Event{
			Type:       protocol.EventTerminalRemoved,
			TerminalID: "term-1",
		}})
		updated := next.(interface{ AppModel() app.Model }).AppModel()
		if updated.Notice == nil || !strings.Contains(updated.Notice.Message, "removed") {
			t.Fatalf("expected remove notice, got %#v", updated.Notice)
		}
	})

	t.Run("state change exits pane", func(t *testing.T) {
		model := livePaneModelForRuntimeTest()
		wrapped := WrapModelWithWorkspacePersistence(model, NewUpdateLoop(nil))

		next, _ := wrapped.Update(UpdateMessage{Event: protocol.Event{
			Type:       protocol.EventTerminalStateChanged,
			TerminalID: "term-1",
			StateChanged: &protocol.TerminalStateChangedData{
				OldState: "running",
				NewState: "exited",
			},
		}})
		updated := next.(interface{ AppModel() app.Model }).AppModel()
		pane, _ := updated.Workspace.ActiveTab().ActivePane()
		if updated.Terminals[types.TerminalID("term-1")].State != terminal.StateExited || pane.SlotState != types.PaneSlotExited {
			t.Fatalf("expected exited state to reach ui, got meta=%#v pane=%+v", updated.Terminals[types.TerminalID("term-1")], pane)
		}
	})

	t.Run("revoke blocks local input", func(t *testing.T) {
		model := livePaneModelForRuntimeTest()
		wrapped := WrapModelWithWorkspacePersistence(model, NewUpdateLoop(nil))

		next, _ := wrapped.Update(UpdateMessage{Event: protocol.Event{
			Type:       protocol.EventCollaboratorsRevoked,
			TerminalID: "term-1",
		}})
		updated := next.(interface{ AppModel() app.Model }).AppModel()
		service := &stubTerminalService{}
		router := NewInputRouter(service)
		if err := router.HandleKey(context.Background(), updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); err != nil {
			t.Fatalf("HandleKey returned error: %v", err)
		}
		if len(service.lastInputData) != 0 {
			t.Fatalf("expected revoked collaborator to block input, got %q", string(service.lastInputData))
		}
	})
}

func TestRuntimeWrapperInitStartsRestoredPreviewStreamImmediately(t *testing.T) {
	client := &stubClient{
		attachResult: &protocol.AttachResult{Channel: 21, Mode: "observer"},
		snapshotByID: map[string][]*protocol.Snapshot{
			"term-2": {sampleSnapshotForRuntimeTest("term-2", "restored preview")},
		},
		streams: map[uint16]chan protocol.StreamFrame{
			21: make(chan protocol.StreamFrame, 2),
		},
	}
	model := sampleWorkbenchStateForRuntimeTest()
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool.PreviewTerminalID = types.TerminalID("term-2")
	model.Pool.SelectedTerminalID = types.TerminalID("term-2")
	model.Pool.PreviewReadonly = true
	model.Pool.PreviewSubscriptionRevision = 7
	meta := model.Terminals[types.TerminalID("term-1")]
	meta.State = terminal.StateExited
	model.Terminals[types.TerminalID("term-1")] = meta

	restored := RebindRestoredModel(context.Background(), client, model)
	wrapped := WrapModelWithWorkspacePersistence(restored, NewUpdateLoop(nil))
	client.streams[21] <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("tick")}

	cmd := wrapped.Init()
	if cmd == nil {
		t.Fatal("expected init to start restored preview stream")
	}
	msg := cmd()
	if _, ok := msg.(app.PreviewStreamMessage); !ok {
		t.Fatalf("expected preview stream message on init, got %T", msg)
	}
}

func TestRuntimeWrapperKeepsOtherStreamsAliveAfterOneStreamCloses(t *testing.T) {
	model := sampleWorkbenchStateForRuntimeTest()
	model.Terminals[types.TerminalID("term-1")] = terminal.Metadata{
		ID:          types.TerminalID("term-1"),
		Name:        "shell",
		Command:     []string{"/bin/sh"},
		State:       terminal.StateRunning,
		OwnerPaneID: types.PaneID("pane-1"),
	}
	model.Sessions[types.TerminalID("term-1")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Attached:   true,
		Snapshot:   sampleSnapshotForRuntimeTest("term-1", "old-live"),
	}

	store := NewSessionStore()
	liveStream := make(chan protocol.StreamFrame, 2)
	previewStream := make(chan protocol.StreamFrame, 1)
	store.BindLive(types.TerminalID("term-1"), 7, sampleSnapshotForRuntimeTest("term-1", "old-live"), liveStream, func() {})
	store.BindPreview(types.TerminalID("term-2"), 21, sampleSnapshotForRuntimeTest("term-2", "old-preview"), previewStream, func() {})
	model.PreviewStreamNext = store.NextStreamMessageCmd

	executor := intentExecutorFunc(func(_ context.Context, current app.Model, intent app.Intent) (app.Model, tea.Cmd, error) {
		next := current.Apply(intent)
		if tick, ok := intent.(app.SessionStreamTickIntent); ok {
			next = next.Apply(app.SessionSnapshotRefreshedIntent{
				TerminalID: tick.TerminalID,
				Snapshot:   sampleSnapshotForRuntimeTest(string(tick.TerminalID), "new-live"),
			})
		}
		if store.HasActiveStreams() {
			next.PreviewStreamNext = store.NextStreamMessageCmd
		} else {
			next.PreviewStreamNext = nil
		}
		return next, next.PreviewStreamNext(), nil
	})
	model.IntentExecutor = executor
	wrapped := WrapModelWithWorkspacePersistence(model, NewUpdateLoop(nil))

	close(previewStream)
	firstCmd := wrapped.Init()
	if firstCmd == nil {
		t.Fatal("expected init cmd")
	}
	firstMsg := firstCmd()
	if _, ok := firstMsg.(app.PreviewStreamClosedMessage); !ok {
		t.Fatalf("expected preview closed message first, got %T", firstMsg)
	}

	nextModel, nextCmd := wrapped.Update(firstMsg)
	if nextCmd == nil {
		t.Fatal("expected remaining live stream to re-arm after preview close")
	}
	liveStream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("tick")}
	secondMsg := nextCmd()
	if _, ok := secondMsg.(app.LiveStreamMessage); !ok {
		t.Fatalf("expected live stream message after preview close, got %T", secondMsg)
	}

	finalModel, _ := nextModel.Update(secondMsg)
	updated := finalModel.(interface{ AppModel() app.Model }).AppModel()
	if got := flattenRuntimeSnapshotText(updated.Sessions[types.TerminalID("term-1")].Snapshot); !strings.Contains(got, "new-live") || strings.Contains(got, "old-live") {
		t.Fatalf("expected live stream to keep refreshing after preview close, got %q", got)
	}
}

type quitAfterInitModel struct {
	model app.Model
}

func (m quitAfterInitModel) Init() tea.Cmd {
	return tea.Quit
}

func (m quitAfterInitModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m quitAfterInitModel) View() string {
	return ""
}

func (m quitAfterInitModel) AppModel() app.Model {
	return m.model
}
