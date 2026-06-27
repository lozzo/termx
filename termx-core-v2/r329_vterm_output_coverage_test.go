package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestR329ED2SmallPrimaryFrameRepaintDoesNotDuplicateHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r329-ed2-primary",
		Command: []string{"shell"},
		Size:    Size{Cols: 18, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	if err := server.IngestOutput(context.Background(), "term-r329-ed2-primary", "\x1b[?2026hframe-a\r\nframe-b\x1b[?2026l"); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r329-ed2-primary", "\x1b[H\x1b[2Jnext-a\r\nnext-b"); err != nil {
		t.Fatalf("ingest ED2 redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r329-ed2-primary", 18, 2)
	committed := strings.Join(committedHistoryRowTexts(rows), "|")
	if committed != "" {
		t.Fatalf("ED2 clear-time old primary frame must not duplicate transient UI history, committed=%q rows=%#v", committed, rows)
	}
	current := strings.Join(currentPrimaryFrameRowTexts(rows), "|")
	if current != "next-a|next-b" {
		t.Fatalf("ED2 redraw should publish new current primary frame, current=%q rows=%#v", current, rows)
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("expected current primary frame segment after ED2 redraw, rows=%#v", rows)
	}
}
