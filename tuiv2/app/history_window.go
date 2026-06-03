package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var (
	errAuthoritativeHistorySourceUnavailable = errors.New("authoritative history source unavailable")
	errAuthoritativeHistoryStoreUnavailable  = errors.New("authoritative history store unavailable")
)

const defaultAuthoritativeHistoryWindowRows = 500

type historyWindowLoadedMsg struct {
	TerminalID          string
	RequestToken        historyview.WindowToken
	RequestBeforeCursor int
	Window              historyview.HistoryWindow
	Err                 error
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

func (m *Model) loadLatestHistoryWindowForPaneCmd(paneID string) tea.Cmd {
	pane, rect, ok := m.visiblePaneForInput(paneID)
	if !ok || pane == nil || pane.TerminalID == "" {
		return nil
	}
	limit, cols := historyWindowRequestShape(rect)
	return m.loadLatestHistoryWindowCmd(pane.TerminalID, limit, cols)
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
			TerminalID:          terminalID,
			RequestToken:        current.Token,
			RequestBeforeCursor: current.BeforeCursor,
			Window:              window,
			Err:                 err,
		}
	}
}

func (m *Model) loadOlderHistoryWindowForPaneCmd(paneID string) tea.Cmd {
	pane, rect, ok := m.visiblePaneForInput(paneID)
	if !ok || pane == nil || pane.TerminalID == "" {
		return nil
	}
	limit, cols := historyWindowRequestShape(rect)
	return m.loadOlderHistoryWindowCmd(pane.TerminalID, limit, cols)
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
	window := historyWindowForStoreApply(msg.Window, terminalID, msg.RequestToken, msg.RequestBeforeCursor)
	if !m.historyStore.ApplyHistoryWindow(window) {
		return nil
	}
	if window.Op == historyview.WindowOpReplace {
		m.historyStore.ClearPendingRequest(window.TerminalID, "")
	}
	m.syncCopyModeStateAfterHistoryWindow(window)
	if m.render != nil {
		m.render.Invalidate()
	}
	return nil
}

func historyWindowForStoreApply(window historyview.HistoryWindow, terminalID string, requestToken historyview.WindowToken, requestBeforeCursor int) historyview.HistoryWindow {
	if strings.TrimSpace(window.TerminalID) == "" {
		window.TerminalID = terminalID
	}
	if window.Op == historyview.WindowOpPrepend && len(window.Rows) == 0 && len(window.Lines) == 0 && !window.HasMore {
		if window.Token == "" {
			window.Token = requestToken
		}
		if window.BeforeCursor <= 0 {
			window.BeforeCursor = requestBeforeCursor
		}
	}
	return window
}

func (m *Model) syncCopyModeStateAfterHistoryWindow(window historyview.HistoryWindow) {
	if m == nil || strings.TrimSpace(window.TerminalID) == "" || len(window.Rows) == 0 || len(window.Lines) == 0 {
		return
	}
	states := m.allCopyModeStates()
	for _, state := range states {
		if state.TerminalID != window.TerminalID {
			continue
		}
		if state.WindowToken != "" && state.WindowToken != string(window.Token) && window.Op != historyview.WindowOpReplace {
			continue
		}
		state.WindowToken = string(window.Token)
		buffer, ok := m.copyModeBufferForState(state, 1)
		if !ok {
			continue
		}
		if window.Op == historyview.WindowOpReplace || state.CursorLogical.Line >= buffer.logicalLineCount() {
			state.CursorLogical = copyModeLogicalPos{Line: maxInt(0, buffer.logicalLineCount()-1), Offset: 0}
		}
		point, ok := buffer.pointForLogicalPos(state.CursorLogical)
		if !ok {
			point = copyModePoint{Row: maxInt(0, buffer.totalRows()-1), Col: 0}
			state.CursorLogical = copyModeLogicalPos{Line: maxInt(0, buffer.logicalLineCount()-1), Offset: 0}
		}
		state.Cursor = buffer.clampPoint(point)
		if window.Op == historyview.WindowOpReplace {
			state.ViewTopRow = buffer.maxViewTopRow()
		}
		state.Mark = nil
		state.MarkLogical = nil
		m.saveCopyModeState(state)
	}
}

func historyWindowRequestShape(rect workbench.Rect) (limit int, cols int) {
	limit = maxInt(defaultAuthoritativeHistoryWindowRows, maxInt(1, rect.H)*4)
	cols = maxInt(1, rect.W)
	return limit, cols
}
