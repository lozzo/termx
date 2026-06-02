package historyview

import (
	"strings"
	"sync"

	"github.com/lozzow/termx/internal/protocol"
)

// MemoryStore 保存 core 返回的 authoritative history window 和 copy mode 交互态。
// 它不把窗口反写成 runtime/local history truth。
type MemoryStore struct {
	mu         sync.Mutex
	surfaces   map[string]LiveSurface
	windows    map[string]HistoryWindow
	viewports  map[string]int
	cursors    map[string]Cursor
	selections map[string]Selection
	pending    map[string]WindowToken
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		surfaces:   make(map[string]LiveSurface),
		windows:    make(map[string]HistoryWindow),
		viewports:  make(map[string]int),
		cursors:    make(map[string]Cursor),
		selections: make(map[string]Selection),
		pending:    make(map[string]WindowToken),
	}
}

func (s *MemoryStore) ApplyLiveSurface(surface LiveSurface) {
	if s == nil || strings.TrimSpace(surface.TerminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	s.surfaces[surface.TerminalID] = cloneLiveSurface(surface)
}

func (s *MemoryStore) ApplyHistoryWindow(window HistoryWindow) bool {
	if s == nil || strings.TrimSpace(window.TerminalID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	window = normalizeHistoryWindow(window)
	current, hasCurrent := s.windows[window.TerminalID]
	if !s.acceptWindowLocked(current, hasCurrent, window) {
		return false
	}
	if window.Op == WindowOpPrepend && hasCurrent {
		window = prependHistoryWindow(window, current)
	}
	s.windows[window.TerminalID] = cloneHistoryWindow(window)
	if window.Op == WindowOpReplace {
		s.viewports[window.TerminalID] = clampInt(s.viewports[window.TerminalID], 0, maxInt(0, len(window.Rows)-1))
	} else {
		s.viewports[window.TerminalID] += len(window.Rows) - len(current.Rows)
	}
	if s.pending[window.TerminalID] == window.Token {
		delete(s.pending, window.TerminalID)
	}
	return true
}

func (s *MemoryStore) LiveSurface(terminalID string) (LiveSurface, bool) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return LiveSurface{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	surface, ok := s.surfaces[terminalID]
	return cloneLiveSurface(surface), ok
}

func (s *MemoryStore) HistoryWindow(terminalID string) (HistoryWindow, bool) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return HistoryWindow{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	window, ok := s.windows[terminalID]
	return cloneHistoryWindow(window), ok
}

func (s *MemoryStore) SetViewportTop(terminalID string, row int) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	maxRow := 0
	if window, ok := s.windows[terminalID]; ok {
		maxRow = maxInt(0, len(window.Rows)-1)
	}
	s.viewports[terminalID] = clampInt(row, 0, maxRow)
}

func (s *MemoryStore) ViewportTop(terminalID string) int {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	return s.viewports[terminalID]
}

func (s *MemoryStore) SetCursor(terminalID string, cursor Cursor) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	s.cursors[terminalID] = cursor
}

func (s *MemoryStore) Cursor(terminalID string) (Cursor, bool) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return Cursor{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	cursor, ok := s.cursors[terminalID]
	return cursor, ok
}

func (s *MemoryStore) SetSelection(terminalID string, selection Selection) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	if !selection.Active {
		delete(s.selections, terminalID)
		return
	}
	s.selections[terminalID] = selection
}

func (s *MemoryStore) Selection(terminalID string) (Selection, bool) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return Selection{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	selection, ok := s.selections[terminalID]
	return selection, ok
}

func (s *MemoryStore) ClearSelection(terminalID string) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	delete(s.selections, terminalID)
}

func (s *MemoryStore) SetPendingRequest(terminalID string, token WindowToken) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	if token == "" {
		delete(s.pending, terminalID)
		return
	}
	s.pending[terminalID] = token
}

func (s *MemoryStore) PendingRequest(terminalID string) WindowToken {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	return s.pending[terminalID]
}

