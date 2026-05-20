package termx

import (
	"time"

	"github.com/lozzow/termx/termx-vterm/vterm"
)

type terminalAlternateGrid struct {
	rows       []terminalGridRow
	totalRows  int
	droppedOld int
}

func (g *terminalAlternateGrid) reset() {
	if g == nil {
		return
	}
	g.rows = nil
	g.totalRows = 0
	g.droppedOld = 0
}

func (g *terminalAlternateGrid) appendDamageRows(rows []vterm.DamageOp) {
	if g == nil || len(rows) == 0 {
		return
	}
	for _, row := range rows {
		g.rows = append(g.rows, terminalGridRow{
			cells:     row.Cells,
			runs:      row.Runs,
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		})
	}
	g.totalRows += len(rows)
	if overflow := len(g.rows) - terminalAlternateScrollbackRows; overflow > 0 {
		copy(g.rows, g.rows[overflow:])
		clear(g.rows[len(g.rows)-overflow:])
		g.rows = g.rows[:len(g.rows)-overflow]
		g.droppedOld += overflow
	}
}

func (g *terminalAlternateGrid) viewport(beforeOffset, limit int) ([][]Cell, []time.Time, []string, []bool, int, bool) {
	rows, hasMore := g.window(beforeOffset, limit)
	if len(rows) == 0 {
		return nil, nil, nil, nil, g.total(), hasMore
	}
	out := make([][]Cell, 0, len(rows))
	timestamps := make([]time.Time, 0, len(rows))
	rowKinds := make([]string, 0, len(rows))
	wrapped := make([]bool, 0, len(rows))
	for _, row := range rows {
		out = append(out, convertRows([][]vterm.Cell{row.cells})[0])
		timestamps = append(timestamps, row.timestamp)
		rowKinds = append(rowKinds, row.rowKind)
		wrapped = append(wrapped, row.wrapped)
	}
	return out, timestamps, rowKinds, wrapped, g.total(), hasMore
}

func (g *terminalAlternateGrid) replay(beforeOffset, limit int) ([]byte, int, bool) {
	rows, hasMore := g.window(beforeOffset, limit)
	if len(rows) == 0 {
		return nil, 0, hasMore
	}
	return encodeGridRowsReplay(rows), len(rows), hasMore
}

func (g *terminalAlternateGrid) window(beforeOffset, limit int) ([]terminalGridRow, bool) {
	if g == nil || len(g.rows) == 0 {
		return nil, false
	}
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = defaultGridHistoryPageRows
	}
	if limit > maxGridReplayRows {
		limit = maxGridReplayRows
	}
	stored := len(g.rows)
	if beforeOffset > stored {
		beforeOffset = stored
	}
	end := stored - beforeOffset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	for start > 0 && g.rows[start].wrapped {
		start--
	}
	hasMore := start > 0 || g.droppedOld > 0
	if start >= end {
		return nil, hasMore
	}
	out := make([]terminalGridRow, end-start)
	copy(out, g.rows[start:end])
	return out, hasMore
}

func (g *terminalAlternateGrid) total() int {
	if g == nil {
		return 0
	}
	return g.totalRows
}
