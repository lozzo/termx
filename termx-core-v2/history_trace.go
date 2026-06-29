package termxcorev2

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-core-v2/history"
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
	return fmt.Sprintf(
		"i=%d projection=%d line=%d row=%d segment=%s kind=%s session=%d frame=%d fixed=%v cols=%d text=%q",
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
		coreHistoryTraceShortText(coreHistoryRowText(row)),
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

func coreHistoryTraceShortText(text string) string {
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\r", "\\r")
	runes := []rune(text)
	if len(runes) <= 96 {
		return text
	}
	return string(runes[:96]) + "..."
}
