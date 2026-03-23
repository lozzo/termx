package tui

import (
	"context"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app/intent"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeSnapshotClient interface {
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
}

type runtimeTerminalResizeClient interface {
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
}

type runtimeUpdateHandler struct {
	store          *runtimeTerminalStore
	snapshotClient RuntimeSnapshotClient
	updates        chan tea.Msg
	done           chan struct{}
	wg             sync.WaitGroup
	active         bool
}

type runtimeStreamFrameMsg struct {
	TerminalID types.TerminalID
	Frame      protocol.StreamFrame
}

type runtimeProtocolEventMsg struct {
	Event protocol.Event
}

type runtimeSnapshotRefreshedMsg struct {
	TerminalID types.TerminalID
	Snapshot   *protocol.Snapshot
	Err        error
}

func NewRuntimeUpdateHandler(sessions RuntimeSessions, store *runtimeTerminalStore, snapshotClient RuntimeSnapshotClient) *runtimeUpdateHandler {
	handler := &runtimeUpdateHandler{
		store:          store,
		snapshotClient: snapshotClient,
		updates:        make(chan tea.Msg, 256),
		done:           make(chan struct{}),
	}
	for terminalID, session := range sessions.Terminals {
		if session.Stream == nil {
			continue
		}
		handler.active = true
		handler.wg.Add(1)
		go handler.forwardStream(terminalID, session.Stream)
	}
	if sessions.EventStream != nil {
		handler.active = true
		handler.wg.Add(1)
		go handler.forwardEvents(sessions.EventStream)
	}
	return handler
}

func (h *runtimeUpdateHandler) InitCmd() tea.Cmd {
	if h == nil || !h.active {
		return nil
	}
	return h.nextCmd()
}

func (h *runtimeUpdateHandler) Stop() {
	if h == nil {
		return
	}
	close(h.done)
	h.wg.Wait()
	close(h.updates)
}

func (h *runtimeUpdateHandler) HandleMessage(state types.AppState, msg tea.Msg) (bool, tea.Cmd) {
	switch msgValue := msg.(type) {
	case runtimeStreamFrameMsg:
		return true, h.handleStreamFrame(msgValue)
	case runtimeProtocolEventMsg:
		return true, h.handleProtocolEvent(msgValue.Event)
	case runtimeSnapshotRefreshedMsg:
		return true, h.handleSnapshotRefreshed(msgValue)
	case tea.WindowSizeMsg:
		return true, h.handleWindowSize(state, msgValue)
	default:
		return false, nil
	}
}

func (h *runtimeUpdateHandler) handleStreamFrame(msg runtimeStreamFrameMsg) tea.Cmd {
	nextCmd := h.nextCmd()
	switch msg.Frame.Type {
	case protocol.TypeOutput:
		if err := h.store.WriteOutput(msg.TerminalID, msg.Frame.Payload); err != nil {
			return tea.Batch(nextCmd, btui.FeedbackCmd(btui.ExecutionResult{
				Notices: []btui.Notice{{
					Level: btui.NoticeLevelError,
					Text:  err.Error(),
				}},
			}))
		}
		return nextCmd
	case protocol.TypeSyncLost:
		dropped, err := protocol.DecodeSyncLostPayload(msg.Frame.Payload)
		if err != nil {
			return tea.Batch(nextCmd, btui.FeedbackCmd(btui.ExecutionResult{
				Notices: []btui.Notice{{Level: btui.NoticeLevelError, Text: err.Error()}},
			}))
		}
		h.store.MarkSyncLost(msg.TerminalID, dropped)
		refreshCmd := h.refreshSnapshotCmd(msg.TerminalID)
		return tea.Batch(nextCmd, refreshCmd)
	case protocol.TypeClosed:
		exitCode, err := protocol.DecodeClosedPayload(msg.Frame.Payload)
		if err != nil {
			return tea.Batch(nextCmd, btui.FeedbackCmd(btui.ExecutionResult{
				Notices: []btui.Notice{{Level: btui.NoticeLevelError, Text: err.Error()}},
			}))
		}
		h.store.MarkClosed(msg.TerminalID, exitCode)
		return tea.Batch(nextCmd, btui.FeedbackCmd(btui.ExecutionResult{
			Intents: []intent.Intent{intent.TerminalProgramExitedIntent{
				TerminalID: msg.TerminalID,
				ExitCode:   exitCode,
			}},
		}))
	default:
		return nextCmd
	}
}

func (h *runtimeUpdateHandler) handleProtocolEvent(evt protocol.Event) tea.Cmd {
	notices := h.store.ApplyEvent(evt)
	intents := runtimeEventIntents(evt)
	nextCmd := h.nextCmd()
	if len(notices) == 0 && len(intents) == 0 {
		return nextCmd
	}
	feedback := btui.ExecutionResult{
		Intents: make([]intent.Intent, 0, len(intents)),
		Notices: make([]btui.Notice, 0, len(notices)),
	}
	feedback.Intents = append(feedback.Intents, intents...)
	for _, text := range notices {
		if text == "" {
			continue
		}
		feedback.Notices = append(feedback.Notices, btui.Notice{
			Level: btui.NoticeLevelError,
			Text:  text,
		})
	}
	return tea.Batch(nextCmd, btui.FeedbackCmd(feedback))
}

