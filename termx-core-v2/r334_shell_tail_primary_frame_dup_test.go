package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestR415SyncFrameWithScrollOutDoesNotAdoptSealedShellRows(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	terminalID := "term-r415-sync-scrollout-shell-tail"
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      terminalID,
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if err := server.IngestOutput(context.Background(), terminalID, "shell prompt "+string(rune('0'+i))+"\r\n"); err != nil {
			t.Fatalf("ingest shell line %d: %v", i, err)
		}
	}
	terminal, err := server.Terminal(terminalID)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	tx := history.TerminalSemanticTransaction{
		Seq:                     415,
		Size:                    history.TerminalSemanticSize{Cols: 80, Rows: 12},
		SynchronizedBegin:       true,
		SynchronizedEnd:         true,
		PrimaryFrameTouchedRows: []int{7, 8},
		PrimaryScrollOut: []history.TerminalSemanticScrollOut{{
			Runs:   []history.TerminalSemanticCellRun{{Text: "shell prompt 1"}},
			Row:    0,
			RowSet: true,
		}},
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
	if !decision.PublishPrimaryFrameTouchedRowsOnly {
		terminal.historyMu.Unlock()
		t.Fatalf("sync primary frame with scroll-out and no clear must still publish touched rows only: %#v", decision)
	}
	journal := history.HistoryJournalFromDecision(terminalID, tx, decision)
	batch, err := terminal.journalRenderer.ApplyJournal(journal)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply scroll-out primary journal: %v labels=%v", err, historyJournalLabelsForTerminalTest(journal))
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store scroll-out primary journal: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, terminalID, 80, 4)
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("active current frame must not re-own sealed shell row %q count=%d rows=%#v", needle, got, rows)
		}
	}
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame && strings.Contains(historyCellsText(row.Cells), "shell prompt") {
			t.Fatalf("current primary frame must not contain sealed shell row: row=%#v rows=%#v", row, rows)
		}
	}
	if got := historyTextCount(rows, "codex welcome"); got != 1 {
		t.Fatalf("touched current frame payload should appear once while active, count=%d rows=%#v", got, rows)
	}

	process := factory.process(terminalID)
	if process == nil {
		t.Fatal("expected process")
	}
	process.exit(0)
	assertEventually(t, time.Second, func() bool {
		info, err := server.GetTerminal(terminalID)
		return err == nil && info.State == TerminalStateExited
	}, "terminal should exit")

	rows, _ = r326CollectAllHistoryRows(t, server, terminalID, 80, 4)
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("final screen-frame must not re-own sealed shell row %q count=%d rows=%#v", needle, got, rows)
		}
	}
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCommitted && row.Kind == history.LineKindScreenFrame && strings.Contains(historyCellsText(row.Cells), "shell prompt") {
			t.Fatalf("closed screen-frame must not contain sealed shell row: row=%#v rows=%#v", row, rows)
		}
	}
	if got := historyTextCount(rows, "codex prompt"); got != 1 {
		t.Fatalf("final touched screen-frame payload should appear once, count=%d rows=%#v", got, rows)
	}
}

func TestR415RealVTermSyncScrollOutDoesNotFrameSealedShellRows(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	terminalID := "term-r415-real-sync-scrollout"
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      terminalID,
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if err := server.IngestOutput(context.Background(), terminalID, "shell prompt "+string(rune('0'+i))+"\r\n"); err != nil {
			t.Fatalf("ingest shell line %d: %v", i, err)
		}
	}
	if err := server.IngestOutput(context.Background(), terminalID, "\x1b[?2026hcodex welcome\r\ncodex prompt\x1b[?2026l"); err != nil {
		t.Fatalf("ingest real sync frame: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, terminalID, 40, 3)
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("real vterm sync scroll-out must not duplicate sealed shell row %q count=%d rows=%#v", needle, got, rows)
		}
	}
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame && strings.Contains(historyCellsText(row.Cells), "shell prompt") {
			t.Fatalf("real vterm current frame must not contain sealed shell row: row=%#v rows=%#v", row, rows)
		}
	}
	if got := historyTextCount(rows, "codex welcome"); got != 1 {
		t.Fatalf("real vterm current frame payload should appear once, count=%d rows=%#v", got, rows)
	}
}

func TestR415RealVTermLongSyncAfterShellKeepsAppScrollOut(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	terminalID := "term-r415-long-sync-after-shell"
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      terminalID,
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if err := server.IngestOutput(context.Background(), terminalID, "shell prompt "+string(rune('0'+i))+"\r\n"); err != nil {
			t.Fatalf("ingest shell line %d: %v", i, err)
		}
	}
	var output strings.Builder
	output.WriteString("\x1b[?2026h")
	for i := 1; i <= 8; i++ {
		output.WriteString(fmt.Sprintf("codex line %02d\r\n", i))
	}
	output.WriteString("\x1b[?2026l")
	if err := server.IngestOutput(context.Background(), terminalID, output.String()); err != nil {
		t.Fatalf("ingest long real sync frame: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, terminalID, 40, 3)
	for i := 1; i <= 5; i++ {
		needle := "shell prompt " + string(rune('0'+i))
		if got := historyTextCount(rows, needle); got != 1 {
			t.Fatalf("long sync after shell must not duplicate sealed shell row %q count=%d rows=%#v", needle, got, rows)
		}
	}
	for _, want := range []string{"codex line 01", "codex line 08"} {
		if got := historyTextCount(rows, want); got != 1 {
			t.Fatalf("long sync after shell must keep app output %q once, count=%d rows=%#v", want, got, rows)
		}
	}
}
