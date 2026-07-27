package core

import (
	"fmt"
	"strings"

	"github.com/anytty/anytty/core/history"
)

func historyTextCount(rows []history.HistoryRow, needle string) int {
	count := 0
	texts := rawHistoryRowTexts(rows)
	for index, text := range texts {
		if isLifecycleHistoryRowTextAt(texts, index) {
			continue
		}
		if strings.Contains(text, needle) {
			count++
		}
	}
	return count
}

func committedHistoryRowTexts(rows []history.HistoryRow) []string {
	var out []string
	texts := rawHistoryRowTexts(rows)
	for index, row := range rows {
		text := texts[index]
		if row.Segment == history.HistorySegmentCommitted && !isLifecycleHistoryRowTextAt(texts, index) {
			out = append(out, text)
		}
	}
	return out
}

func rawHistoryRowTexts(rows []history.HistoryRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, historyCellsText(row.Cells))
	}
	return texts
}

func historyLineIDForText(rows []history.HistoryRow, want string) (history.LogicalLineID, bool) {
	for _, row := range rows {
		if historyCellsText(row.Cells) == want {
			return row.LineID, true
		}
	}
	return 0, false
}

func isLifecycleHistoryText(text string) bool {
	return strings.HasPrefix(text, "terminal started: ") ||
		strings.HasPrefix(text, "started at: ") ||
		strings.HasPrefix(text, "terminal exited: ") ||
		strings.HasPrefix(text, "exited at: ") ||
		strings.HasPrefix(text, "command: ")
}

func isLifecycleHistoryRowTextAt(texts []string, index int) bool {
	if index < 0 || index >= len(texts) {
		return false
	}
	text := texts[index]
	if isLifecycleHistoryText(text) {
		return true
	}
	return text == "" && index+1 < len(texts) && strings.HasPrefix(texts[index+1], "terminal exited: ")
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
