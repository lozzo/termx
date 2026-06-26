package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestR327ResumeStyleClearAndRedrawRebuildsAuthoritativeHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r327-resume",
		Command: []string{"shell"},
		Size:    Size{Cols: 18, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	resumeA := strings.Join([]string{
		"\x1b[?2026h",
		"session-a-01\r\n",
		"session-a-02\r\n",
		"session-a-03\r\n",
		"session-a-04",
		"\x1b[?2026l",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-r327-resume", resumeA); err != nil {
		t.Fatalf("ingest first resume redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r327-resume", 18, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"session-a-01", "session-a-02", "session-a-03", "session-a-04"} {
		if !strings.Contains(text, want) {
			t.Fatalf("first resume redraw should be visible in authoritative history, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("resume redraw should publish current primary frame, rows=%#v", rows)
	}
}

func TestR327SecondResumeKeepsRepeatedHistoryAsNewSessionEvents(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r327-second-resume",
		Command: []string{"shell"},
		Size:    Size{Cols: 14, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	redraw := "\x1b[?2026hrepeat\r\nrepeat\r\ncurrent\x1b[?2026l"
	if err := server.IngestOutput(context.Background(), "term-r327-second-resume", redraw); err != nil {
		t.Fatalf("ingest first redraw: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r327-second-resume", redraw); err != nil {
		t.Fatalf("ingest second redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r327-second-resume", 14, 2)
	if got := historyTextCount(rows, "repeat"); got < 4 {
		t.Fatalf("second resume with same visible text must record new scroll-out events, got count=%d text=%q rows=%#v", got, strings.Join(historyRowTexts(rows), "\n"), rows)
	}
	currentRows := 0
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame {
			currentRows++
		}
	}
	if currentRows == 0 || currentRows > 2 {
		t.Fatalf("second resume should leave only one bounded current primary frame, current_rows=%d rows=%#v", currentRows, rows)
	}
}

func TestR327FullReplacePrimaryFrameIsConsumedAsScreenRedraw(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r327-full-replace",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r327-full-replace")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}

	tx := history.TerminalSemanticTransaction{
		Seq:                 1,
		Size:                history.TerminalSemanticSize{Cols: 20, Rows: 3},
		RequiresFullReplace: true,
		FullReplaceReason:   "broad_direct_cell_damage",
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 20,
			Rows: [][]history.TerminalSemanticCell{
				{{Content: "r", Width: 1}, {Content: "e", Width: 1}, {Content: "d", Width: 1}, {Content: "r", Width: 1}, {Content: "a", Width: 1}, {Content: "w", Width: 1}},
			},
		},
	}
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		t.Fatalf("apply full replace redraw transaction: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		t.Fatalf("store full replace redraw batch: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r327-full-replace", history.HistoryWindowRequest{
		TerminalID: "term-r327-full-replace",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history window after full replace redraw: %v", err)
	}
	if got := historyRowTexts(window.Rows); strings.Join(got, "|") != "redraw" {
		t.Fatalf("non-resize full replace with primary frame should publish current frame, got %v window=%#v", got, window)
	}
	if window.Rows[0].Segment != history.HistorySegmentCurrentPrimaryFrame {
		t.Fatalf("full replace redraw row should stay mutable current frame, got %#v", window.Rows[0])
	}
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
