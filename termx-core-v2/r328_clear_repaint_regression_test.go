package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestR328ED3ClearScrollbackDropsOldAuthoritativeHistoryBeforeRedraw(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r328-ed3",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r328-ed3", "old-a\r\nold-b\r\n"); err != nil {
		t.Fatalf("seed old history: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r328-ed3", "\x1b[3Jnew-a\r\nnew-b"); err != nil {
		t.Fatalf("ingest ED3 redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r328-ed3", 20, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	if strings.Contains(text, "old-a") || strings.Contains(text, "old-b") {
		t.Fatalf("ED3 clear-scrollback boundary must drop old authoritative history, got:\n%s\nrows=%#v", text, rows)
	}
	if !strings.Contains(text, "new-a") || !strings.Contains(text, "new-b") {
		t.Fatalf("redraw after ED3 must remain visible after clear boundary, got:\n%s\nrows=%#v", text, rows)
	}
}

func TestR328ED2ClearScreenPreservesClearedScreenAsHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r328-ed2",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r328-ed2", "old-a\r\nold-b"); err != nil {
		t.Fatalf("seed old visible screen: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r328-ed2", "\x1b[H\x1b[2Jnew-a\r\nnew-b"); err != nil {
		t.Fatalf("ingest ED2 redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r328-ed2", 20, 2)
	gotCommitted := committedHistoryRowTexts(rows)
	if got := strings.Join(gotCommitted, "|"); got != "old-a|old-b" {
		t.Fatalf("ED2 should preserve cleared screen before redraw without reordering or duplication, committed=%v rows=%#v", gotCommitted, rows)
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("redraw after ED2 should publish current primary frame, rows=%#v", rows)
	}
	gotCurrent := currentPrimaryFrameRowTexts(rows)
	if got := strings.Join(gotCurrent, "|"); got != "new-a|new-b" {
		t.Fatalf("redraw after ED2 should replace current primary frame, current=%v rows=%#v", gotCurrent, rows)
	}
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
