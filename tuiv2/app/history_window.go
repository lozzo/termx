package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/historyview"
)

var (
	errAuthoritativeHistorySourceUnavailable = errors.New("authoritative history source unavailable")
	errAuthoritativeHistoryStoreUnavailable  = errors.New("authoritative history store unavailable")
)

type historyWindowLoadedMsg struct {
	TerminalID   string
	RequestToken historyview.WindowToken
	Window       historyview.HistoryWindow
	Err          error
}

func (m *Model) loadLatestHistoryWindowCmd(terminalID string, limit, cols int) tea.Cmd {
	terminalID = strings.TrimSpace(terminalID)
	if m == nil || terminalID == "" {
		return nil
	}
	source := m.historySource
	if source == nil {
		return func() tea.Msg {
			return historyWindowLoadedMsg{TerminalID: terminalID, Err: errAuthoritativeHistorySourceUnavailable}
		}
	}
	request := historyview.WindowRequest{
		TerminalID: terminalID,
		Limit:      limit,
		Cols:       cols,
	}
	return func() tea.Msg {
		window, err := source.LatestHistoryWindow(context.Background(), request)
		return historyWindowLoadedMsg{TerminalID: terminalID, Window: window, Err: err}
	}
}

func (m *Model) loadOlderHistoryWindowCmd(terminalID string, limit, cols int) tea.Cmd {
	terminalID = strings.TrimSpace(terminalID)
	if m == nil || terminalID == "" {
		return nil
	}
	if m.historyStore == nil {
		return func() tea.Msg {
			return historyWindowLoadedMsg{TerminalID: terminalID, Err: errAuthoritativeHistoryStoreUnavailable}
		}
	}
	current, ok := m.historyStore.HistoryWindow(terminalID)
	if !ok || !current.HasMore || current.Token == "" || current.BeforeCursor <= 0 {
		return nil
	}
	source := m.historySource
	if source == nil {
		return func() tea.Msg {
			return historyWindowLoadedMsg{TerminalID: terminalID, Err: errAuthoritativeHistorySourceUnavailable}
		}
	}
	request := historyview.WindowRequest{
		TerminalID:   terminalID,
		Token:        current.Token,
		BeforeCursor: current.BeforeCursor,
		Limit:        limit,
		Cols:         cols,
	}
	m.historyStore.SetPendingRequest(terminalID, current.Token)
	return func() tea.Msg {
		window, err := source.OlderHistoryWindow(context.Background(), request)
		return historyWindowLoadedMsg{
			TerminalID:   terminalID,
			RequestToken: current.Token,
			Window:       window,
			Err:          err,
		}
	}
}

func (m *Model) applyHistoryWindowLoadedMsg(msg historyWindowLoadedMsg) tea.Cmd {
	if m == nil {
		return nil
	}
	terminalID := strings.TrimSpace(msg.TerminalID)
	if terminalID == "" {
		terminalID = strings.TrimSpace(msg.Window.TerminalID)
	}
	if msg.RequestToken != "" && m.historyStore != nil {
		defer m.historyStore.ClearPendingRequest(terminalID, msg.RequestToken)
	}
	if msg.Err != nil {
		return m.showError(msg.Err)
	}
	if m.historyStore == nil {
		return m.showError(errAuthoritativeHistoryStoreUnavailable)
	}
	if !m.historyStore.ApplyHistoryWindow(msg.Window) {
		return nil
	}
	if msg.Window.Op == historyview.WindowOpReplace {
		m.historyStore.ClearPendingRequest(msg.Window.TerminalID, "")
	}
	if m.render != nil {
		m.render.Invalidate()
	}
	return nil
}
