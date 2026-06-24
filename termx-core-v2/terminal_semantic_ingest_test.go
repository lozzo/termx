package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestTerminalSemanticIngestUsesSharedVTermBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	t.Cleanup(resetTerminalSemanticIngestTestHooks)
	var batches int
	terminalSemanticIngestBatchHook = func(batch terminalSemanticBatch) {
		batches++
		if batch.Cols != 12 || batch.Rows != 2 {
			t.Fatalf("semantic ingest must use real PTY size, got %dx%d", batch.Cols, batch.Rows)
		}
		if !batch.FromSharedVTerm {
			t.Fatal("history ingest must consume the live vterm semantic batch")
		}
	}

	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\r\ntwo\r\nthree"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if batches == 0 {
		t.Fatal("expected history ingest to receive at least one vterm semantic batch")
	}

	window, err := server.LatestWindow("term-1", 12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsAll(window, "one", "two", "three") {
		t.Fatalf("semantic ingest should preserve primary output, got %#v", window.Rows)
	}
}

func TestTerminalSemanticIngestRowsOneTwoPrimaryOutputDoesNotDuplicate(t *testing.T) {
	for _, rows := range []uint16{1, 2} {
		t.Run("rows_"+string(rune('0'+rows)), func(t *testing.T) {
			server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
			if _, err := server.RegisterTerminal(TerminalRecord{
				ID:      "term-1",
				Command: []string{"shell"},
				Size:    Size{Cols: 16, Rows: rows},
			}); err != nil {
				t.Fatalf("register terminal: %v", err)
			}
			if err := server.IngestOutput(context.Background(), "term-1", "alpha\r\nbeta\r\ngamma"); err != nil {
				t.Fatalf("ingest output: %v", err)
			}
			window, err := server.LatestWindow("term-1", 16, 8)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			text := historyWindowJoinedText(window)
			for _, want := range []string{"alpha", "beta", "gamma"} {
				if strings.Count(text, want) != 1 {
					t.Fatalf("rows=%d should contain %q once, got %q rows=%#v", rows, want, text, window.Rows)
				}
			}
		})
	}
}

func historyWindowContainsAll(window history.HistoryWindow, wants ...string) bool {
	for _, want := range wants {
		if !historyWindowContainsText(window, want) {
			return false
		}
	}
	return true
}

func historyWindowJoinedText(window history.HistoryWindow) string {
	var builder strings.Builder
	for _, row := range window.Rows {
		builder.WriteString(row.Text)
		builder.WriteByte('\n')
	}
	return builder.String()
}
