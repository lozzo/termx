package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestR334PrimaryFrameStartDoesNotDuplicateAlreadySealedShellTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r334-shell-tail",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if err := server.IngestOutput(context.Background(), "term-r334-shell-tail", "shell prompt "+string(rune('0'+i))+"\r\n"); err != nil {
			t.Fatalf("ingest shell line %d: %v", i, err)
		}
	}
	if err := server.IngestOutput(context.Background(), "term-r334-shell-tail", "\x1b[?2026h\x1b[8;1Hcodex welcome\x1b[?2026l"); err != nil {
		t.Fatalf("ingest primary frame start: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r334-shell-tail", "\x1b[?2026h\x1b[9;1Hcodex prompt\x1b[?2026l"); err != nil {
		t.Fatalf("ingest primary frame incremental update: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r334-shell-tail", 80, 4)
	text := strings.Join(historyRowTexts(rows), "\n")
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("sealed shell tail must appear once, %q count=%d:\n%s\nrows=%#v", needle, got, text, rows)
		}
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("new synchronized UI payload should still publish current frame, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "codex welcome"); got != 1 {
		t.Fatalf("current frame payload should appear once, count=%d:\n%s\nrows=%#v", got, text, rows)
	}
	if got := historyTextCount(rows, "codex prompt"); got != 1 {
		t.Fatalf("incremental current frame payload should appear once, count=%d:\n%s\nrows=%#v", got, text, rows)
	}
}
