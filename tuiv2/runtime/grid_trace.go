package runtime

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/gridtrace"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type runtimeTraceRowSummary struct {
	text           string
	cells          int
	cols           int
	styled         int
	wide           int
	links          int
	trailingSpaces int
	styles         string
	hash           string
}

func traceRuntimeSnapshot(event string, snap *protocol.Snapshot, kv ...any) {
	if !gridtrace.Enabled() || snap == nil {
		return
	}
	base := append([]any{
		"terminal_id", snap.TerminalID,
		"cols", snap.Size.Cols,
		"rows", snap.Size.Rows,
		"scrollback_rows", len(snap.Scrollback),
		"screen_rows", len(snap.Screen.Cells),
		"offset", snap.ScrollbackOffset,
		"total", snap.ScrollbackTotal,
		"logical_total", snap.ScrollbackLogicalTotal,
		"has_more", snap.ScrollbackHasMore,
		"loaded_rows", snap.ScrollbackLoadedRows,
		"generation", snap.HistoryGeneration,
		"first_row_id", snap.ScrollbackFirstRowID,
		"last_row_id", snap.ScrollbackLastRowID,
	}, kv...)
	gridtrace.Log(event+".snapshot", base...)
	traceRuntimeCompactRows(event+".scrollback", snap.TerminalID, snap.Scrollback, kv...)
	traceRuntimeProtocolCellRows(event+".screen", snap.TerminalID, snap.Screen.Cells, kv...)
}

func traceRuntimeGridViewport(event, terminalID string, viewport *protocol.GridViewport, kv ...any) {
	if !gridtrace.Enabled() || viewport == nil {
		return
	}
	base := append([]any{
		"terminal_id", terminalID,
		"cols", viewport.Size.Cols,
		"rows", viewport.Size.Rows,
		"scrollback_rows", len(viewport.Rows),
		"offset", viewport.ScrollbackOffset,
		"limit", viewport.ScrollbackLimit,
		"total", viewport.ScrollbackTotal,
		"logical_total", viewport.ScrollbackLogicalTotal,
		"has_more", viewport.ScrollbackHasMore,
		"loaded_rows", viewport.LoadedRows,
		"generation", viewport.HistoryGeneration,
		"first_row_id", viewport.FirstRowID,
		"last_row_id", viewport.LastRowID,
	}, kv...)
	gridtrace.Log(event+".viewport", base...)
	traceRuntimeCompactRows(event+".rows", terminalID, viewport.Rows, kv...)
}

func traceRuntimeCompactRows(event, terminalID string, rows []protocol.CompactRow, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := runtimeSummaryFromProtocolCells(row.DecodeCells())
		if !runtimeTraceShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i, "compact_text_bytes", len(row.Text), "compact_runs", len(row.Runs), "compact_cells", len(row.Cells)}, kv...)
		traceRuntimeLogSummary(event+".row", summary, args...)
	}
}

func traceRuntimeProtocolCellRows(event, terminalID string, rows [][]protocol.Cell, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := runtimeSummaryFromProtocolCells(row)
		if !runtimeTraceShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i}, kv...)
		traceRuntimeLogSummary(event+".row", summary, args...)
	}
}

func traceRuntimeVTermRows(event, terminalID string, rows [][]localvterm.Cell, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := runtimeSummaryFromVTermCells(row)
		if !runtimeTraceShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i}, kv...)
		traceRuntimeLogSummary(event+".row", summary, args...)
	}
}

func traceRuntimeLogSummary(event string, summary runtimeTraceRowSummary, kv ...any) {
	args := append(kv,
		"cells", summary.cells,
		"cols", summary.cols,
		"styled", summary.styled,
		"wide", summary.wide,
		"links", summary.links,
		"trailing_spaces", summary.trailingSpaces,
		"styles", summary.styles,
		"hash", summary.hash,
		"text", summary.text,
	)
	gridtrace.Log(event, args...)
}

func runtimeTraceShouldLogRow(index, total int, summary runtimeTraceRowSummary) bool {
	if index < 4 || index >= total-4 {
		return true
	}
	if summary.styled > 0 || summary.wide > 0 || summary.links > 0 || summary.trailingSpaces > 0 {
		return true
	}
	return runtimeTraceTextLooksInteresting(summary.text)
}

func runtimeTraceTextLooksInteresting(text string) bool {
	return strings.ContainsAny(text, "█▀▄▌▐") ||
		strings.Contains(text, "QR") ||
		strings.Contains(text, "TERMX") ||
		strings.Contains(text, "PROMPT") ||
		strings.Contains(text, "remote pair") ||
		strings.Contains(text, "000100") ||
		strings.Contains(text, "001000")
}

func runtimeSummaryFromProtocolCells(row []protocol.Cell) runtimeTraceRowSummary {
	var b strings.Builder
	summary := runtimeTraceRowSummary{cells: len(row)}
	styleSeen := map[string]bool{}
	for _, cell := range row {
		width := cell.Width
		if width <= 0 {
			width = 1
		}
		if width > 1 {
			summary.wide++
		}
		summary.cols += width
		content := cell.Content
		if content == "" {
			content = " "
		}
		b.WriteString(content)
		if cell.Style != (protocol.CellStyle{}) {
			summary.styled++
			styleSeen[runtimeProtocolStyle(cell.Style)] = true
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			summary.links++
		}
	}
	text := b.String()
	summary.text = gridtrace.Short(text, 220)
	summary.trailingSpaces = runtimeTrailingSpaces(text)
	summary.styles = runtimeJoinStyleKeys(styleSeen)
	summary.hash = runtimeTraceHash(text)
	return summary
}

func runtimeSummaryFromVTermCells(row []localvterm.Cell) runtimeTraceRowSummary {
	var b strings.Builder
	summary := runtimeTraceRowSummary{cells: len(row)}
	styleSeen := map[string]bool{}
	for _, cell := range row {
		width := cell.Width
		if width <= 0 {
			continue
		}
		if width > 1 {
			summary.wide++
		}
		summary.cols += width
		content := cell.Content
		if content == "" {
			content = " "
		}
		b.WriteString(content)
		if cell.Style != (localvterm.CellStyle{}) {
			summary.styled++
			styleSeen[runtimeVTermStyle(cell.Style)] = true
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			summary.links++
		}
	}
	text := b.String()
	summary.text = gridtrace.Short(text, 220)
	summary.trailingSpaces = runtimeTrailingSpaces(text)
	summary.styles = runtimeJoinStyleKeys(styleSeen)
	summary.hash = runtimeTraceHash(text)
	return summary
}

func runtimeProtocolStyle(style protocol.CellStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func runtimeVTermStyle(style localvterm.CellStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func runtimeJoinStyleKeys(values map[string]bool) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return gridtrace.Short(strings.Join(out, "|"), 220)
}

func runtimeTrailingSpaces(text string) int {
	count := 0
	for i := len(text); i > 0; {
		r, size := runtimeLastRune(text[:i])
		if r != ' ' {
			break
		}
		count++
		i -= size
	}
	return count
}

func runtimeLastRune(text string) (rune, int) {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] < 0x80 || text[i]&0xc0 == 0xc0 {
			r := []rune(text[i:])
			if len(r) == 0 {
				return 0, 1
			}
			return r[0], len(text) - i
		}
	}
	return 0, 1
}

func runtimeTraceHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%016x", h.Sum64())
}
