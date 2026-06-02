package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type copyModePoint struct {
	Row int
	Col int
}

type copyModeLogicalPos struct {
	Line   int
	Offset int
}

type copyModeState struct {
	PaneID         string
	TerminalID     string
	WindowToken    string
	ViewTopRow     int
	Cursor         copyModePoint
	CursorLogical  copyModeLogicalPos
	Mark           *copyModePoint
	MarkLogical    *copyModeLogicalPos
	MouseSelecting bool
	AutoScrollDir  int
	AutoScrollSeq  uint64
}

func (m *Model) activePaneInCopyMode() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		return false
	}
	_, ok := m.copyModeStateForPane(pane.ID)
	return ok
}

func clearNoticeCmd(seq uint64) tea.Cmd {
	return tea.Tick(noticeClearDelay, func(time.Time) tea.Msg {
		return clearNoticeMsg{seq: seq}
	})
}

func copyModeAutoScrollTickCmd(seq uint64) tea.Cmd {
	return tea.Tick(copyModeAutoScrollDelay, func(time.Time) tea.Msg {
		return copyModeAutoScrollMsg{seq: seq}
	})
}

func cloneProtocolRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = append([]protocol.Cell(nil), row...)
	}
	return out
}

func cloneSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cloneProtocolRows(snapshot.Screen.Cells),
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	cloned.ScreenWrapped = append([]bool(nil), snapshot.ScreenWrapped...)
	cloned.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped...)
	cloned.ScreenOwnership = append([]string(nil), snapshot.ScreenOwnership...)
	cloned.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership...)
	return &cloned
}

func copyModePinnedAtTop(state copyModeState) bool {
	return state.CursorLogical.Line == 0 && state.ViewTopRow == 0
}

func (m *Model) showNotice(text string) tea.Cmd {
	if m == nil {
		return nil
	}
	m.noticeSeq++
	m.notice = strings.TrimSpace(text)
	m.render.Invalidate()
	if m.notice == "" {
		return nil
	}
	return clearNoticeCmd(m.noticeSeq)
}

func (m *Model) copyModeStateForPane(paneID string) (copyModeState, bool) {
	if m == nil || paneID == "" {
		return copyModeState{}, false
	}
	if m.copyMode.PaneID == paneID {
		return m.copyMode, true
	}
	if m.copyModes == nil {
		return copyModeState{}, false
	}
	state, ok := m.copyModes[paneID]
	if !ok || state.PaneID == "" {
		return copyModeState{}, false
	}
	return state, true
}

func (m *Model) saveCopyModeState(state copyModeState) {
	if m == nil || state.PaneID == "" {
		return
	}
	if m.copyModes == nil {
		m.copyModes = make(map[string]copyModeState)
	}
	m.copyModes[state.PaneID] = state
	if pane := m.workbenchActivePane(); pane != nil && pane.ID == state.PaneID {
		m.copyMode = state
		return
	}
	if m.copyMode.PaneID == state.PaneID {
		m.copyMode = state
	}
}

func (m *Model) saveCurrentCopyModeState() {
	if m == nil || m.copyMode.PaneID == "" {
		return
	}
	m.saveCopyModeState(m.copyMode)
}

func (m *Model) loadCopyModeStateForPane(paneID string) bool {
	state, ok := m.copyModeStateForPane(paneID)
	if !ok {
		return false
	}
	m.copyMode = state
	return true
}

func (m *Model) loadActiveCopyModeState() bool {
	pane := m.workbenchActivePane()
	if pane == nil {
		return false
	}
	return m.loadCopyModeStateForPane(pane.ID)
}

func (m *Model) deleteCopyModeStateForPane(paneID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.copyModes != nil {
		delete(m.copyModes, paneID)
	}
	if m.copyMode.PaneID == paneID {
		m.copyMode = copyModeState{}
		if pane := m.workbenchActivePane(); pane != nil && pane.ID != paneID {
			_ = m.loadCopyModeStateForPane(pane.ID)
		}
	}
}

func (m *Model) workbenchActivePane() *workbench.PaneState {
	if m == nil || m.workbench == nil {
		return nil
	}
	return m.workbench.ActivePane()
}

func (m *Model) allCopyModeStates() []copyModeState {
	if m == nil {
		return nil
	}
	statesByPane := make(map[string]copyModeState)
	if m.copyModes != nil {
		for paneID, state := range m.copyModes {
			if paneID != "" && state.PaneID != "" {
				statesByPane[paneID] = state
			}
		}
	}
	if m.copyMode.PaneID != "" {
		statesByPane[m.copyMode.PaneID] = m.copyMode
	}
	if len(statesByPane) == 0 {
		return nil
	}
	paneIDs := make([]string, 0, len(statesByPane))
	for paneID := range statesByPane {
		paneIDs = append(paneIDs, paneID)
	}
	sort.Strings(paneIDs)
	out := make([]copyModeState, 0, len(paneIDs))
	for _, paneID := range paneIDs {
		out = append(out, statesByPane[paneID])
	}
	return out
}

