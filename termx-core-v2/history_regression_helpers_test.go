package termxcorev2

import (
	"context"
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
