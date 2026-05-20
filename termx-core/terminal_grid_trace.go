package termx

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/gridtrace"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

type traceRowSummary struct {
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

func traceGridDamageOps(event, terminalID string, rows []vterm.DamageOp, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		if !traceGridShouldLogRow(i, len(rows), summaryFromDamageOp(row)) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i, "wrapped", row.WrappedSet && row.Wrapped, "row_kind", row.RowKind, "runs", len(row.Runs)}, kv...)
		traceGridLogSummary(event+".row", summaryFromDamageOp(row), args...)
	}
}

func traceGridTerminalRows(event, terminalID string, rows []terminalGridRow, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := summaryFromTerminalGridRow(row)
		if !traceGridShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i, "wrapped", row.wrapped, "row_kind", row.rowKind, "runs", len(row.runs)}, kv...)
		traceGridLogSummary(event+".row", summary, args...)
	}
}

func traceGridVTermRows(event, terminalID string, rows [][]vterm.Cell, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := summaryFromVTermCells(row)
		if !traceGridShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i}, kv...)
		traceGridLogSummary(event+".row", summary, args...)
	}
}

func traceGridCoreRows(event, terminalID string, rows [][]Cell, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := summaryFromCoreCells(row)
		if !traceGridShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i}, kv...)
		traceGridLogSummary(event+".row", summary, args...)
	}
}

func traceGridProtocolRows(event, terminalID string, rows []protocol.CompactRow, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	base := append([]any{"terminal_id", terminalID, "rows", len(rows)}, kv...)
	gridtrace.Log(event+".batch", base...)
	for i, row := range rows {
		summary := summaryFromProtocolCells(row.DecodeCells())
		if !traceGridShouldLogRow(i, len(rows), summary) {
			continue
		}
		args := append([]any{"terminal_id", terminalID, "row", i, "compact_text_bytes", len(row.Text), "compact_runs", len(row.Runs), "compact_cells", len(row.Cells)}, kv...)
		traceGridLogSummary(event+".row", summary, args...)
	}
}

func traceGridLogSummary(event string, summary traceRowSummary, kv ...any) {
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

func traceGridTrimScreenOverlap(terminalID string, before, after, total int) {
	if !gridtrace.Enabled() {
		return
	}
	gridtrace.Log("core.snapshot.trim_screen_overlap", "terminal_id", terminalID, "before", before, "after", after, "trimmed", before-after, "total", total)
}

func traceGridShouldLogRow(index, total int, summary traceRowSummary) bool {
	if index < 4 || index >= total-4 {
		return true
	}
	if summary.styled > 0 || summary.wide > 0 || summary.links > 0 || summary.trailingSpaces > 0 {
		return true
	}
	return traceTextLooksInteresting(summary.text)
}

func traceTextLooksInteresting(text string) bool {
	return strings.ContainsAny(text, "█▀▄▌▐") ||
		strings.Contains(text, "QR") ||
		strings.Contains(text, "TERMX") ||
		strings.Contains(text, "PROMPT") ||
		strings.Contains(text, "remote pair") ||
		strings.Contains(text, "000100") ||
		strings.Contains(text, "001000")
}

func summaryFromDamageOp(row vterm.DamageOp) traceRowSummary {
	if len(row.Cells) > 0 {
		return summaryFromVTermCells(row.Cells)
	}
	cells := make([]vterm.Cell, 0)
	for _, run := range row.Runs {
		cells = append(cells, vtermCellsFromRun(run)...)
	}
	return summaryFromVTermCells(cells)
}

func summaryFromTerminalGridRow(row terminalGridRow) traceRowSummary {
	if len(row.cells) > 0 {
		return summaryFromVTermCells(row.cells)
	}
	cells := make([]vterm.Cell, 0)
	for _, run := range row.runs {
		cells = append(cells, vtermCellsFromRun(run)...)
	}
	return summaryFromVTermCells(cells)
}

func vtermCellsFromRun(run vterm.CellRun) []vterm.Cell {
	if run.Text == "" {
		return nil
	}
	cells := make([]vterm.Cell, 0, len(run.Text))
	for _, r := range run.Text {
		content := string(r)
		width := 1
		if r == '\t' {
			content = "\t"
		}
		cells = append(cells, vterm.Cell{Content: content, Width: width, Style: run.Style})
	}
	return cells
}

func summaryFromVTermCells(row []vterm.Cell) traceRowSummary {
	var b strings.Builder
	summary := traceRowSummary{cells: len(row)}
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
		if cell.Style != (vterm.CellStyle{}) {
			summary.styled++
			styleSeen[traceVTermStyle(cell.Style)] = true
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			summary.links++
		}
	}
	summary.text = gridtrace.Short(b.String(), 220)
	summary.trailingSpaces = trailingSpaces(b.String())
	summary.styles = joinStyleKeys(styleSeen)
	summary.hash = traceHash(b.String())
	return summary
}

func summaryFromCoreCells(row []Cell) traceRowSummary {
	var b strings.Builder
	summary := traceRowSummary{cells: len(row)}
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
		if cell.Style != (CellStyle{}) {
			summary.styled++
			styleSeen[traceCoreStyle(cell.Style)] = true
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			summary.links++
		}
	}
	summary.text = gridtrace.Short(b.String(), 220)
	summary.trailingSpaces = trailingSpaces(b.String())
	summary.styles = joinStyleKeys(styleSeen)
	summary.hash = traceHash(b.String())
	return summary
}

func summaryFromProtocolCells(row []protocol.Cell) traceRowSummary {
	var b strings.Builder
	summary := traceRowSummary{cells: len(row)}
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
			styleSeen[traceProtocolStyle(cell.Style)] = true
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			summary.links++
		}
	}
	summary.text = gridtrace.Short(b.String(), 220)
	summary.trailingSpaces = trailingSpaces(b.String())
	summary.styles = joinStyleKeys(styleSeen)
	summary.hash = traceHash(b.String())
	return summary
}

func traceVTermStyle(style vterm.CellStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func traceCoreStyle(style CellStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func traceProtocolStyle(style protocol.CellStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func joinStyleKeys(values map[string]bool) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return gridtrace.Short(strings.Join(out, "|"), 220)
}

func trailingSpaces(text string) int {
	count := 0
	for i := len(text); i > 0; {
		r, size := lastRune(text[:i])
		if r != ' ' {
			break
		}
		count++
		i -= size
	}
	return count
}

func lastRune(text string) (rune, int) {
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

func traceHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%016x", h.Sum64())
}