func (m *Model) clearCopySelection() {
	if m == nil {
		return
	}
	m.copyMode.Mark = nil
	m.stopMouseCopySelection()
	m.saveCurrentCopyModeState()
}

func (m *Model) reconcileCopyModeContext() {
	if m == nil || m.workbench == nil {
		return
	}
	changed := false
	for _, state := range m.allCopyModeStates() {
		tabID, err := m.workbench.ResolvePaneTab("", state.PaneID)
		if err == nil && tabID != "" {
			continue
		}
		m.resetPaneViewport(state.PaneID)
		m.deleteCopyModeStateForPane(state.PaneID)
		changed = true
	}
	if m.copyMode.PaneID == "" {
		// No active copy-mode pane remains.
	}
	if changed && m.mode().Kind == input.ModeDisplay && !m.activePaneInCopyMode() {
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	}
	if changed {
		m.render.Invalidate()
	}
}

func (m *Model) leaveCopyMode() {
	if m == nil {
		return
	}
	if m.copyMode.PaneID == "" {
		_ = m.loadActiveCopyModeState()
	}
	paneID := m.copyMode.PaneID
	if active := m.workbenchActivePane(); active != nil {
		if state, ok := m.copyModeStateForPane(active.ID); ok {
			m.copyMode = state
			paneID = state.PaneID
		}
	}
	m.resetPaneViewport(paneID)
	m.deleteCopyModeStateForPane(paneID)
	m.render.Invalidate()
}

func (m *Model) ensureCopyMode() bool {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return false
	}
	pane := m.workbench.ActivePane()
	tab := m.workbench.CurrentTab()
	if pane == nil || tab == nil || pane.ID == "" || pane.TerminalID == "" {
		return false
	}
	if m.copyMode.PaneID != "" && m.copyMode.PaneID != pane.ID {
		m.saveCurrentCopyModeState()
	}
	_ = m.loadCopyModeStateForPane(pane.ID)
	if m.copyMode.PaneID == pane.ID {
		buffer, ok := m.activeCopyModeBuffer()
		if !ok || buffer.totalRows() == 0 {
			return false
		}
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if logical, ok := buffer.logicalPosForPoint(m.copyMode.Cursor); ok {
			m.copyMode.CursorLogical = logical
		}
		if m.copyMode.Mark != nil {
			point := buffer.clampPoint(*m.copyMode.Mark)
			m.copyMode.Mark = &point
			if logical, ok := buffer.logicalPosForPoint(point); ok {
				m.copyMode.MarkLogical = &logical
			}
		}
		m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
		m.saveCurrentCopyModeState()
		return true
	}
	m.copyMode = copyModeState{
		PaneID:     pane.ID,
		TerminalID: pane.TerminalID,
	}
	m.saveCurrentCopyModeState()
	return true
}

func (m *Model) enterCopyModeForPaneCmd(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	if strings.TrimSpace(paneID) != "" {
		if tabID, err := m.workbench.ResolvePaneTab("", paneID); err == nil && tabID != "" {
			tab := m.workbench.CurrentTab()
			if tab != nil && tab.ID == tabID && tab.ActivePaneID != paneID {
				_ = m.workbench.FocusPane(tabID, paneID)
			}
		}
	}
	m.setMode(input.ModeState{Kind: input.ModeDisplay})
	if !m.ensureCopyMode() {
		m.render.Invalidate()
		return nil
	}
	if m.render != nil {
		m.render.Invalidate()
	}
	return m.loadLatestHistoryWindowForPaneCmd(m.copyMode.PaneID)
}

func (m *Model) pasteBufferToActiveCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if m.yankBuffer == "" {
		return m.showError(fmt.Errorf("copy buffer is empty"))
	}
	return m.pasteTextToActiveCmd(m.yankBuffer)
}

func (m *Model) pasteClipboardToActiveCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if systemClipboardReader == nil {
		return m.showError(fmt.Errorf("system clipboard unavailable"))
	}
	text, err := systemClipboardReader()
	if err != nil {
		return m.showError(err)
	}
	if text == "" {
		return m.showError(fmt.Errorf("system clipboard is empty"))
	}
	return m.pasteTextToActiveCmd(text)
}

func (m *Model) pasteTextToActiveCmd(text string) tea.Cmd {
	if m == nil || text == "" {
		return nil
	}
	paneID := ""
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			paneID = pane.ID
		}
	}
	if paneID == "" {
		return m.showError(fmt.Errorf("no active pane"))
	}
	return m.handleTerminalInput(input.TerminalInput{
		Kind:   input.TerminalInputPaste,
		PaneID: paneID,
		Text:   text,
	})
}
