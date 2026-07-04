package termxcorev2

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func historyTextCount(rows []history.HistoryRow, needle string) int {
	count := 0
	for _, row := range rows {
		if strings.Contains(historyCellsText(row.Cells), needle) {
			count++
		}
	}
	return count
}

func committedHistoryRowTexts(rows []history.HistoryRow) []string {
	var out []string
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCommitted {
			out = append(out, historyCellsText(row.Cells))
		}
	}
	return out
}

func r333NumberedMarker(label string, session int, lines int) string {
	return fmt.Sprintf("=== %s S%02d lines=%d clear=ed2 sync=1 ===", label, session, lines)
}

func r333NumberedLine(session int, line int, total int, cjkEvery int) string {
	text := fmt.Sprintf("S%02d %03d/%03d | seq=%03d", session, line, total, line)
	if cjkEvery > 0 && line%cjkEvery == 0 {
		text += fmt.Sprintf(" | 中文编号%03d中文", line)
	}
	return text
}

func r407OrdinaryStressPayload(lines int) (string, int) {
	var payload strings.Builder
	for line := 1; line <= lines; line++ {
		stressLine := r392OrdinaryStressLine(line)
		payload.WriteString(stressLine)
		payload.WriteString("\r\n")
	}
	return payload.String(), payload.Len()
}

func r392OrdinaryStressLine(line int) string {
	return fmt.Sprintf(
		"%06d [INFO  ] history  ingest    id=%06d lat=%03dms q=%d bytes=%d mode=raw cursor=%d:%d rev=%d %s",
		line,
		line,
		(line*37)%1000,
		(line*97)%8192,
		64+(line*211)%65535,
		(line*13)%220,
		(line*17)%120,
		1+(line*19)%4096,
		strings.Repeat("payload", 31),
	)
}
