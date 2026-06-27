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
	if err := server.IngestOutput(context.Background(), "term-r328-ed2", "old-a\r\nold-b\r\n"); err != nil {
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

func TestR333ED2ClearScreenDoesNotDuplicateAlreadySealedOrdinaryLines(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r333-ed2-dedupe",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-ed2-dedupe", "older one\r\nvisible tail\r\n"); err != nil {
		t.Fatalf("seed ordinary lines: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-ed2-dedupe", "\x1b[?2026h\x1b[2J\x1b[Hcodex current\x1b[?2026l"); err != nil {
		t.Fatalf("ingest codex-style redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r333-ed2-dedupe", 24, 1)
	committed := strings.Join(committedHistoryRowTexts(rows), "|")
	if committed != "older one|visible tail" {
		t.Fatalf("ED2 clear-time proof must not duplicate already sealed ordinary lines, committed=%q rows=%#v", committed, rows)
	}
	current := strings.Join(currentPrimaryFrameRowTexts(rows), "|")
	if current != "codex current" {
		t.Fatalf("codex redraw should remain current frame, current=%q rows=%#v", current, rows)
	}
}

func TestR333SynchronizedBeginAloneDoesNotPublishExistingShellScreenAsFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r333-sync-begin",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-sync-begin", "older one\r\nvisible tail\r\n"); err != nil {
		t.Fatalf("seed ordinary lines: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-sync-begin", "\x1b[?2026h"); err != nil {
		t.Fatalf("ingest synchronized begin: %v", err)
	}

	window, err := server.TerminalHistoryWindow(context.Background(), "term-r333-sync-begin", history.HistoryWindowRequest{
		TerminalID: "term-r333-sync-begin",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       24,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("synchronized begin without repaint payload must not publish existing shell screen as current frame, rows=%#v", window.Rows)
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
