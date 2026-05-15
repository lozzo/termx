package app

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	tuiruntime "github.com/lozzow/termx/tuiv2/runtime"
)

func debugPendingReadies(readies []tuiruntime.PendingStreamReady) string {
	if len(readies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(readies))
	for _, ready := range readies {
		parts = append(parts, fmt.Sprintf("%s:%d/%d", ready.TerminalID, ready.Channel, ready.ScreenSequence))
	}
	return strings.Join(parts, ",")
}

func (m *Model) debugRuntimeSurfaceSummary() string {
	if m == nil || m.runtime == nil {
		return ""
	}
	visible := m.runtime.Visible()
	if visible == nil || len(visible.Terminals) == 0 {
		return ""
	}
	parts := make([]string, 0, len(visible.Terminals))
	for _, terminal := range visible.Terminals {
		cursor := ""
		tail := ""
		if terminal.Surface != nil {
			c := terminal.Surface.Cursor()
			cursor = fmt.Sprintf("%d,%d", c.Row, c.Col)
			tail = debugProtocolTailFromSurface(terminal.Surface, 4)
		} else if terminal.Snapshot != nil {
			c := terminal.Snapshot.Cursor
			cursor = fmt.Sprintf("%d,%d", c.Row, c.Col)
			tail = debugProtocolTailRows(terminal.Snapshot.Screen.Cells, 4)
		}
		parts = append(parts, fmt.Sprintf(
			"id=%s state=%s surface=%d snapshot=%d update=%d changed=%d cursor=%s tail=%q",
			terminal.TerminalID,
			terminal.State,
			terminal.SurfaceVersion,
			terminal.SnapshotVersion,
			terminal.ScreenUpdate.SurfaceVersion,
			len(terminal.ScreenUpdate.ChangedRows),
			cursor,
			tail,
		))
	}
	return strings.Join(parts, " | ")
}

func debugProtocolTailFromSurface(surface tuiruntime.TerminalSurface, limit int) string {
	if surface == nil || limit <= 0 {
		return ""
	}
	screenRows := surface.ScreenRows()
	if screenRows <= 0 {
		return ""
	}
	scrollbackRows := surface.ScrollbackRows()
	start := screenRows - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, screenRows-start)
	for row := start; row < screenRows; row++ {
		out = append(out, debugProtocolRowString(surface.Row(scrollbackRows+row)))
	}
	return strings.Join(out, "\\n")
}

func debugProtocolTailRows(rows [][]protocol.Cell, limit int) string {
	if len(rows) == 0 || limit <= 0 {
		return ""
	}
	start := len(rows) - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(rows)-start)
	for _, row := range rows[start:] {
		out = append(out, debugProtocolRowString(row))
	}
	return strings.Join(out, "\\n")
}

func debugProtocolRowString(row []protocol.Cell) string {
	if len(row) == 0 {
		return ""
	}
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}
