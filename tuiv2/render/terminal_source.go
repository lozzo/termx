package render

import (
	"os"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
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

type snapshotRenderSource struct {
	snapshot *protocol.Snapshot
}

type surfaceRenderSource struct {
	surface runtime.TerminalSurface
}

var forceSnapshotSource = os.Getenv("TERMX_FORCE_SNAPSHOT_SOURCE") == "1"

func renderSource(snapshot *protocol.Snapshot, surface runtime.TerminalSurface) terminalRenderSource {
	switch {
	case forceSnapshotSource && snapshot != nil:
		return snapshotRenderSource{snapshot: snapshot}
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

func terminalMetricsForSource(source terminalRenderSource) renderTerminalMetrics {
	if source == nil {
		return renderTerminalMetrics{}
	}
	metrics := renderTerminalMetrics{
		Cols: int(source.Size().Cols),
		Rows: int(source.Size().Rows),
	}
	if metrics.Rows <= 0 {
		metrics.Rows = source.ScreenRows()
	}
	if metrics.Cols <= 0 {
		for row := source.ScrollbackRows(); row < source.TotalRows(); row++ {
			if rowW := protocolRowDisplayWidth(source.Row(row)); rowW > metrics.Cols {
				metrics.Cols = rowW
			}
		}
	}
	return metrics
}

func renderSourceRowMaxCol(source terminalRenderSource, rowIndex int) int {
	row := source.Row(rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if source == nil || source.Size().Cols == 0 {
		return 0
	}
	return int(source.Size().Cols) - 1
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