func (s *MemoryStore) ClearPendingRequest(terminalID string, token WindowToken) {
	if s == nil || strings.TrimSpace(terminalID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	if token == "" || s.pending[terminalID] == token {
		delete(s.pending, terminalID)
	}
}

func (s *MemoryStore) ensureLocked() {
	if s.surfaces == nil {
		s.surfaces = make(map[string]LiveSurface)
	}
	if s.windows == nil {
		s.windows = make(map[string]HistoryWindow)
	}
	if s.viewports == nil {
		s.viewports = make(map[string]int)
	}
	if s.cursors == nil {
		s.cursors = make(map[string]Cursor)
	}
	if s.selections == nil {
		s.selections = make(map[string]Selection)
	}
	if s.pending == nil {
		s.pending = make(map[string]WindowToken)
	}
}

func (s *MemoryStore) acceptWindowLocked(current HistoryWindow, hasCurrent bool, next HistoryWindow) bool {
	switch next.Op {
	case WindowOpReplace:
		return true
	case WindowOpPrepend:
		if !hasCurrent || current.Token == "" || next.Token == "" {
			return false
		}
		if next.Token != current.Token {
			return false
		}
		if next.Generation != 0 && current.Generation != 0 && next.Generation != current.Generation {
			return false
		}
		if next.LastBoundaryID != 0 && current.FirstBoundaryID != 0 && next.LastBoundaryID >= current.FirstBoundaryID {
			return false
		}
		if next.LastLineID != 0 && current.FirstLineID != 0 && next.LastLineID >= current.FirstLineID {
			return false
		}
		return true
	default:
		return false
	}
}

func prependHistoryWindow(older, current HistoryWindow) HistoryWindow {
	baseRows := len(older.Rows)
	baseLines := len(older.Lines)
	older.Rows = append(cloneHistoryRows(older.Rows), cloneHistoryRows(current.Rows)...)
	lines := cloneLineSpans(older.Lines)
	for _, span := range current.Lines {
		span.StartRow += baseRows
		span.EndRow += baseRows
		lines = append(lines, span)
	}
	older.Lines = lines
	older.Op = WindowOpPrepend
	older.LoadedRows = maxInt(older.LoadedRows, current.LoadedRows+baseRows)
	older.TotalRows = maxInt(older.TotalRows, current.TotalRows)
	older.LoadedLines = maxInt(older.LoadedLines, current.LoadedLines+baseLines)
	older.TotalLines = maxInt(older.TotalLines, current.TotalLines)
	older.FirstLineID = firstNonZero(older.FirstLineID, current.FirstLineID)
	older.LastLineID = current.LastLineID
	older.FirstBoundaryID = firstNonZero(older.FirstBoundaryID, current.FirstBoundaryID)
	older.LastBoundaryID = current.LastBoundaryID
	older.Token = current.Token
	if older.Timestamp.IsZero() {
		older.Timestamp = current.Timestamp
	}
	return older
}

func normalizeHistoryWindow(window HistoryWindow) HistoryWindow {
	if window.LoadedLines == 0 && !historyWindowHasClippedBeforeLine(window.Lines) {
		window.LoadedLines = len(window.Lines)
	}
	if window.TotalLines == 0 {
		window.TotalLines = window.LoadedLines
	}
	if window.FirstBoundaryID == 0 {
		window.FirstBoundaryID = window.FirstLineID
	}
	if window.LastBoundaryID == 0 {
		window.LastBoundaryID = window.LastLineID
	}
	return cloneHistoryWindow(window)
}

func historyWindowHasClippedBeforeLine(lines []LineSpan) bool {
	for _, line := range lines {
		if line.ClippedBefore {
			return true
		}
	}
	return false
}

func cloneLiveSurface(surface LiveSurface) LiveSurface {
	surface.Screen = protocol.ScreenData{
		Cells:             cloneProtocolRows(surface.Screen.Cells),
		IsAlternateScreen: surface.Screen.IsAlternateScreen,
	}
	return surface
}

func cloneHistoryWindow(window HistoryWindow) HistoryWindow {
	window.Rows = cloneHistoryRows(window.Rows)
	window.Lines = cloneLineSpans(window.Lines)
	return window
}

func cloneHistoryRows(rows []HistoryRow) []HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]HistoryRow, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = protocol.CloneCompactRow(row.Cells)
	}
	return out
}

func cloneLineSpans(spans []LineSpan) []LineSpan {
	if len(spans) == 0 {
		return nil
	}
	return append([]LineSpan(nil), spans...)
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

func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