func (h *runtimeUpdateHandler) handleSnapshotRefreshed(msg runtimeSnapshotRefreshedMsg) tea.Cmd {
	nextCmd := h.nextCmd()
	if msg.Err != nil {
		return tea.Batch(nextCmd, btui.FeedbackCmd(btui.ExecutionResult{
			Notices: []btui.Notice{{
				Level: btui.NoticeLevelError,
				Text:  msg.Err.Error(),
			}},
		}))
	}
	h.store.LoadSnapshot(msg.TerminalID, msg.Snapshot)
	return nextCmd
}

func (h *runtimeUpdateHandler) handleWindowSize(state types.AppState, msg tea.WindowSizeMsg) tea.Cmd {
	session, ok := activeTerminalSession(state, h.store)
	if !ok || session.Channel == 0 {
		return nil
	}
	client, ok := h.snapshotClient.(runtimeTerminalResizeClient)
	if !ok {
		return btui.FeedbackCmd(btui.ExecutionResult{
			Notices: []btui.Notice{{
				Level: btui.NoticeLevelError,
				Text:  fmt.Sprintf("runtime resize client missing for %s", session.TerminalID),
			}},
		})
	}
	size := protocol.Size{
		Cols: uint16(max(1, msg.Width)),
		Rows: uint16(max(1, msg.Height)),
	}
	return func() tea.Msg {
		if err := client.Resize(context.Background(), session.Channel, size.Cols, size.Rows); err != nil {
			return btui.FeedbackMsg{
				Notices: []btui.Notice{{
					Level: btui.NoticeLevelError,
					Text:  err.Error(),
				}},
			}
		}
		h.store.Resize(session.TerminalID, size)
		return nil
	}
}

func (h *runtimeUpdateHandler) refreshSnapshotCmd(terminalID types.TerminalID) tea.Cmd {
	if h.snapshotClient == nil {
		return btui.FeedbackCmd(btui.ExecutionResult{
			Notices: []btui.Notice{{
				Level: btui.NoticeLevelError,
				Text:  fmt.Sprintf("runtime snapshot client missing for %s", terminalID),
			}},
		})
	}
	return func() tea.Msg {
		snapshot, err := h.snapshotClient.Snapshot(context.Background(), string(terminalID), 0, 200)
		return runtimeSnapshotRefreshedMsg{
			TerminalID: terminalID,
			Snapshot:   snapshot,
			Err:        err,
		}
	}
}

func (h *runtimeUpdateHandler) nextCmd() tea.Cmd {
	if h == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-h.done:
			return nil
		case msg, ok := <-h.updates:
			if !ok {
				return nil
			}
			return msg
		}
	}
}

func (h *runtimeUpdateHandler) forwardStream(terminalID types.TerminalID, stream <-chan protocol.StreamFrame) {
	defer h.wg.Done()
	for {
		select {
		case <-h.done:
			return
		case frame, ok := <-stream:
			if !ok {
				return
			}
			h.send(runtimeStreamFrameMsg{TerminalID: terminalID, Frame: frame})
		}
	}
}

func (h *runtimeUpdateHandler) forwardEvents(events <-chan protocol.Event) {
	defer h.wg.Done()
	for {
		select {
		case <-h.done:
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			h.send(runtimeProtocolEventMsg{Event: evt})
		}
	}
}

func (h *runtimeUpdateHandler) send(msg tea.Msg) {
	select {
	case <-h.done:
		return
	case h.updates <- msg:
	}
}

func runtimeEventIntents(evt protocol.Event) []intent.Intent {
	terminalID := types.TerminalID(evt.TerminalID)
	switch evt.Type {
	case protocol.EventTerminalCreated:
		if evt.Created == nil {
			return nil
		}
		return []intent.Intent{intent.RegisterTerminalIntent{
			TerminalID: terminalID,
			Name:       evt.Created.Name,
			Command:    append([]string(nil), evt.Created.Command...),
			State:      types.TerminalRunStateRunning,
		}}
	case protocol.EventTerminalStateChanged:
		if evt.StateChanged == nil {
			return nil
		}
		if evt.StateChanged.NewState == string(types.TerminalRunStateExited) && evt.StateChanged.ExitCode != nil {
			return []intent.Intent{intent.TerminalProgramExitedIntent{
				TerminalID: terminalID,
				ExitCode:   *evt.StateChanged.ExitCode,
			}}
		}
		switch evt.StateChanged.NewState {
		case string(types.TerminalRunStateRunning):
			return []intent.Intent{intent.SyncTerminalStateIntent{
				TerminalID: terminalID,
				State:      types.TerminalRunStateRunning,
			}}
		case string(types.TerminalRunStateStopped):
			return []intent.Intent{intent.SyncTerminalStateIntent{
				TerminalID: terminalID,
				State:      types.TerminalRunStateStopped,
			}}
		case string(types.TerminalRunStateExited):
			return []intent.Intent{intent.SyncTerminalStateIntent{
				TerminalID: terminalID,
				State:      types.TerminalRunStateExited,
			}}
		}
	case protocol.EventTerminalRemoved:
		return []intent.Intent{intent.TerminalRemovedIntent{
			TerminalID: terminalID,
		}}
	}
	return nil
}
