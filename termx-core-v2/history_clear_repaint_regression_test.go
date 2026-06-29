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

func TestR328ED3ClearScrollbackCreatesNewPageWithoutDroppingAuthoritativeHistory(t *testing.T) {
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
	for _, want := range []string{"old-a", "old-b", "new-a", "new-b"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ED3 clear-scrollback must create a new page without tearing old authoritative history, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	committed := strings.Join(committedHistoryRowTexts(rows), "\n")
	for _, want := range []string{"old-a", "old-b"} {
		if got := strings.Count(committed, want); got != 1 {
			t.Fatalf("ED3 must keep old page exactly once in authoritative history, %q count=%d:\n%s\nrows=%#v", want, got, committed, rows)
		}
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
		t.Fatalf("ingest redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r333-ed2-dedupe", 24, 1)
	committed := strings.Join(committedHistoryRowTexts(rows), "|")
	if committed != "older one|visible tail" {
		t.Fatalf("ED2 clear-time proof must not duplicate already sealed ordinary lines, committed=%q rows=%#v", committed, rows)
	}
	current := strings.Join(currentPrimaryFrameRowTexts(rows), "|")
	if current != "codex current" {
		t.Fatalf("redraw should remain current frame, current=%q rows=%#v", current, rows)
	}
}

func TestR332OrdinaryCJKOutputDoesNotInsertContinuationSpaces(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r332-cjk-ordinary",
		Command: []string{"shell"},
		Size:    Size{Cols: 120, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r332-cjk-ordinary", "S01 010/100 | seq=010 | 中文编号010中文\r\n"); err != nil {
		t.Fatalf("ingest ordinary cjk output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r332-cjk-ordinary", history.HistoryWindowRequest{
		TerminalID: "term-r332-cjk-ordinary",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       120,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "\n"); !strings.Contains(got, "中文编号010中文") {
		t.Fatalf("ordinary CJK output must not gain continuation spaces, got %q rows=%#v", got, window.Rows)
	}
}
