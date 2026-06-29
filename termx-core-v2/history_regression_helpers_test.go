package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func waitForTerminalState(t *testing.T, server *Server, terminalID string, want TerminalState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last TerminalInfo
	for time.Now().Before(deadline) {
		info, err := server.GetTerminal(terminalID)
		if err == nil {
			last = info
			if info.State == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal %q state %q, got %#v", terminalID, want, last)
}

func waitForLiveRow(t *testing.T, server *Server, terminalID string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := server.LiveRows(terminalID)
		if err == nil {
			for _, row := range rows {
				if strings.Contains(row, want) {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, _ := server.LiveRows(terminalID)
	t.Fatalf("timed out waiting for live row %q, got %#v", want, rows)
}

func historyRowTexts(rows []history.HistoryRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, historyCellsText(row.Cells))
	}
	return texts
}

func historyRowsContainSegment(rows []history.HistoryRow, segment history.HistorySegment) bool {
	for _, row := range rows {
		if row.Segment == segment {
			return true
		}
	}
	return false
}

func historyCellsText(cells []history.Cell) string {
	var out strings.Builder
	for _, cell := range cells {
		out.WriteString(cell.Text)
	}
	return out.String()
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

func currentPrimaryFrameRowTexts(rows []history.HistoryRow) []string {
	var out []string
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame {
			out = append(out, historyCellsText(row.Cells))
		}
	}
	return out
}

func historyTextCount(rows []history.HistoryRow, needle string) int {
	count := 0
	for _, row := range rows {
		if strings.Contains(historyCellsText(row.Cells), needle) {
			count++
		}
	}
	return count
}

func historyTextIndex(rows []string, needle string) int {
	for index, row := range rows {
		if strings.Contains(row, needle) {
			return index
		}
	}
	return -1
}

func r326CollectAllHistoryRows(t *testing.T, server *Server, terminalID string, cols int, limit int) ([]history.HistoryRow, int) {
	t.Helper()
	latest, err := server.TerminalHistoryWindow(context.Background(), terminalID, history.HistoryWindowRequest{
		TerminalID: terminalID,
		Mode:       history.HistoryWindowModeLatest,
		Cols:       cols,
		Limit:      limit,
	})
	if err != nil {
		t.Fatalf("latest history window: %v", err)
	}
	rows := append([]history.HistoryRow(nil), latest.Rows...)
	pageCount := 1
	cursor := latest.Boundary.Cursor
	for cursor.Valid {
		older, err := server.TerminalHistoryWindow(context.Background(), terminalID, history.HistoryWindowRequest{
			TerminalID: terminalID,
			Mode:       history.HistoryWindowModeOlder,
			Cols:       cols,
			Limit:      limit,
			Cursor:     cursor,
		})
		if err != nil {
			t.Fatalf("older history window: %v", err)
		}
		if len(older.Rows) == 0 {
			break
		}
		pageCount++
		rows = append(append([]history.HistoryRow(nil), older.Rows...), rows...)
		cursor = older.Boundary.Cursor
	}
	return rows, pageCount
}

func r326RowsContainKind(rows []history.HistoryRow, kind history.LineKind) bool {
	for _, row := range rows {
		if row.Kind == kind {
			return true
		}
	}
	return false
}

func r326RowsContainSegmentWithCols(rows []history.HistoryRow, segment history.HistorySegment, cols int) bool {
	for _, row := range rows {
		if row.Segment == segment && row.ScreenCols == cols {
			return true
		}
	}
	return false
}

func historyCellsForRegression(text string) []history.TerminalSemanticCell {
	cells := make([]history.TerminalSemanticCell, 0, len([]rune(text)))
	for _, r := range text {
		cells = append(cells, history.TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return cells
}

func r333NumberedStreamSession(session int, lines int, cjkEvery int) string {
	var builder strings.Builder
	builder.WriteString(r333NumberedMarker("STREAM_BEGIN", session, lines))
	builder.WriteString("\r\n")
	for line := 1; line <= lines; line++ {
		builder.WriteString(r333NumberedLine(session, line, lines, cjkEvery))
		builder.WriteString("\r\n")
	}
	builder.WriteString(r333NumberedMarker("STREAM_END", session, lines))
	builder.WriteString("\r\n")
	return builder.String()
}

func r333NumberedRedrawSession(session int, lines int, cjkEvery int) string {
	var builder strings.Builder
	builder.WriteString("\x1b[?2026h\x1b[2J\x1b[H")
	builder.WriteString(r333NumberedMarker("REDRAW_BEGIN", session, lines))
	builder.WriteString("\r\n")
	for line := 1; line <= lines; line++ {
		builder.WriteString(r333NumberedLine(session, line, lines, cjkEvery))
		builder.WriteString("\r\n")
	}
	builder.WriteString(r333NumberedMarker("REDRAW_END", session, lines))
	builder.WriteString("\r\n\x1b[?2026l")
	return builder.String()
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
