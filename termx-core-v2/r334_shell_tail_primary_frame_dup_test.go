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

func TestR335FullReplacePrimaryFrameStartDoesNotDuplicateAlreadySealedShellTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r335-full-replace-shell-tail",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if err := server.IngestOutput(context.Background(), "term-r335-full-replace-shell-tail", "shell prompt "+string(rune('0'+i))+"\r\n"); err != nil {
			t.Fatalf("ingest shell line %d: %v", i, err)
		}
	}
	terminal, err := server.Terminal("term-r335-full-replace-shell-tail")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	tx := history.TerminalSemanticTransaction{
		Seq:                     90,
		Size:                    history.TerminalSemanticSize{Cols: 80, Rows: 12},
		RequiresFullReplace:     true,
		FullReplaceReason:       "broad_direct_cell_damage",
		PrimaryFrameTouchedRows: []int{7, 8},
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 80,
			Rows: [][]history.TerminalSemanticCell{
				historyCellsForRegression("shell prompt 1"),
				historyCellsForRegression("shell prompt 2"),
				historyCellsForRegression("shell prompt 3"),
				historyCellsForRegression("shell prompt 4"),
				historyCellsForRegression("shell prompt 5"),
				nil,
				nil,
				historyCellsForRegression("codex welcome"),
				historyCellsForRegression("codex prompt"),
			},
		},
	}

	terminal.historyMu.Lock()
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply full replace primary frame start: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store full replace primary frame start: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r335-full-replace-shell-tail", 80, 4)
	text := strings.Join(historyRowTexts(rows), "\n")
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("sealed shell tail must appear once after full-replace frame start, %q count=%d:\n%s\nrows=%#v", needle, got, text, rows)
		}
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("new full-replace UI payload should still publish current frame, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "codex welcome"); got != 1 {
		t.Fatalf("full-replace current frame payload should appear once, count=%d:\n%s\nrows=%#v", got, text, rows)
	}
	if got := historyTextCount(rows, "codex prompt"); got != 1 {
		t.Fatalf("full-replace current frame prompt should appear once, count=%d:\n%s\nrows=%#v", got, text, rows)
	}
}
