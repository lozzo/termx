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
	PaneID              string
	Snapshot            *protocol.Snapshot
	ViewTopRow          int
	Cursor              copyModePoint
	CursorLogical       copyModeLogicalPos
	Mark                *copyModePoint
	MarkLogical         *copyModeLogicalPos
	MouseSelecting      bool
	AutoScrollDir       int
	AutoScrollSeq       uint64
}

type copyModeResumeState struct {
	PaneID          string
	TerminalID      string
	Snapshot        *protocol.Snapshot
	BaselineVersion uint64
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

func snapshotUsesAlternateScreen(snapshot *protocol.Snapshot) bool {
	return snapshot != nil && (snapshot.Modes.AlternateScreen || snapshot.Screen.IsAlternateScreen)
}

func clearSnapshotScrollback(snapshot *protocol.Snapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Scrollback = nil
	snapshot.ScrollbackTimestamps = nil
	snapshot.ScrollbackRowKinds = nil
	snapshot.ScrollbackWrapped = nil
	snapshot.ScrollbackOwnership = nil
	snapshot.ScrollbackOffset = 0
	snapshot.ScrollbackTotal = 0
	snapshot.ScrollbackLogicalTotal = 0
	snapshot.ScrollbackHasMore = false
	snapshot.ScrollbackLoadedRows = 0
	snapshot.HistoryGeneration = 0
	snapshot.ScrollbackFirstRowID = 0
	snapshot.ScrollbackLastRowID = 0
}

func trimCopyModeSnapshotScrollbackWindow(snapshot *protocol.Snapshot, limit int, trimNewest bool) int {
	if snapshot == nil || limit <= 0 || len(snapshot.Scrollback) <= limit {
		return 0
	}
	drop := len(snapshot.Scrollback) - limit
	// Keep canonical committed-row coordinates aligned with the loaded history
	// window even when the frozen materialized slice is locally bounded.
	if trimNewest {
		keep := len(snapshot.Scrollback) - drop
		committedDrop := protocol.CountCommittedRowOwnershipRange(snapshot.ScrollbackOwnership, keep, len(snapshot.ScrollbackOwnership))
		snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[:keep])
		snapshot.ScrollbackTimestamps = cloneTimePrefix(snapshot.ScrollbackTimestamps, keep)
		snapshot.ScrollbackRowKinds = cloneStringPrefix(snapshot.ScrollbackRowKinds, keep)
		snapshot.ScrollbackWrapped = cloneBoolPrefix(snapshot.ScrollbackWrapped, keep)
		snapshot.ScrollbackOwnership = cloneStringPrefix(snapshot.ScrollbackOwnership, keep)
		snapshot.ScrollbackOffset += committedDrop
		return drop
	}
	snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[drop:])
	snapshot.ScrollbackTimestamps = cloneTimeSuffix(snapshot.ScrollbackTimestamps, drop)
	snapshot.ScrollbackRowKinds = cloneStringSuffix(snapshot.ScrollbackRowKinds, drop)
	snapshot.ScrollbackWrapped = cloneBoolSuffix(snapshot.ScrollbackWrapped, drop)
	snapshot.ScrollbackOwnership = cloneStringSuffix(snapshot.ScrollbackOwnership, drop)
	snapshot.ScrollbackHasMore = true
	return 0
}

func cloneTimePrefix(values []time.Time, keep int) []time.Time {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]time.Time(nil), values[:keep]...)
}

func cloneStringPrefix(values []string, keep int) []string {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]string(nil), values[:keep]...)
}

func cloneBoolPrefix(values []bool, keep int) []bool {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]bool(nil), values[:keep]...)
}

func cloneTimeSuffix(values []time.Time, drop int) []time.Time {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]time.Time(nil), values[drop:]...)
}

func cloneStringSuffix(values []string, drop int) []string {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]string(nil), values[drop:]...)
}

func cloneBoolSuffix(values []bool, drop int) []bool {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]bool(nil), values[drop:]...)
}

