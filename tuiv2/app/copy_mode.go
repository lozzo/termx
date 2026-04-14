package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
)

type copyModePoint struct {
	Row int
	Col int
}

type copyModeState struct {
	PaneID         string
	Snapshot       *protocol.Snapshot
	LoadedRows     int
	ViewTopRow     int
	Cursor         copyModePoint
	Mark           *copyModePoint
	MouseSelecting bool
	AutoScrollDir  int
	AutoScrollSeq  uint64
}

type copyModeResumeState struct {
	PaneID          string
	TerminalID      string
	Snapshot        *protocol.Snapshot
	BaselineVersion uint64
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
	cloned.Scrollback = cloneProtocolRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	return &cloned
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

func (m *Model) resetCopyMode() {
	if m == nil {
		return
	}
	m.copyMode = copyModeState{}
}

func (m *Model) clearCopySelection() {
	if m == nil {
		return
	}
	m.copyMode.Mark = nil
	m.stopMouseCopySelection()
}

func (m *Model) reconcileCopyModeContext() {
	if m == nil || m.workbench == nil || m.copyMode.PaneID == "" {
		return
	}
	pane := m.workbench.ActivePane()
	if pane != nil && pane.ID == m.copyMode.PaneID {
		return
	}
	if tab := m.workbench.CurrentTab(); tab != nil {
		tab.ScrollOffset = 0
	}
	m.clearCopySelection()
	m.resetCopyMode()
	m.copyModeResume = copyModeResumeState{}
	if m.mode().Kind == input.ModeDisplay {
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	}
	m.render.Invalidate()
}

func (m *Model) prepareCopyModeExit() {
	if m == nil || m.copyMode.PaneID == "" || m.copyMode.Snapshot == nil || m.workbench == nil || m.runtime == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyMode.PaneID || pane.TerminalID == "" {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	if terminal.VTerm != nil {
		m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
		terminal = m.runtime.Registry().Get(pane.TerminalID)
	}
	if terminal == nil || !terminal.Stream.Active || terminal.Snapshot == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	m.copyModeResume = copyModeResumeState{
		PaneID:          pane.ID,
		TerminalID:      pane.TerminalID,
		Snapshot:        cloneSnapshot(m.copyMode.Snapshot),
		BaselineVersion: terminal.SurfaceVersion,
	}
}

func (m *Model) activeCopyModeResumeSnapshot() (string, *protocol.Snapshot, bool) {
	if m == nil || m.copyModeResume.Snapshot == nil || m.workbench == nil || m.runtime == nil {
		return "", nil, false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyModeResume.PaneID || pane.TerminalID != m.copyModeResume.TerminalID {
		return "", nil, false
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || !terminal.Stream.Active || terminal.Snapshot == nil {
		return "", nil, false
	}
	// If a live local surface exists, prefer it immediately on copy-mode exit.
	// Keeping the frozen snapshot until another interaction lands makes the
	// terminal appear stuck even though the local VTerm is already current.
	if terminal.VTerm != nil && terminal.SurfaceVersion > 0 {
		return "", nil, false
	}
	if terminal.SurfaceVersion != m.copyModeResume.BaselineVersion {
		return "", nil, false
	}
	return pane.ID, m.copyModeResume.Snapshot, true
}

func (m *Model) leaveCopyMode() {
	if m == nil {
		return
	}
	if m.workbench != nil {
		if tab := m.workbench.CurrentTab(); tab != nil {
			tab.ScrollOffset = 0
		}
	}
	if m.runtime != nil && m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID != "" {
			m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
			m.runtime.PublishSurfaceForTesting(pane.TerminalID)
		}
	}
	m.resetCopyMode()
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
		return false
	}
	if m.copyMode.PaneID == pane.ID {
		buffer, ok := m.activeCopyModeBuffer()
		if !ok || buffer.totalRows() == 0 {
			return false
		}
		if delta := len(buffer.snapshot.Scrollback) - m.copyMode.LoadedRows; delta > 0 {
			m.copyMode.ViewTopRow += delta
			m.copyMode.Cursor.Row += delta
			if m.copyMode.Mark != nil {
				point := *m.copyMode.Mark
				point.Row += delta
				m.copyMode.Mark = &point
			}
		}
		m.copyMode.LoadedRows = len(buffer.snapshot.Scrollback)
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if m.copyMode.Mark != nil {
			point := buffer.clampPoint(*m.copyMode.Mark)
			m.copyMode.Mark = &point
		}
		m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
		return true
	}
	liveBuffer, ok := m.activeLiveCopyModeBuffer()
	if !ok || liveBuffer.totalRows() == 0 {
		return false
	}
	frozenSnapshot := cloneSnapshot(liveBuffer.snapshot)
	if frozenSnapshot == nil {
		return false
	}
	buffer := copyModeBuffer{
		snapshot: frozenSnapshot,
		height:   liveBuffer.height,
	}
	start := copyModePoint{Row: maxInt(0, len(buffer.snapshot.Scrollback)+buffer.cursorRow()), Col: maxInt(0, buffer.cursorCol())}
	start = buffer.clampPoint(start)
	m.copyMode = copyModeState{
		PaneID:     pane.ID,
		Snapshot:   frozenSnapshot,
		LoadedRows: len(buffer.snapshot.Scrollback),
		ViewTopRow: maxInt(0, buffer.totalRows()-buffer.height),
		Cursor:     start,
	}
	m.syncCopyModeViewport(buffer, start)
	return true
}

func (m *Model) adjustCopyModeAfterSnapshotLoaded(terminalID string) {
	if m == nil || terminalID == "" || m.copyMode.PaneID == "" || m.workbench == nil {
		return
	}
	if m.copyMode.Snapshot != nil {
		return
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyMode.PaneID || pane.TerminalID != terminalID {
		return
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return
	}
	if delta := len(buffer.snapshot.Scrollback) - m.copyMode.LoadedRows; delta > 0 {
		m.copyMode.ViewTopRow += delta
		m.copyMode.Cursor.Row += delta
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if m.copyMode.Mark != nil {
			point := *m.copyMode.Mark
			point.Row += delta
			point = buffer.clampPoint(point)
			m.copyMode.Mark = &point
		}
	}
	m.copyMode.LoadedRows = len(buffer.snapshot.Scrollback)
	m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
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
