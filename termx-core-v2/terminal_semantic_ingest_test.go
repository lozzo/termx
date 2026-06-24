package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
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

func TestTerminalSemanticProjectorConsumesVTermDamage(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	t.Cleanup(resetTerminalSemanticIngestTestHooks)
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ClearToEOLOps += next.ClearToEOLOps
		stats.ScrollbackAppends += next.ScrollbackAppends
		stats.FullReplaceOnly += next.FullReplaceOnly
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
	if stats.DamageBatches == 0 || stats.WriteSpanOps == 0 || stats.ScrollbackAppends == 0 {
		t.Fatalf("projector must observe vterm write/span and primary scroll-out damage, got %#v", stats)
	}
}

func TestTerminalSemanticProjectorCodexRawDamageSignals(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	t.Cleanup(resetTerminalSemanticIngestTestHooks)
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ClearToEOLOps += next.ClearToEOLOps
		stats.ScrollbackAppends += next.ScrollbackAppends
		stats.FullReplaceOnly += next.FullReplaceOnly
	}

	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 160, Rows: 48},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := strings.Join([]string{
		"before-one\nbefore-two\n",
		"\x1b[?2026h",
		"\x1b[?1004h",
		"\x1b[5;1H\x1b[J",
		"\x1b[5;48r\x1b[5;1H\x1bM\x1bM",
		"\x1b[r\x1b[1;11r\x1b[4;1H",
		"\x1b[39;49m\x1b[K╭─ update ─╮",
		"\x1b[21;1H› \x1b[21;3HSummarize recent commits",
		"\x1b[23;3Hgpt-5.5 xhigh · ~/Documents/workdir/termx",
		"\x1b[?2026l",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if stats.DamageBatches == 0 || stats.WriteSpanOps == 0 || stats.ClearToEOLOps == 0 {
		t.Fatalf("projector must observe Codex vterm damage ops, got %#v", stats)
	}
	window, err := server.LatestWindow("term-1", 160, 20)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 2 || !historyWindowContainsAll(window, "before-one", "before-two", "Summarize recent commits", "gpt-5.5") {
		t.Fatalf("Codex raw semantic ingest boundary regressed, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalSemanticProjectorCanApplyDamageWithoutRawParser(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(12, 2)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("prefixXYZ")},
				{Code: vterm.ScreenOpClearToEOL, Row: 0, Col: 6},
				{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: vtermCells("next")},
			},
			Cursor:   vterm.CursorState{Row: 1, Col: 4, Visible: true},
			Modes:    vterm.TerminalModes{AutoWrap: true},
			SizeCols: 12,
			SizeRows: 2,
		}},
		Cols:            12,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsAll(window, "prefix", "next") || historyWindowContainsText(window, "XYZ") {
		t.Fatalf("damage projector should write and clear without raw parser, got %#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorScrollbackDamageCommitsOnlyOnce(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(12, 2)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("one")},
				{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: vtermCells("two")},
				{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 0, Width: 12, Height: 2}, Dy: -1},
				{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: vtermCells("three")},
			},
			ScrollbackAppend: []vterm.DamageOp{{
				Runs:       []vterm.CellRun{{Text: "one"}},
				WrappedSet: true,
			}},
			Cursor:   vterm.CursorState{Row: 1, Col: 5, Visible: true},
			Modes:    vterm.TerminalModes{AutoWrap: true},
			SizeCols: 12,
			SizeRows: 2,
		}},
		Cols:            12,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if got := pipeline.CommittedIDs(); len(got) != 1 {
		t.Fatalf("scroll-out should commit exactly one logical line, got %v", got)
	}
	window, err := pipeline.LatestWindow(12, 8)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if strings.Count(text, "one") != 1 || strings.Count(text, "two") != 1 || strings.Count(text, "three") != 1 {
		t.Fatalf("scrollback damage must not duplicate payload, got %q rows=%#v", text, window.Rows)
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

func vtermCells(text string) []vterm.Cell {
	cells := make([]vterm.Cell, 0, len(text))
	for _, r := range text {
		cells = append(cells, vterm.Cell{Content: string(r), Width: 1})
	}
	return cells
}

func historyWindowJoinedText(window history.HistoryWindow) string {
	var builder strings.Builder
	for _, row := range window.Rows {
		builder.WriteString(row.Text)
		builder.WriteByte('\n')
	}
	return builder.String()
}
