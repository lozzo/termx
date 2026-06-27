package termxcorev2

import (
	"context"
	"fmt"
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

func TestR333CodexNumberedResumeED2HistoryPagesAllSessions(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r333-numbered-resume",
		Command: []string{"shell"},
		Size:    Size{Cols: 64, Rows: 8},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	if err := server.IngestOutput(context.Background(), "term-r333-numbered-resume", r333NumberedStreamSession(1, 100, 10)); err != nil {
		t.Fatalf("ingest S01 stream: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-numbered-resume", r333NumberedRedrawSession(2, 100, 10)); err != nil {
		t.Fatalf("ingest S02 redraw: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-numbered-resume", r333NumberedRedrawSession(3, 100, 10)); err != nil {
		t.Fatalf("ingest S03 redraw: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-numbered-resume", "PROMPT_AFTER"); err != nil {
		t.Fatalf("ingest prompt: %v", err)
	}

	rows, pages := r326CollectAllHistoryRows(t, server, "term-r333-numbered-resume", 64, 17)
	texts := historyRowTexts(rows)
	joined := strings.Join(texts, "\n")
	for _, needle := range []string{
		"S01 001/100 | seq=001",
		"S01 100/100 | seq=100 | 中文编号100中文",
		"S02 001/100 | seq=001",
		"S02 100/100 | seq=100 | 中文编号100中文",
		"S03 001/100 | seq=001",
		"S03 100/100 | seq=100 | 中文编号100中文",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("numbered resume history missing %q after %d pages:\n%s\nrows=%#v", needle, pages, joined, rows)
		}
	}
	if strings.Contains(joined, "中 文") || strings.Contains(joined, "编 号") {
		t.Fatalf("CJK text must not gain materialized spaces:\n%s", joined)
	}
	promptIndex := historyTextIndex(texts, "PROMPT_AFTER")
	redrawEndIndex := historyTextIndex(texts, "REDRAW_END S03")
	if promptIndex < 0 || redrawEndIndex < 0 || promptIndex < redrawEndIndex {
		t.Fatalf("S03 frame/prompt order is wrong, prompt=%d redraw_end=%d:\n%s", promptIndex, redrawEndIndex, joined)
	}
	if pages <= 1 {
		t.Fatalf("test must exercise older pagination, pages=%d rows=%d", pages, len(rows))
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

func r333NumberedStreamSession(session int, lines int, cjkEvery int) string {
	var builder strings.Builder
	builder.WriteString(r333NumberedMarker("STREAM_BEGIN", session, lines))
	builder.WriteString("\r\n")
	for line := 1; line <= lines; line++ {
		builder.WriteString(r333NumberedLine(session, line, lines, cjkEvery))
		builder.WriteString("\r\n")
	}
	builder.WriteString(r333NumberedMarker("STREAM_END", session, lines))
	builder.WriteString("\r\n")
	return builder.String()
}

func r333NumberedRedrawSession(session int, lines int, cjkEvery int) string {
	var builder strings.Builder
	builder.WriteString("\x1b[?2026h\x1b[2J\x1b[H")
	builder.WriteString(r333NumberedMarker("REDRAW_BEGIN", session, lines))
	builder.WriteString("\r\n")
	for line := 1; line <= lines; line++ {
		builder.WriteString(r333NumberedLine(session, line, lines, cjkEvery))
		builder.WriteString("\r\n")
	}
	builder.WriteString(r333NumberedMarker("REDRAW_END", session, lines))
	builder.WriteString("\r\n\x1b[?2026l")
	return builder.String()
}

func r333NumberedMarker(label string, session int, lines int) string {
	return fmt.Sprintf("=== %s S%02d lines=%d clear=ed2 sync=1 ===", label, session, lines)
}

func r333NumberedLine(session int, line int, total int, cjkEvery int) string {
	text := fmt.Sprintf("S%02d %03d/%03d | seq=%03d", session, line, total, line)
	if cjkEvery > 0 && line%cjkEvery == 0 {
		text += fmt.Sprintf(" | 中文编号%03d中文", line)
	}
	return text
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
