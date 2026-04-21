package runtime

import (
	"time"

	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

type TerminalSurface interface {
	Size() protocol.Size
	Cursor() protocol.CursorState
	Modes() protocol.TerminalModes
	IsAlternateScreen() bool
	ScreenRows() int
	ScrollbackRows() int
	TotalRows() int
	Row(rowIndex int) []protocol.Cell
	RowTimestamp(rowIndex int) time.Time
	RowKind(rowIndex int) string
}

type rowSurfaceSource interface {
	Size() (int, int)
	CursorState() localvterm.CursorState
	Modes() localvterm.TerminalModes
	IsAltScreen() bool
	ScreenRowCount() int
	ScrollbackRowCount() int
	ScreenRowView(row int) []localvterm.Cell
	ScrollbackRowView(row int) []localvterm.Cell
	ScreenRowTimestampAt(row int) time.Time
	ScrollbackRowTimestampAt(row int) time.Time
	ScreenRowKindAt(row int) string
	ScrollbackRowKindAt(row int) string
}

type vtermSurface struct {
	source rowSurfaceSource
}

func surfaceFromVTerm(vt VTermLike) TerminalSurface {
	if vt == nil {
		return nil
	}
	source, ok := vt.(rowSurfaceSource)
	if !ok {
		return nil
	}
	return vtermSurface{source: source}
}

func visibleSurface(terminal *TerminalRuntime) TerminalSurface {
	if terminal == nil || terminal.SurfaceVersion == 0 || terminal.PreferSnapshot {
		return nil
	}
	return surfaceFromVTerm(terminal.VTerm)
}

func (r *Runtime) LiveSurface(terminalID string) TerminalSurface {
	if r == nil || r.registry == nil || terminalID == "" {
		return nil
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || terminal.SurfaceVersion == 0 || terminal.VTerm == nil {
		return nil
	}
	return surfaceFromVTerm(terminal.VTerm)
}

func (s vtermSurface) Size() protocol.Size {
	cols, rows := s.source.Size()
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

func (s vtermSurface) Cursor() protocol.CursorState {
	return protocolCursorFromVTerm(s.source.CursorState())
}

func (s vtermSurface) Modes() protocol.TerminalModes {
	return protocolModesFromVTerm(s.source.Modes())
}

func (s vtermSurface) IsAlternateScreen() bool {
	return s.source.IsAltScreen()
}

func (s vtermSurface) ScreenRows() int {
	return s.source.ScreenRowCount()
}

func (s vtermSurface) ScrollbackRows() int {
	return s.source.ScrollbackRowCount()
}

func (s vtermSurface) TotalRows() int {
	return s.ScrollbackRows() + s.ScreenRows()
}

func (s vtermSurface) Row(rowIndex int) []protocol.Cell {
	if rowIndex < 0 {
		return nil
	}
	if rowIndex < s.ScrollbackRows() {
		return protocolCellsFromVTermRow(s.source.ScrollbackRowView(rowIndex))
	}
	rowIndex -= s.ScrollbackRows()
	if rowIndex < 0 || rowIndex >= s.ScreenRows() {
		return nil
	}
	return protocolCellsFromVTermRow(s.source.ScreenRowView(rowIndex))
}

func (s vtermSurface) RowView(rowIndex int) []localvterm.Cell {
	if rowIndex < 0 {
		return nil
	}
	if rowIndex < s.ScrollbackRows() {
		return s.source.ScrollbackRowView(rowIndex)
	}
	rowIndex -= s.ScrollbackRows()
	if rowIndex < 0 || rowIndex >= s.ScreenRows() {
		return nil
	}
	return s.source.ScreenRowView(rowIndex)
}

func (s vtermSurface) RowTimestamp(rowIndex int) time.Time {
	if rowIndex < 0 {
		return time.Time{}
	}
	if rowIndex < s.ScrollbackRows() {
		return s.source.ScrollbackRowTimestampAt(rowIndex)
	}
	rowIndex -= s.ScrollbackRows()
	if rowIndex < 0 || rowIndex >= s.ScreenRows() {
		return time.Time{}
	}
	return s.source.ScreenRowTimestampAt(rowIndex)
}

func (s vtermSurface) RowKind(rowIndex int) string {
	if rowIndex < 0 {
		return ""
	}
	if rowIndex < s.ScrollbackRows() {
		return s.source.ScrollbackRowKindAt(rowIndex)
	}
	rowIndex -= s.ScrollbackRows()
	if rowIndex < 0 || rowIndex >= s.ScreenRows() {
		return ""
	}
	return s.source.ScreenRowKindAt(rowIndex)
}

func (s vtermSurface) RowHash(rowIndex int) uint64 {
	hash := surfaceRowHashOffset64
	hash = surfaceRowHashMixUint64(hash, uint64(rowIndex+1))
	if rowIndex < 0 {
		return hash
	}
	kind := s.RowKind(rowIndex)
	hash = surfaceRowHashMixString(hash, kind)
	if kind != "" {
		ts := s.RowTimestamp(rowIndex)
		hash = surfaceRowHashMixInt64(hash, ts.UnixNano())
		return hash
	}
	var row []localvterm.Cell
	switch {
	case rowIndex < s.ScrollbackRows():
		row = s.source.ScrollbackRowView(rowIndex)
	default:
		rowIndex -= s.ScrollbackRows()
		if rowIndex < 0 || rowIndex >= s.ScreenRows() {
			return hash
		}
		row = s.source.ScreenRowView(rowIndex)
	}
	hash = surfaceRowHashMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = surfaceRowHashMixString(hash, cell.Content)
		hash = surfaceRowHashMixInt64(hash, int64(cell.Width))
		hash = surfaceRowHashMixString(hash, cell.Style.FG)
		hash = surfaceRowHashMixString(hash, cell.Style.BG)
		hash = surfaceRowHashMixBool(hash, cell.Style.Bold)
		hash = surfaceRowHashMixBool(hash, cell.Style.Italic)
		hash = surfaceRowHashMixBool(hash, cell.Style.Underline)
		hash = surfaceRowHashMixBool(hash, cell.Style.Blink)
		hash = surfaceRowHashMixBool(hash, cell.Style.Reverse)
		hash = surfaceRowHashMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func protocolCellsFromVTermRow(row []localvterm.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocolCellFromVTermCell(cell)
	}
	return out
}

const (
	surfaceRowHashOffset64 = uint64(14695981039346656037)
	surfaceRowHashPrime64  = uint64(1099511628211)
)

func surfaceRowHashMixUint64(hash uint64, value uint64) uint64 {
	hash ^= value
	hash *= surfaceRowHashPrime64
	return hash
}

func surfaceRowHashMixInt64(hash uint64, value int64) uint64 {
	return surfaceRowHashMixUint64(hash, uint64(value))
}

func surfaceRowHashMixBool(hash uint64, value bool) uint64 {
	if value {
		return surfaceRowHashMixUint64(hash, 1)
	}
	return surfaceRowHashMixUint64(hash, 0)
}

func surfaceRowHashMixString(hash uint64, value string) uint64 {
	hash = surfaceRowHashMixUint64(hash, uint64(len(value)))
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= surfaceRowHashPrime64
	}
	return hash
}

func syncSurfaceScrollbackState(terminal *TerminalRuntime) {
	if terminal == nil {
		return
	}
	surface := surfaceFromVTerm(terminal.VTerm)
	if surface == nil {
		return
	}
	if loaded := surface.ScrollbackRows(); loaded > terminal.ScrollbackLoadedLimit {
		terminal.ScrollbackLoadedLimit = loaded
	}
	if terminal.ScrollbackLoadingLimit > 0 && surface.ScrollbackRows() >= terminal.ScrollbackLoadingLimit {
		terminal.ScrollbackLoadingLimit = 0
	}
}

func (r *Runtime) bumpSurfaceVersion(terminal *TerminalRuntime) {
	if terminal == nil {
		return
	}
	terminal.SurfaceVersion++
	syncSurfaceScrollbackState(terminal)
}