func (m *Model) copyModeSnapshot(terminalID string, snapshot *protocol.Snapshot) *protocol.Snapshot {
	cloned := cloneSnapshot(snapshot)
	if snapshotUsesAlternateScreen(cloned) {
		if m != nil && m.runtime != nil {
			cloned = m.runtime.AlternateScrollbackSnapshot(terminalID, cloned)
		} else {
			clearSnapshotScrollback(cloned)
		}
	}
	protocol.TrimSnapshotScrollbackScreenVisualOverlap(cloned)
	return cloned
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
		m.copyModeResume = copyModeResumeState{}
	}
	if changed && m.mode().Kind == input.ModeDisplay && !m.activePaneInCopyMode() {
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	}
	if changed {
		m.render.Invalidate()
	}
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

func (m *Model) clearCopyModeOwnedHistoryLoadingForPane(paneID string) {
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
	m.clearCopyModeOwnedHistoryLoadingForPane(paneID)
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
	liveBuffer, ok := m.liveCopyModeBufferForPane(pane.ID)
	if !ok || liveBuffer.totalRows() == 0 {
		return false
	}
	frozenSnapshot := m.copyModeSnapshot(pane.TerminalID, liveBuffer.snapshot)
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
		ViewTopRow: maxInt(0, buffer.totalRows()-buffer.height),
		Cursor:     start,
	}
	if logical, ok := buffer.logicalPosForPoint(start); ok {
		m.copyMode.CursorLogical = logical
	}
	m.syncCopyModeViewport(buffer, start)
	m.saveCurrentCopyModeState()
	return true
}

func (m *Model) adjustCopyModeAfterSnapshotLoaded(terminalID string, snapshot *protocol.Snapshot) {
}

func (m *Model) adjustCopyModeAfterSnapshotLoadedWithWindow(terminalID string, snapshot *protocol.Snapshot, offset int, allowLatestReplace bool) bool {
	return false
}

func (m *Model) reanchorCopyModePoints(buffer copyModeBuffer, fallbackDelta int, preserveTop bool, preferRefs bool) {
	if m == nil {
		return
	}
	if preserveTop {
		m.copyMode.Cursor = buffer.clampPoint(copyModePoint{Row: 0, Col: m.copyMode.Cursor.Col})
		if logical, ok := buffer.logicalPosForPoint(m.copyMode.Cursor); ok {
			m.copyMode.CursorLogical = logical
		}
		if m.copyMode.Mark == nil {
			m.copyMode.MarkLogical = nil
			return
		}
		point := buffer.clampPoint(*m.copyMode.Mark)
		m.copyMode.Mark = &point
		if logical, ok := buffer.logicalPosForPoint(point); ok {
			m.copyMode.MarkLogical = &logical
		}
		return
	}
	if preferRefs {
		if !preserveTop && m.copyMode.CursorLogical.Line > 0 {
			logical := m.copyMode.CursorLogical
			logical.Line += fallbackDelta
			if point, ok := buffer.pointForLogicalPos(logical); ok {
				m.copyMode.Cursor = point
				m.copyMode.CursorLogical = logical
			} else {
				m.copyMode.Cursor.Row += fallbackDelta
			}
		}
	} else {
		m.copyMode.Cursor.Row += fallbackDelta
	}
	m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
	if logical, ok := buffer.logicalPosForPoint(m.copyMode.Cursor); ok {
		m.copyMode.CursorLogical = logical
	}
	if m.copyMode.Mark == nil {
		m.copyMode.MarkLogical = nil
		return
	}
	point := *m.copyMode.Mark
	if preferRefs {
		if m.copyMode.MarkLogical != nil && !preserveTop && m.copyMode.MarkLogical.Line > 0 {
			logical := *m.copyMode.MarkLogical
			logical.Line += fallbackDelta
			if mapped, ok := buffer.pointForLogicalPos(logical); ok {
				point = mapped
				m.copyMode.MarkLogical = &logical
			} else {
				point.Row += fallbackDelta
			}
		} else {
			point.Row += fallbackDelta
		}
	} else {
		point.Row += fallbackDelta
	}
	point = buffer.clampPoint(point)
	m.copyMode.Mark = &point
	if logical, ok := buffer.logicalPosForPoint(point); ok {
		m.copyMode.MarkLogical = &logical
	}
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
