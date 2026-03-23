package tui

import (
	"context"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeSnapshotClient interface {
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
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

func (h *runtimeUpdateHandler) HandleMessage(_ types.AppState, msg tea.Msg) (bool, tea.Cmd) {
	switch msgValue := msg.(type) {
	case runtimeStreamFrameMsg:
		return true, h.handleStreamFrame(msgValue)
	case runtimeProtocolEventMsg:
		return true, h.handleProtocolEvent(msgValue.Event)
	case runtimeSnapshotRefreshedMsg:
		return true, h.handleSnapshotRefreshed(msgValue)
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
		return nextCmd
	default:
		return nextCmd
	}
}

func (h *runtimeUpdateHandler) handleProtocolEvent(evt protocol.Event) tea.Cmd {
	notices := h.store.ApplyEvent(evt)
	nextCmd := h.nextCmd()
	if len(notices) == 0 {
		return nextCmd
	}
	feedback := btui.ExecutionResult{
		Notices: make([]btui.Notice, 0, len(notices)),
	}
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
