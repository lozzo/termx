package app

import (
	"time"

	"github.com/lozzow/termx/protocol"
	tuiv2runtime "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

func (m *Model) floatingDragPreviewSnapshot(paneID, terminalID string, rect workbench.Rect) *protocol.Snapshot {
	if m == nil || m.runtime == nil || terminalID == "" {
		return nil
	}
	height := rect.H
	if contentRect, ok := paneContentRect(rect); ok {
		height = contentRect.H
	}
	if height <= 0 {
		height = 1
	}
	offset := m.paneViewportOffset(paneID)
	if surface := m.runtime.LiveSurface(terminalID); surface != nil {
		return snapshotFromTerminalSurfaceWindow(terminalID, surface, height, offset)
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		return nil
	}
	if terminal.Snapshot != nil {
		return snapshotWindowFromSnapshot(terminal.Snapshot, height, offset)
	}
	if terminal.VTerm != nil {
		return snapshotFromVTermScreenWindow(terminalID, terminal.VTerm, height)
	}
	return nil
}

func snapshotFromTerminalSurfaceWindow(terminalID string, surface tuiv2runtime.TerminalSurface, height, offset int) *protocol.Snapshot {
	if surface == nil {
		return nil
	}
	scrollbackRows := surface.ScrollbackRows()
	screenRows := surface.ScreenRows()
	start, end := terminalPreviewWindowRange(scrollbackRows, screenRows, height, offset)
	rows := make([][]protocol.Cell, 0, maxInt(0, end-start))
	times := make([]time.Time, 0, maxInt(0, end-start))
	kinds := make([]string, 0, maxInt(0, end-start))
	for row := start; row < end; row++ {
		rows = append(rows, cloneProtocolCellRowForPreview(surface.Row(row)))
		times = append(times, surface.RowTimestamp(row))
		kinds = append(kinds, surface.RowKind(row))
	}
	size := surface.Size()
	size.Rows = uint16(len(rows))
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       size,
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: surface.IsAlternateScreen(),
		},
		ScreenTimestamps: times,
		ScreenRowKinds:   kinds,
		Cursor:           cursorForPreviewWindow(surface.Cursor(), scrollbackRows, start, len(rows)),
		Modes:            surface.Modes(),
		Timestamp:        time.Now(),
	}
}

func snapshotWindowFromSnapshot(snapshot *protocol.Snapshot, height, offset int) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	scrollbackRows := len(snapshot.Scrollback)
	screenRows := len(snapshot.Screen.Cells)
	start, end := terminalPreviewWindowRange(scrollbackRows, screenRows, height, offset)
	rows := make([][]protocol.Cell, 0, maxInt(0, end-start))
	times := make([]time.Time, 0, maxInt(0, end-start))
	kinds := make([]string, 0, maxInt(0, end-start))
	for row := start; row < end; row++ {
		rows = append(rows, cloneProtocolCellRowForPreview(snapshotWindowRow(snapshot, row)))
		times = append(times, snapshotWindowRowTimestamp(snapshot, row))
		kinds = append(kinds, snapshotWindowRowKind(snapshot, row))
	}
	size := snapshot.Size
	size.Rows = uint16(len(rows))
	return &protocol.Snapshot{
		TerminalID: snapshot.TerminalID,
		Size:       size,
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
		},
		ScreenTimestamps: times,
		ScreenRowKinds:   kinds,
		Cursor:           cursorForPreviewWindow(snapshot.Cursor, scrollbackRows, start, len(rows)),
		Modes:            snapshot.Modes,
		Timestamp:        time.Now(),
	}
}

func snapshotFromVTermScreenWindow(terminalID string, vt tuiv2runtime.VTermLike, height int) *protocol.Snapshot {
	if vt == nil {
		return nil
	}
	screen := vt.ScreenContent()
	limit := minInt(maxInt(0, height), len(screen.Cells))
	rows := make([][]protocol.Cell, 0, limit)
	for row := 0; row < limit; row++ {
		rows = append(rows, protocolCellsFromVTermCellsForPreview(screen.Cells[row]))
	}
	cols, _ := vt.Size()
	cursor := protocolCursorFromVTermForPreview(vt.CursorState())
	if cursor.Row < 0 || cursor.Row >= len(rows) {
		cursor.Visible = false
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(maxInt(0, cols)), Rows: uint16(len(rows))},
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: screen.IsAlternateScreen,
		},
		Cursor:    cursor,
		Modes:     protocolModesFromVTermForPreview(vt.Modes()),
		Timestamp: time.Now(),
	}
}

func terminalPreviewWindowRange(scrollbackRows, screenRows, height, offset int) (int, int) {
	if height <= 0 {
		return 0, 0
	}
	totalRows := scrollbackRows + screenRows
	if totalRows <= 0 {
		return 0, 0
	}
	if offset <= 0 {
		start := scrollbackRows
		end := start + minInt(height, screenRows)
		return start, minInt(end, totalRows)
	}
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	if end > totalRows {
		end = totalRows
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	return start, end
}

func snapshotWindowRow(snapshot *protocol.Snapshot, row int) []protocol.Cell {
	if snapshot == nil || row < 0 {
		return nil
	}
	if row < len(snapshot.Scrollback) {
		return snapshot.Scrollback[row]
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return nil
	}
	return snapshot.Screen.Cells[row]
}

func snapshotWindowRowTimestamp(snapshot *protocol.Snapshot, row int) time.Time {
	if snapshot == nil || row < 0 {
		return time.Time{}
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackTimestamps) {
			return snapshot.ScrollbackTimestamps[row]
		}
		return time.Time{}
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.ScreenTimestamps) {
		return time.Time{}
	}
	return snapshot.ScreenTimestamps[row]
}

func snapshotWindowRowKind(snapshot *protocol.Snapshot, row int) string {
	if snapshot == nil || row < 0 {
		return ""
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackRowKinds) {
			return snapshot.ScrollbackRowKinds[row]
		}
		return ""
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.ScreenRowKinds) {
		return ""
	}
	return snapshot.ScreenRowKinds[row]
}

func cursorForPreviewWindow(cursor protocol.CursorState, scrollbackRows, start, rows int) protocol.CursorState {
	if !cursor.Visible {
		return cursor
	}
	absoluteRow := scrollbackRows + cursor.Row
	if absoluteRow < start || absoluteRow >= start+rows {
		cursor.Visible = false
		return cursor
	}
	cursor.Row = absoluteRow - start
	return cursor
}

func cloneProtocolCellRowForPreview(row []protocol.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	return append([]protocol.Cell(nil), row...)
}

func protocolCellsFromVTermCellsForPreview(row []localvterm.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocol.Cell{
			Content: cell.Content,
			Width:   cell.Width,
			Style: protocol.CellStyle{
				FG:            cell.Style.FG,
				BG:            cell.Style.BG,
				Bold:          cell.Style.Bold,
				Italic:        cell.Style.Italic,
				Underline:     cell.Style.Underline,
				Blink:         cell.Style.Blink,
				Reverse:       cell.Style.Reverse,
				Strikethrough: cell.Style.Strikethrough,
			},
		}
	}
	return out
}

func protocolCursorFromVTermForPreview(cursor localvterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesFromVTermForPreview(modes localvterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}
