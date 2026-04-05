package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

func (r *Runtime) LoadSnapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	snapshot, err := r.client.Snapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: err}
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal != nil {
		terminal.Snapshot = snapshot
		terminal.ScrollbackLoadingLimit = 0
		if offset == 0 && snapshot != nil {
			if loaded := len(snapshot.Scrollback); loaded > terminal.ScrollbackLoadedLimit {
				terminal.ScrollbackLoadedLimit = loaded
			}
			if limit > 0 {
				terminal.ScrollbackExhausted = len(snapshot.Scrollback) < limit
			}
		}
		r.ensureVTerm(terminal)
		loadSnapshotIntoVTerm(terminal.VTerm, snapshot)
		r.touch()
	}
	return snapshot, nil
}

func (r *Runtime) refreshSnapshot(terminalID string) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || terminal.VTerm == nil {
		return
	}
	terminal.Snapshot = snapshotFromVTerm(terminalID, terminal.VTerm)
	r.invalidate()
}

func loadSnapshotIntoVTerm(vt VTermLike, snap *protocol.Snapshot) {
	if vt == nil || snap == nil {
		return
	}
	cols, rows := vt.Size()
	vt.LoadSnapshotWithScrollback(protocolRowsToVTerm(snap.Scrollback), protocolScreenToVTerm(snap.Screen), protocolCursorToVTerm(snap.Cursor), protocolModesToVTerm(snap.Modes))
	if cols > 0 && rows > 0 {
		vt.Resize(cols, rows)
	}
}

func snapshotFromVTerm(terminalID string, vt VTermLike) *protocol.Snapshot {
	if vt == nil {
		return nil
	}
	cols, rows := vt.Size()
	screen := vt.ScreenContent()
	outRows := make([][]protocol.Cell, 0, len(screen.Cells))
	for _, row := range screen.Cells {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		outRows = append(outRows, out)
	}
	scrollback := vt.ScrollbackContent()
	backlog := make([][]protocol.Cell, 0, len(scrollback))
	for _, row := range scrollback {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		backlog = append(backlog, out)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen: protocol.ScreenData{
			Cells:             outRows,
			IsAlternateScreen: screen.IsAlternateScreen,
		},
		Scrollback: backlog,
		Cursor:     protocolCursorFromVTerm(vt.CursorState()),
		Modes:      protocolModesFromVTerm(vt.Modes()),
		Timestamp:  time.Now(),
	}
}

func protocolCellFromVTermCell(cell localvterm.Cell) protocol.Cell {
	return protocol.Cell{
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

func protocolRowsToVTerm(rows [][]protocol.Cell) [][]localvterm.Cell {
	out := make([][]localvterm.Cell, len(rows))
	for y, row := range rows {
		out[y] = make([]localvterm.Cell, len(row))
		for x, cell := range row {
			out[y][x] = localvterm.Cell{
				Content: cell.Content,
				Width:   cell.Width,
				Style: localvterm.CellStyle{
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
	}
	return out
}

func protocolScreenToVTerm(screen protocol.ScreenData) localvterm.ScreenData {
	return localvterm.ScreenData{
		Cells:             protocolRowsToVTerm(screen.Cells),
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func protocolCursorToVTerm(cursor protocol.CursorState) localvterm.CursorState {
	return localvterm.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   localvterm.CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesToVTerm(modes protocol.TerminalModes) localvterm.TerminalModes {
	return localvterm.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func protocolCursorFromVTerm(cursor localvterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesFromVTerm(modes localvterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}
