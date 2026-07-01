package termxcorev2

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

const coreHistoryTracePreviewRows = 4

func coreHistoryWindowSummary(rows []history.HistoryRow) string {
	if len(rows) == 0 {
		return ""
	}
	indexes := coreHistoryTraceSampleIndexes(len(rows), coreHistoryTracePreviewRows)
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, coreHistoryRowSummary(index, rows[index]))
	}
	return strings.Join(parts, " || ")
}

func coreHistoryRowSummary(index int, row history.HistoryRow) string {
	text := coreHistoryRowText(row)
	return fmt.Sprintf(
		"i=%d projection=%d line=%d row=%d segment=%s kind=%s session=%d frame=%d fixed=%v cols=%d cells=%d width=%d trail=%d text=%q",
		index,
		row.ProjectionRowIndex,
		row.LineID,
		row.RowInLine,
		row.Segment,
		row.Kind,
		row.SessionID,
		row.FrameID,
		row.FixedGrid,
		row.ScreenCols,
		len(row.Cells),
		coreHistoryCellsDisplayWidth(row.Cells),
		coreHistoryTrailingSpaces(text),
		coreHistoryTraceShortText(text),
	)
}

// 中文说明：history trace 只观察 vterm semantic frame 进入 core history 前的
// fixed-grid 形态，用来定位空格是在 semantic source、core store 还是 TUI
// projection/render 层丢失；它不能参与 history truth 或渲染决策。
func coreHistoryFrameSummary(frame *history.TerminalSemanticFrame) string {
	if frame == nil || len(frame.Rows) == 0 {
		return ""
	}
	indexes := coreHistoryTraceSampleIndexes(len(frame.Rows), coreHistoryTracePreviewRows)
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, coreHistorySemanticFrameRowSummary(index, frame.Cols, frame.Rows[index]))
	}
	return strings.Join(parts, " || ")
}

func coreHistoryFrameRowCount(frame *history.TerminalSemanticFrame) int {
	if frame == nil {
		return 0
	}
	return len(frame.Rows)
}

func coreHistorySemanticFrameRowSummary(index int, cols int, cells []vterm.TerminalSemanticCell) string {
	text := coreHistorySemanticCellsText(cells)
	return fmt.Sprintf(
		"i=%d cols=%d cells=%d width=%d trail=%d text=%q",
		index,
		cols,
		len(cells),
		coreHistorySemanticCellsDisplayWidth(cells),
		coreHistoryTrailingSpaces(text),
		coreHistoryTraceShortText(text),
	)
}

func coreHistoryCursorAttrs(prefix string, cursor history.HistoryCursor) []any {
	return []any{
		prefix + "_cursor_valid", cursor.Valid,
		prefix + "_cursor_line", uint64(cursor.LineID),
		prefix + "_cursor_row", cursor.RowInLine,
		prefix + "_cursor_index", cursor.BeforeRowIndex,
		prefix + "_cursor_segment", string(cursor.Segment),
		prefix + "_cursor_session", uint64(cursor.SessionID),
		prefix + "_cursor_frame", uint64(cursor.FrameID),
	}
}

func coreHistoryBoundaryAttrs(prefix string, boundary history.HistoryBoundary) []any {
	return []any{
		prefix + "_boundary_first", uint64(boundary.FirstLineID),
		prefix + "_boundary_last", uint64(boundary.LastLineID),
	}
}

func coreHistoryBacklogAttrs(prefix string, status HistoryBacklogStatus) []any {
	return []any{
		prefix + "_history_enabled", status.HistoryEnabled,
		prefix + "_applied_history_seq", status.AppliedSeq,
		prefix + "_target_history_seq", status.TargetSeq,
		prefix + "_catchup_pending", status.CatchupPending,
		prefix + "_pending_transactions", status.PendingTransactions,
		prefix + "_history_in_flight", status.InFlight,
	}
}

func coreHistoryTraceSampleIndexes(total int, limit int) []int {
	if total <= 0 {
		return nil
	}
	if limit <= 0 || total <= limit*2 {
		out := make([]int, total)
		for index := range out {
			out[index] = index
		}
		return out
	}
	out := make([]int, 0, limit*2)
	for index := 0; index < limit; index++ {
		out = append(out, index)
	}
	for index := total - limit; index < total; index++ {
		out = append(out, index)
	}
	return out
}

func coreHistoryRowText(row history.HistoryRow) string {
	var builder strings.Builder
	for _, cell := range row.Cells {
		builder.WriteString(cell.Text)
	}
	return builder.String()
}

// 中文说明：semantic cell 的 Content 为空但 Width 为正时代表默认空白格，
// 这里必须按空格展开，才能在 trace 中看出 fixed-grid 行尾 blank 是否已进入 core。
func coreHistorySemanticCellsText(cells []vterm.TerminalSemanticCell) string {
	var builder strings.Builder
	for _, cell := range cells {
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		if cell.Content == "" {
			width := cell.Width
			if width < 1 {
				width = 1
			}
			builder.WriteString(strings.Repeat(" ", width))
			continue
		}
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func coreHistoryCellsDisplayWidth(cells []history.Cell) int {
	width := 0
	for _, cell := range cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		width += len([]rune(cell.Text))
	}
	return width
}

func coreHistorySemanticCellsDisplayWidth(cells []vterm.TerminalSemanticCell) int {
	width := 0
	for _, cell := range cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		if cell.Content != "" {
			width += len([]rune(cell.Content))
		}
	}
	return width
}

func coreHistoryTrailingSpaces(text string) int {
	count := 0
	for len(text) > 0 {
		r, size := utf8.DecodeLastRuneInString(text)
		if r != ' ' {
			break
		}
		count++
		text = text[:len(text)-size]
	}
	return count
}

func coreHistoryTraceShortText(text string) string {
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\r", "\\r")
	runes := []rune(text)
	if len(runes) <= 96 {
		return text
	}
	return string(runes[:96]) + "..."
}
