package render

import (
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/termx-core/vterm"
)

type terminalRenderSource interface {
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

type terminalRowHashSource interface {
	RowHash(rowIndex int) uint64
}

type terminalRowContentHashSource interface {
	RowContentHash(rowIndex int) uint64
}

type terminalRowIdentityHashSource interface {
	RowIdentityHash(rowIndex int) uint64
}

type terminalRowVisualHashSource interface {
	RowVisualHash(rowIndex int) uint64
}

type terminalCellRowSource interface {
	RowView(rowIndex int) []localvterm.Cell
}

type snapshotRenderSource struct {
	snapshot *protocol.Snapshot
}

type surfaceRenderSource struct {
	surface runtime.TerminalSurface
}

func renderSource(snapshot *protocol.Snapshot, surface runtime.TerminalSurface) terminalRenderSource {
	switch {
	case surface != nil:
		return surfaceRenderSource{surface: surface}
	case snapshot != nil:
		return snapshotRenderSource{snapshot: snapshot}
	default:
		return nil
	}
}

func (s snapshotRenderSource) Size() protocol.Size { return s.snapshot.Size }

func (s snapshotRenderSource) Cursor() protocol.CursorState { return s.snapshot.Cursor }

func (s snapshotRenderSource) Modes() protocol.TerminalModes { return s.snapshot.Modes }

func (s snapshotRenderSource) IsAlternateScreen() bool {
	return s.snapshot != nil && s.snapshot.Screen.IsAlternateScreen
}

func (s snapshotRenderSource) ScreenRows() int {
	if s.snapshot == nil {
		return 0
	}
	return len(s.snapshot.Screen.Cells)
}

func (s snapshotRenderSource) ScrollbackRows() int {
	if s.snapshot == nil {
		return 0
	}
	return len(s.snapshot.Scrollback)
}

func (s snapshotRenderSource) TotalRows() int {
	return s.ScrollbackRows() + s.ScreenRows()
}

func (s snapshotRenderSource) Row(rowIndex int) []protocol.Cell {
	return snapshotRow(s.snapshot, rowIndex)
}

func (s snapshotRenderSource) RowTimestamp(rowIndex int) time.Time {
	return snapshotRowTimestamp(s.snapshot, rowIndex)
}

func (s snapshotRenderSource) RowKind(rowIndex int) string {
	return snapshotRowKind(s.snapshot, rowIndex)
}

func (s snapshotRenderSource) RowHash(rowIndex int) uint64 {
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	if s.snapshot == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := snapshotRowKind(s.snapshot, rowIndex)
	hash = fnvMixString(hash, kind)
	if kind != "" {
		ts := snapshotRowTimestamp(s.snapshot, rowIndex)
		hash = fnvMixInt64(hash, ts.UnixNano())
		return hash
	}
	row := snapshotRow(s.snapshot, rowIndex)
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func (s snapshotRenderSource) RowContentHash(rowIndex int) uint64 {
	hash := fnvOffset64
	if s.snapshot == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := snapshotRowKind(s.snapshot, rowIndex)
	hash = fnvMixString(hash, kind)
	ts := snapshotRowTimestamp(s.snapshot, rowIndex)
	hash = fnvMixInt64(hash, ts.UnixNano())
	return hashProtocolRow(hash, snapshotRow(s.snapshot, rowIndex))
}

func (s snapshotRenderSource) RowIdentityHash(rowIndex int) uint64 {
	return s.RowContentHash(rowIndex)
}

func (s snapshotRenderSource) RowVisualHash(rowIndex int) uint64 {
	if s.snapshot == nil || rowIndex < 0 {
		return fnvMixUint64(fnvOffset64, 0)
	}
	return hashProtocolRow(fnvOffset64, snapshotRow(s.snapshot, rowIndex))
}

func (s surfaceRenderSource) Size() protocol.Size { return s.surface.Size() }

func (s surfaceRenderSource) Cursor() protocol.CursorState { return s.surface.Cursor() }

func (s surfaceRenderSource) Modes() protocol.TerminalModes { return s.surface.Modes() }

func (s surfaceRenderSource) IsAlternateScreen() bool { return s.surface.IsAlternateScreen() }

func (s surfaceRenderSource) ScreenRows() int { return s.surface.ScreenRows() }

func (s surfaceRenderSource) ScrollbackRows() int { return s.surface.ScrollbackRows() }

func (s surfaceRenderSource) TotalRows() int { return s.surface.TotalRows() }

func (s surfaceRenderSource) Row(rowIndex int) []protocol.Cell { return s.surface.Row(rowIndex) }

func (s surfaceRenderSource) RowTimestamp(rowIndex int) time.Time {
	return s.surface.RowTimestamp(rowIndex)
}

func (s surfaceRenderSource) RowKind(rowIndex int) string { return s.surface.RowKind(rowIndex) }

func (s surfaceRenderSource) RowHash(rowIndex int) uint64 {
	if source, ok := s.surface.(interface{ RowHash(int) uint64 }); ok {
		return source.RowHash(rowIndex)
	}
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	if s.surface == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := s.surface.RowKind(rowIndex)
	hash = fnvMixString(hash, kind)
	if kind != "" {
		ts := s.surface.RowTimestamp(rowIndex)
		hash = fnvMixInt64(hash, ts.UnixNano())
		return hash
	}
	row := s.surface.Row(rowIndex)
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func (s surfaceRenderSource) RowContentHash(rowIndex int) uint64 {
	if source, ok := s.surface.(interface{ RowContentHash(int) uint64 }); ok {
		return source.RowContentHash(rowIndex)
	}
	hash := fnvOffset64
	if s.surface == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := s.surface.RowKind(rowIndex)
	hash = fnvMixString(hash, kind)
	ts := s.surface.RowTimestamp(rowIndex)
	hash = fnvMixInt64(hash, ts.UnixNano())
	if rowSource, ok := s.surface.(interface{ RowView(int) []localvterm.Cell }); ok {
		return hashVTermRow(hash, rowSource.RowView(rowIndex))
	}
	return hashProtocolRow(hash, s.surface.Row(rowIndex))
}

func (s surfaceRenderSource) RowIdentityHash(rowIndex int) uint64 {
	if source, ok := s.surface.(interface{ RowIdentityHash(int) uint64 }); ok {
		return source.RowIdentityHash(rowIndex)
	}
	return s.RowContentHash(rowIndex)
}

func (s surfaceRenderSource) RowVisualHash(rowIndex int) uint64 {
	if source, ok := s.surface.(interface{ RowVisualHash(int) uint64 }); ok {
		return source.RowVisualHash(rowIndex)
	}
	if rowSource, ok := s.surface.(interface{ RowView(int) []localvterm.Cell }); ok {
		return hashVTermRow(fnvOffset64, rowSource.RowView(rowIndex))
	}
	return hashProtocolRow(fnvOffset64, s.surface.Row(rowIndex))
}

func (s surfaceRenderSource) RowView(rowIndex int) []localvterm.Cell {
	if source, ok := s.surface.(interface{ RowView(int) []localvterm.Cell }); ok {
		return source.RowView(rowIndex)
	}
	return nil
}

func hashProtocolRow(hash uint64, row []protocol.Cell) uint64 {
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func hashVTermRow(hash uint64, row []localvterm.Cell) uint64 {
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func renderSourceCursorProjectionTarget(rect workbench.Rect, source terminalRenderSource) (cursorProjectionTarget, bool) {
	if source == nil {
		return cursorProjectionTarget{}, false
	}
	cursor := source.Cursor()
	cursorX := rect.X + cursor.Col
	cursorY := rect.Y + cursor.Row
	if cursorX < rect.X || cursorY < rect.Y || cursorX >= rect.X+rect.W || cursorY >= rect.Y+rect.H {
		return cursorProjectionTarget{}, false
	}
	return cursorProjectionTarget{
		X:     cursorX,
		Y:     cursorY,
		Shape: cursor.Shape,
		Blink: cursor.Blink,
	}, true
}

func renderSourceLikelyOwnsVisualCursor(source terminalRenderSource) bool {
	if source == nil {
		return false
	}
	modes := source.Modes()
	return source.IsAlternateScreen() ||
		modes.AlternateScreen ||
		modes.MouseTracking ||
		modes.BracketedPaste
}

func visualCursorProjectionTargetForSource(rect workbench.Rect, source terminalRenderSource) (cursorProjectionTarget, bool) {
	if source == nil || !renderSourceLikelyOwnsVisualCursor(source) {
		return cursorProjectionTarget{}, false
	}
	screenRows := source.ScreenRows()
	if screenRows == 0 {
		return cursorProjectionTarget{}, false
	}
	startRow := maxInt(0, screenRows/2)
	base := source.ScrollbackRows()
	for row := screenRows - 1; row >= startRow; row-- {
		cells := source.Row(base + row)
		for col := 0; col < len(cells) && col < rect.W; col++ {
			if !cellLooksLikeVisualCursor(cells, col) {
				continue
			}
			return cursorProjectionTarget{
				X:     rect.X + col,
				Y:     rect.Y + row,
				Shape: "block",
				Blink: false,
			}, true
		}
	}
	return cursorProjectionTarget{}, false
}

func shouldPreferVisualCursorTargetForSource(source terminalRenderSource, snapshotTarget, visualTarget cursorProjectionTarget, visualOK bool) bool {
	if source == nil || !visualOK {
		return false
	}
	cursor := source.Cursor()
	if !cursor.Visible {
		return true
	}
	if !renderSourceLikelyOwnsVisualCursor(source) {
		return false
	}
	return cursor.Row <= 1 && visualTarget.Y >= snapshotTarget.Y+2
}

func protocolRowDisplayWidth(row []protocol.Cell) int {
	width := 0
	for _, cell := range row {
		switch {
		case cell.Content == "" && cell.Width == 0:
			continue
		case cell.Width > 0:
			width += cell.Width
		case cell.Content != "":
			width += xansi.StringWidth(cell.Content)
		default:
			width++
		}
	}
	return width
}
