package render

import (
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

type spriteTestSurface struct {
	size             protocol.Size
	cursor           protocol.CursorState
	modes            protocol.TerminalModes
	screen           [][]protocol.Cell
	scrollback       [][]protocol.Cell
	screenTimestamps []time.Time
	scrollTimestamps []time.Time
	screenKinds      []string
	scrollKinds      []string
}

func (s *spriteTestSurface) Size() protocol.Size { return s.size }

func (s *spriteTestSurface) Cursor() protocol.CursorState { return s.cursor }

func (s *spriteTestSurface) Modes() protocol.TerminalModes { return s.modes }

func (s *spriteTestSurface) IsAlternateScreen() bool { return false }

func (s *spriteTestSurface) ScreenRows() int { return len(s.screen) }

func (s *spriteTestSurface) ScrollbackRows() int { return len(s.scrollback) }

func (s *spriteTestSurface) TotalRows() int { return s.ScrollbackRows() + s.ScreenRows() }

func (s *spriteTestSurface) Row(rowIndex int) []protocol.Cell {
	if rowIndex < 0 {
		return nil
	}
	if rowIndex < len(s.scrollback) {
		return append([]protocol.Cell(nil), s.scrollback[rowIndex]...)
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screen) {
		return nil
	}
	return append([]protocol.Cell(nil), s.screen[rowIndex]...)
}

func (s *spriteTestSurface) RowTimestamp(rowIndex int) time.Time {
	if rowIndex < 0 {
		return time.Time{}
	}
	if rowIndex < len(s.scrollTimestamps) {
		return s.scrollTimestamps[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenTimestamps) {
		return time.Time{}
	}
	return s.screenTimestamps[rowIndex]
}

func (s *spriteTestSurface) RowKind(rowIndex int) string {
	if rowIndex < 0 {
		return ""
	}
	if rowIndex < len(s.scrollKinds) {
		return s.scrollKinds[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenKinds) {
		return ""
	}
	return s.screenKinds[rowIndex]
}

func protocolRowFromText(text string) []protocol.Cell {
	runes := []rune(text)
	row := make([]protocol.Cell, 0, len(runes))
	for _, r := range runes {
		row = append(row, protocol.Cell{Content: string(r), Width: 1})
	}
	return row
}
