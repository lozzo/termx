package state

import (
	"fmt"
	"strings"
)

const historyTracePreviewRows = 4

// HistoryTraceWindowSummary 返回 copy/history 诊断日志使用的窗口摘要。
// domain owner：TUI state；truth source 是 core authoritative HistoryWindow
// 投影后的本地状态。它只能用于日志定位错位层级，不能参与分页或渲染决策。
func HistoryTraceWindowSummary(rows []HistoryRow) string {
	if len(rows) == 0 {
		return ""
	}
	indexes := historyTraceSampleIndexes(len(rows), historyTracePreviewRows)
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, HistoryTraceRowSummary(index, rows[index]))
	}
	return strings.Join(parts, " || ")
}

// HistoryTraceRowSummary 返回单行的 source identity 和短文本。
// 它显式带上 ProjectionRowIndex/LineID/Segment/FrameID，便于对照 core dump、
// protocol response、TUI merge/trim 和 render-visible 四层是否发生错位。
func HistoryTraceRowSummary(index int, row HistoryRow) string {
	return fmt.Sprintf(
		"i=%d projection=%d line=%d row=%d segment=%s kind=%s session=%d frame=%d fixed=%v cols=%d clip=%v/%v text=%q",
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
		row.ClippedStart,
		row.ClippedEnd,
		historyTraceShortText(row.Text),
	)
}

func historyTraceSampleIndexes(total int, limit int) []int {
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

func historyTraceShortText(text string) string {
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\r", "\\r")
	runes := []rune(text)
	if len(runes) <= 96 {
		return text
	}
	return string(runes[:96]) + "..."
}
