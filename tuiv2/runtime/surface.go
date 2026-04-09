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
	ScreenRow(row int) []localvterm.Cell
	ScrollbackRow(row int) []localvterm.Cell
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
	if terminal == nil || terminal.SurfaceVersion == 0 {
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
		return protocolCellsFromVTermRow(s.source.ScrollbackRow(rowIndex))
	}
	rowIndex -= s.ScrollbackRows()
	if rowIndex < 0 || rowIndex >= s.ScreenRows() {
		return nil
	}
	return protocolCellsFromVTermRow(s.source.ScreenRow(rowIndex))
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

func (r *Runtime) markSurfaceChanged(terminal *TerminalRuntime) {
	if r == nil || terminal == nil {
		return
	}
	r.bumpSurfaceVersion(terminal)
	r.invalidate()
}
