package termxcorev2

import (
	"context"
	"strconv"
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
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
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
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
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
	if stats.DamageBatches == 0 || stats.WriteSpanOps == 0 {
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

func TestTerminalSemanticProjectorConsumesRealVTermScrollRegionRIAndAbsoluteCursor(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ControlOps += next.ControlOps
		stats.ModeOps += next.ModeOps
		stats.ScrollbackAppends += next.ScrollbackAppends
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(80, 12, 100, nil)
	pipeline := newTerminalHistoryPipeline(80, 12)
	for _, raw := range []string{"pre-one\n", "pre-two\n"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("seed vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            80,
			Rows:            12,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("seed ingest %q: %v", raw, err)
		}
	}
	if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		t.Fatalf("force commit seed: %v", err)
	}
	raw := strings.Join([]string{
		"\x1b[?2026h",
		"\x1b[3;10r\x1b[3;1H\x1bM\x1bM",
		"\x1b[r\x1b[1;6r\x1b[4;1H",
		"\x1b[K╭─ update ─╮",
		"\x1b[9;3HScroll region prompt",
		"\x1b[11;3Hstatus line",
		"\x1b[?2026l",
	}, "")
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "decstbm").Control == "" || firstDamageControl(damage, "ri").Control == "" {
		t.Fatalf("test requires real vterm scroll-region and RI semantic controls, damage=%#v", damage)
	}
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            80,
		Rows:            12,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("scroll-region/RI raw batch should be consumed from vterm semantic ops, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(80, 16)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 2 {
		t.Fatalf("scroll-region/RI repaint must not change committed depth, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
	for _, want := range []string{"pre-one", "pre-two", "Scroll region prompt", "status line"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("latest should contain %q after vterm scroll-region/RI ingest, rows=%#v damage=%#v", want, window.Rows, damage)
		}
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

func TestTerminalSemanticProjectorConsumesVTermCursorControls(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("abcdef")},
				{Code: vterm.ScreenOpControl, Control: "cub", Row: 0, Col: 3, Mode: 3},
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 3, Cells: vtermCells("XYZ")},
				{Code: vterm.ScreenOpControl, Control: "cup", Row: 1, Col: 2},
				{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 2, Cells: vtermCells("next")},
			},
			Cursor:   vterm.CursorState{Row: 1, Col: 6, Visible: true},
			Modes:    vterm.TerminalModes{AutoWrap: true},
			SizeCols: 12,
			SizeRows: 3,
		}},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsAll(window, "abcXYZ", "next") || historyWindowContainsText(window, "abcdef") {
		t.Fatalf("vterm cursor controls should drive overwrite/cursor position without parser, got %#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermCursorDamage(t *testing.T) {
	term := vterm.New(12, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte("abcdef\x1b[3DXYZ"))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "abcXYZ") || historyWindowContainsText(window, "abcdef") {
		t.Fatalf("real vterm cursor damage should overwrite via projector, got %#v damage=%#v", window.Rows, damage.Ops)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermVerticalCursorRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(12, 3)
	for _, raw := range []string{
		"top\r\nmiddle\r\nbottom\r",
		"\x1b[2AX",
		"\x1b[BY",
		"\x1b[EZ",
	} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            12,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("vertical cursor raw should use vterm semantic projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(12, 6)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsAll(window, "Xop", "mYddle", "Zottom") {
		t.Fatalf("vertical cursor semantic ops should mutate primary screen ownership, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesVerticalAbsoluteRawWithoutFallback(t *testing.T) {
	term := vterm.New(12, 4, 100, nil)
	pipeline := newTerminalHistoryPipeline(12, 4)
	seed := "top\r\nmiddle\r\nbottom"
	_, err, damage := term.WriteWithDamage([]byte(seed))
	if err != nil {
		t.Fatalf("seed vterm write: %v", err)
	}
	if err := pipeline.IngestSemanticBatch(terminalSemanticBatch{
		Raw:             seed,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            4,
		FromSharedVTerm: true,
	}); err != nil {
		t.Fatalf("seed ingest: %v", err)
	}

	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[2d\x1b[1GROW"
	_, err, damage = term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	vpa := firstDamageControl(damage, "vpa")
	if vpa.Control != "vpa" || vpa.Row != 1 {
		t.Fatalf("vertical absolute raw should expose vterm VPA semantic row 1, got %#v damage=%#v", vpa, damage)
	}
	if err := pipeline.IngestSemanticBatch(terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            4,
		FromSharedVTerm: true,
	}); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("vertical absolute raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 6)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "ROWdle") || strings.Contains(text, "ROWbottom") {
		t.Fatalf("vertical absolute semantic projector should rewrite row 2, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesHorizontalPositionAbsoluteRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 3, 100, nil)
	raw := "abcdef\r\x1b[4`XY"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	hpa := firstDamageControl(damage, "cha")
	if hpa.Control != "cha" || hpa.Col != 3 {
		t.Fatalf("horizontal position absolute raw should expose vterm HPA semantic col 3, got %#v damage=%#v", hpa, damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	if err := pipeline.IngestSemanticBatch(terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("horizontal position absolute raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "abcXYf" {
		t.Fatalf("horizontal position absolute semantic projector should rewrite at vterm column, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorPrefersOrderedSemanticOps(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("abcdef")},
				{Code: vterm.ScreenOpControl, Control: "cub", Row: 0, Col: 3, Mode: 3},
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 3, Cells: vtermCells("XYZ")},
			},
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("stale-screen-diff")},
			},
			SizeCols: 12,
			SizeRows: 3,
		}},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "abcXYZ") || historyWindowContainsText(window, "stale-screen-diff") {
		t.Fatalf("history projector must prefer ordered semantic ops over screen diff ops, got %#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorIgnoresFullReplaceOnlyRawHistory(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.FullReplaceOnly += next.FullReplaceOnly
	}
	defer resetTerminalSemanticIngestTestHooks()

	pipeline := newTerminalHistoryPipeline(20, 4)
	if err := pipeline.Ingest("committed\n"); err != nil {
		t.Fatalf("seed ingest: %v", err)
	}
	batch := terminalSemanticBatch{
		Raw: "parser-must-not-append",
		Damages: []vterm.WriteDamage{{
			RequiresFullReplace: true,
			FullReplaceReason:   "test",
			SizeCols:            20,
			SizeRows:            4,
		}},
		Cols:            20,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest full replace batch: %v", err)
	}
	if stats.FullReplaceOnly != 1 {
		t.Fatalf("expected full-replace-only boundary stat, got %#v", stats)
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if historyWindowContainsText(window, "parser-must-not-append") || !historyWindowContainsText(window, "committed") {
		t.Fatalf("full-replace-only shared batch must not append raw parser history, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesSyntheticFullReplaceSemanticOps(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(20, 4)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			RequiresFullReplace: true,
			FullReplaceReason:   "test",
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("semantic-full")},
			},
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("screen-diff")},
			},
			SizeCols: 20,
			SizeRows: 4,
		}},
		Cols:            20,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest full replace semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "semantic-full") || historyWindowContainsText(window, "screen-diff") {
		t.Fatalf("full-replace semantic ops should be consumed without parser/screen diff history, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesFullReplaceRawSemanticOpsWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	pipeline := newTerminalHistoryPipeline(20, 4)
	batch := terminalSemanticBatch{
		Raw: "parser-duplicate\n",
		Damages: []vterm.WriteDamage{{
			RequiresFullReplace: true,
			FullReplaceReason:   "test",
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("semantic-full")},
				{Code: vterm.ScreenOpControl, Control: "lf", Row: 0, Col: 13},
			},
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("screen-diff")},
			},
			SizeCols: 20,
			SizeRows: 4,
		}},
		Cols:            20,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest full replace raw semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("full-replace raw semantic batch should use vterm projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "semantic-full") || historyWindowContainsText(window, "parser-duplicate") || historyWindowContainsText(window, "screen-diff") {
		t.Fatalf("full-replace raw semantic ops should not append parser or screen diff history, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesFullReplaceRawCursorControlsWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	pipeline := newTerminalHistoryPipeline(20, 4)
	batch := terminalSemanticBatch{
		Raw: "parser-duplicate\n",
		Damages: []vterm.WriteDamage{{
			RequiresFullReplace: true,
			FullReplaceReason:   "test",
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("top")},
				{Code: vterm.ScreenOpControl, Control: "lf", Row: 0, Col: 3},
				{Code: vterm.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: vtermCells("middle")},
				{Code: vterm.ScreenOpControl, Control: "cuu", Row: 0, Col: 6, Mode: 1},
				{Code: vterm.ScreenOpControl, Control: "cr", Row: 0, Col: 6},
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("TOP")},
			},
			SizeCols: 20,
			SizeRows: 4,
		}},
		Cols:            20,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest full replace cursor semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("full-replace cursor semantic batch should use vterm projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "TOP") || !historyWindowContainsText(window, "middle") || historyWindowContainsText(window, "parser-duplicate") {
		t.Fatalf("full-replace cursor semantic ops should not append parser history, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorUsesSemanticClearControl(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Damages: []vterm.WriteDamage{{
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("abcdef")},
				{Code: vterm.ScreenOpControl, Control: "cub", Row: 0, Col: 2, Mode: 4},
				{Code: vterm.ScreenOpControl, Control: "el", Row: 0, Col: 2, Mode: 0},
			},
			Ops: []vterm.DamageOp{
				{Code: vterm.ScreenOpClearToEOL, Row: 0, Col: 0},
			},
			SizeCols: 12,
			SizeRows: 3,
		}},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "ab") || historyWindowContainsText(window, "abcdef") {
		t.Fatalf("semantic clear control should drive erase without using screen diff clear, got %#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorUsesSemanticOpsForRawSharedBatch(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw: "parser-would-write-this\x1b[2D!!",
		Damages: []vterm.WriteDamage{{
			SemanticOps: []vterm.DamageOp{
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: vtermCells("semantic")},
				{Code: vterm.ScreenOpControl, Control: "bs", Row: 0, Col: 7, Mode: 1},
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 7, Cells: vtermCells("X")},
				{Code: vterm.ScreenOpControl, Control: "ht", Row: 0, Col: 15},
				{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 15, Cells: vtermCells("Z")},
			},
			SizeCols: 16,
			SizeRows: 3,
		}},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "semantiX") || !strings.Contains(text, "Z") {
		t.Fatalf("raw shared batch should use vterm semantic ops, got %q rows=%#v", text, window.Rows)
	}
	if strings.Contains(text, "parser-would-write-this") || strings.Contains(text, "!!") {
		t.Fatalf("raw parser should not reapply terminal/text semantics when semantic ops exist, got %q rows=%#v", text, window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermRawSharedBatch(t *testing.T) {
	term := vterm.New(16, 3, 100, nil)
	raw := "ab\bX\tZ"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "aX") || !historyWindowContainsText(window, "Z") || historyWindowContainsText(window, "abX") {
		t.Fatalf("real raw shared batch should be driven by vterm semantic ops, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermStyledErase(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ClearToEOLOps += next.ClearToEOLOps
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(8, 2, 100, nil)
	raw := "\x1b[48;5;24mBG\x1b[K\x1b[0m"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(8, 2)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            8,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.ClearToEOLOps == 0 {
		t.Fatalf("styled EL raw batch should be consumed as vterm semantic clear, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(8, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "BG      " {
		t.Fatalf("history latest should include styled erase blank footprint, got %#v damage=%#v", window.Rows, damage)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 8 {
		t.Fatalf("history erase footprint should project as 8 cells, got %#v", cells)
	}
	for index, cell := range cells {
		if cell.Width != 1 || cell.Style.BG != "idx:24" {
			t.Fatalf("expected semantic styled erase cell %d to keep bg, got %#v rows=%#v", index, cell, window.Rows)
		}
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermPlainErase(t *testing.T) {
	term := vterm.New(8, 2, 100, nil)
	raw := "abcdef\x1b[4D\x1b[K"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(8, 2)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            8,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	window, err := pipeline.LatestWindow(8, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "ab") || historyWindowContainsText(window, "abcdef") {
		t.Fatalf("plain raw EL should be driven by vterm semantic control, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermModeOps(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ClearToEOLOps += next.ClearToEOLOps
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(20, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(20, 3)
	raw := "\x1b[?2026hrunning-frame"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.ModeOps == 0 {
		t.Fatalf("raw private mode should be consumed from vterm semantic modes, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "running-frame") {
		t.Fatalf("primary fullscreen mode should expose mutable frame without committed depth, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermEraseDisplay(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.DamageBatches += next.DamageBatches
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ClearToEOLOps += next.ClearToEOLOps
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(24, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(24, 3)
	for _, raw := range []string{"shell-one\n", "shell-two\n"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm seed write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            24,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest seed batch %q: %v", raw, err)
		}
	}
	raw := "\x1b[?2026h\x1b[H\x1b[Jframe-current"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            24,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.EraseDisplayOps == 0 {
		t.Fatalf("raw ED should be consumed from vterm semantic control, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(24, 6)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 2 || !historyWindowContainsAll(window, "shell-one", "shell-two", "frame-current") {
		t.Fatalf("Codex-style ED0 should preserve committed shell page and mutable frame, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesEraseDisplayModesRawWithoutFallback(t *testing.T) {
	t.Run("page break", func(t *testing.T) {
		term := vterm.New(24, 3, 100, nil)
		pipeline := newTerminalHistoryPipeline(24, 3)
		for _, raw := range []string{"shell-one\n", "draft-page"} {
			_, err, damage := term.WriteWithDamage([]byte(raw))
			if err != nil {
				t.Fatalf("vterm write %q: %v", raw, err)
			}
			batch := terminalSemanticBatch{
				Raw:             raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            24,
				Rows:            3,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch %q: %v", raw, err)
			}
		}

		resetTerminalSemanticIngestTestHooks()
		var stats terminalSemanticProjectorStats
		terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
			stats.SemanticProjectors += next.SemanticProjectors
			stats.RawFallbacks += next.RawFallbacks
			stats.WriteSpanOps += next.WriteSpanOps
			stats.EraseDisplayOps += next.EraseDisplayOps
			stats.ControlOps += next.ControlOps
		}
		defer resetTerminalSemanticIngestTestHooks()

		raw := "\x1b[2Jframe-current"
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		ed := firstDamageControl(damage, "ed")
		if ed.Control != "ed" || ed.Mode != 2 {
			t.Fatalf("ED2 raw batch should expose vterm ed mode 2 control, got %#v damage=%#v", ed, damage)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            24,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
		if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.EraseDisplayOps == 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
			t.Fatalf("ED2 raw batch should use vterm semantic projector without parser fallback, stats=%#v", stats)
		}
		window, err := pipeline.LatestWindow(24, 8)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if window.TotalLines != 2 || !historyWindowContainsAll(window, "shell-one", "draft-page", "frame-current") {
			t.Fatalf("ED2 semantic page-break should commit current page and start fresh frame, total=%d rows=%#v", window.TotalLines, window.Rows)
		}
	})

	t.Run("clear scrollback", func(t *testing.T) {
		term := vterm.New(24, 3, 100, nil)
		pipeline := newTerminalHistoryPipeline(24, 3)
		for _, raw := range []string{"first\nsecond\n"} {
			_, err, damage := term.WriteWithDamage([]byte(raw))
			if err != nil {
				t.Fatalf("vterm write %q: %v", raw, err)
			}
			batch := terminalSemanticBatch{
				Raw:             raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            24,
				Rows:            3,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch %q: %v", raw, err)
			}
		}
		if err := pipeline.ForceCommitFrontier(); err != nil {
			t.Fatalf("force commit seed: %v", err)
		}
		_, err, damage := term.WriteWithDamage([]byte("draft"))
		if err != nil {
			t.Fatalf("vterm write draft: %v", err)
		}
		if err := pipeline.IngestSemanticBatch(terminalSemanticBatch{
			Raw:             "draft",
			Damages:         []vterm.WriteDamage{damage},
			Cols:            24,
			Rows:            3,
			FromSharedVTerm: true,
		}); err != nil {
			t.Fatalf("ingest draft batch: %v", err)
		}

		resetTerminalSemanticIngestTestHooks()
		var stats terminalSemanticProjectorStats
		terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
			stats.SemanticProjectors += next.SemanticProjectors
			stats.RawFallbacks += next.RawFallbacks
			stats.WriteSpanOps += next.WriteSpanOps
			stats.EraseDisplayOps += next.EraseDisplayOps
			stats.ControlOps += next.ControlOps
		}
		defer resetTerminalSemanticIngestTestHooks()

		raw := "\x1b[3Jafter"
		_, err, damage = term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		ed := firstDamageControl(damage, "ed")
		if ed.Control != "ed" || ed.Mode != 3 {
			t.Fatalf("ED3 raw batch should expose vterm ed mode 3 control, got %#v damage=%#v", ed, damage)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            24,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
		if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.EraseDisplayOps == 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
			t.Fatalf("ED3 raw batch should use vterm semantic projector without parser fallback, stats=%#v", stats)
		}
		window, err := pipeline.LatestWindow(24, 8)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		text := historyWindowJoinedText(window)
		if window.TotalLines != 0 || strings.Contains(text, "first") || strings.Contains(text, "second") || !strings.Contains(text, "draft") || !strings.Contains(text, "after") {
			t.Fatalf("ED3 semantic clear-scrollback should truncate committed history and preserve mutable frontier, total=%d text=%q rows=%#v", window.TotalLines, text, window.Rows)
		}
	})
}

func TestTerminalSemanticProjectorConsumesRepeatedPrimaryRepaintRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(40, 6, 100, nil)
	pipeline := newTerminalHistoryPipeline(40, 6)
	for _, raw := range []string{
		"shell-one\nshell-two\n",
		"\x1b[?2026h\x1b[H\x1b[Jframe-one\x1b[6;1Hinput-old",
		"\x1b[H\x1b[Jframe-two\x1b[6;1Hinput-new\x1b[?2026l",
	} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            40,
			Rows:            6,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 || stats.EraseDisplayOps < 2 || stats.ModeOps < 2 || stats.ControlOps == 0 {
		t.Fatalf("repeated primary repaint should use vterm semantic projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(40, 10)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 2 {
		t.Fatalf("repeated repaint must not increase committed depth, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
	text := historyWindowJoinedText(window)
	for _, want := range []string{"shell-one", "shell-two", "frame-two", "input-new"} {
		if !strings.Contains(text, want) {
			t.Fatalf("latest should contain %q after repeated repaint, text=%q rows=%#v", want, text, window.Rows)
		}
	}
	for _, stale := range []string{"frame-one", "input-old"} {
		if strings.Contains(text, stale) {
			t.Fatalf("repeated repaint should replace stale %q, text=%q rows=%#v", stale, text, window.Rows)
		}
	}
}

func TestTerminalSemanticProjectorKeepsCodexUpdateCardAcrossLowerED0(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
		stats.EraseDisplayOps += next.EraseDisplayOps
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(96, 14, 100, nil)
	pipeline := newTerminalHistoryPipeline(96, 14)
	raw := strings.Join([]string{
		"shell\n",
		"\x1b[?2026h\x1b[H\x1b[J",
		"\x1b[3;1HUpdate available! 0.141.0 -> 0.142.0",
		"\x1b[4;1HRun brew upgrade --cask codex to update.",
		"\x1b[7;1HOpenAI Codex",
		"\x1b[10;1H\x1b[J",
		"\x1b[10;1H> Write tests for @filename",
		"\x1b[12;1Hgpt-5.5 xhigh · ~/Documents/workdir/termx",
		"\x1b[?2026l",
	}, "")
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "ed").Control == "" {
		t.Fatalf("test requires real vterm ED semantic control, damage=%#v", damage)
	}
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            96,
		Rows:            14,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 || stats.EraseDisplayOps < 2 || stats.ModeOps < 2 || stats.ControlOps == 0 {
		t.Fatalf("Codex lower ED0 repaint should use vterm semantic projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(96, 16)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, want := range []string{
		"shell",
		"Update available! 0.141.0 -> 0.142.0",
		"Run brew upgrade --cask codex to update.",
		"OpenAI Codex",
		"> Write tests for @filename",
		"gpt-5.5 xhigh",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("latest should keep %q across lower ED0 repaint, text=%q rows=%#v", want, text, window.Rows)
		}
	}
	if window.TotalLines != 1 {
		t.Fatalf("Codex update card frame must stay mutable without committed depth growth, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
	for _, row := range window.Rows {
		if strings.Contains(row.Text, "Update available!") && row.Committed {
			t.Fatalf("update card current frame row must stay mutable, rows=%#v", window.Rows)
		}
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermPrimaryScrollOut(t *testing.T) {
	for _, rows := range []int{1, 2} {
		t.Run("rows_"+string(rune('0'+rows)), func(t *testing.T) {
			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.DamageBatches += next.DamageBatches
				stats.WriteSpanOps += next.WriteSpanOps
				stats.ClearToEOLOps += next.ClearToEOLOps
				stats.EraseDisplayOps += next.EraseDisplayOps
				stats.ModeOps += next.ModeOps
				stats.ScrollbackAppends += next.ScrollbackAppends
			}
			defer resetTerminalSemanticIngestTestHooks()

			term := vterm.New(16, rows, 100, nil)
			pipeline := newTerminalHistoryPipeline(16, rows)
			for _, raw := range []string{"alpha\n", "beta\n", "gamma"} {
				_, err, damage := term.WriteWithDamage([]byte(raw))
				if err != nil {
					t.Fatalf("vterm write %q: %v", raw, err)
				}
				batch := terminalSemanticBatch{
					Raw:             raw,
					Damages:         []vterm.WriteDamage{damage},
					Cols:            16,
					Rows:            rows,
					FromSharedVTerm: true,
				}
				if err := pipeline.IngestSemanticBatch(batch); err != nil {
					t.Fatalf("ingest semantic batch %q: %v", raw, err)
				}
			}
			if stats.ScrollbackAppends == 0 {
				t.Fatalf("raw primary scroll-out should be consumed from vterm semantic scrollback, rows=%d stats=%#v", rows, stats)
			}
			window, err := pipeline.LatestWindow(16, 8)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			text := historyWindowJoinedText(window)
			for _, want := range []string{"alpha", "beta", "gamma"} {
				if strings.Count(text, want) != 1 {
					t.Fatalf("rows=%d should contain %q once via semantic scroll-out, got %q rows=%#v", rows, want, text, window.Rows)
				}
			}
		})
	}
}

func TestTerminalSemanticProjectorConsumesLowRowsMultiScrollRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.ScrollbackAppends += next.ScrollbackAppends
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(10, 2, 100, nil)
	raw := "one\ntwo\nthree\nfour\nfive"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "soft-wrap").Control != "soft-wrap" {
		t.Fatalf("low rows multi-scroll batch should expose vterm soft-wrap semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(10, 2)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            10,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ScrollbackAppends < 2 {
		t.Fatalf("low rows multi-scroll batch should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(10, 10)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, want := range []string{"one", "two", "three", "four", "five"} {
		if strings.Count(text, want) != 1 {
			t.Fatalf("multi-scroll semantic batch should preserve %q once, got %q rows=%#v damage=%#v", want, text, window.Rows, damage)
		}
	}
}

func TestTerminalSemanticProjectorConsumesTallRowsPrimaryScrollOutRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.ScrollbackAppends += next.ScrollbackAppends
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 5, 100, nil)
	raw := "one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\nseven"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if len(damage.ScrollbackAppend) < 2 {
		t.Fatalf("tall rows batch should expose primary scrollback ownership exits, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 5)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            5,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ScrollbackAppends < 2 {
		t.Fatalf("tall primary scroll-out batch should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 12)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, want := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		if strings.Count(text, want) != 1 {
			t.Fatalf("tall primary scroll-out semantic batch should preserve %q once, got %q rows=%#v damage=%#v", want, text, window.Rows, damage)
		}
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermStyledLinkTextRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(24, 4, 100, nil)
	raw := "\x1b[1;31mERR\x1b[0m \x1b]8;id=termx;https://example.test\aLINK\x1b]8;;\a\nplain"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(24, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            24,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("styled/link raw batch should use vterm semantic text cells without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(24, 8)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "ERR LINK" || window.Rows[1].Text != "plain" {
		t.Fatalf("styled/link semantic text should preserve logical rows, got %#v damage=%#v", window.Rows, damage)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 3 {
		t.Fatalf("expected styled/link cells from vterm semantic text, got %#v rows=%#v", cells, window.Rows)
	}
	if cells[0].Text != "ERR" || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("expected red bold ERR from vterm semantic text, got %#v rows=%#v", cells[0], window.Rows)
	}
	if cells[2].Text != "LINK" || cells[2].LinkURL != "https://example.test" || cells[2].LinkParams != "id=termx" {
		t.Fatalf("expected OSC8 link metadata from vterm semantic text, got %#v rows=%#v", cells[2], window.Rows)
	}
	if window.Rows[1].Cells[0].Text != "plain" || window.Rows[1].Cells[0].Style != (history.CellStyle{}) || window.Rows[1].Cells[0].LinkURL != "" {
		t.Fatalf("expected following line to be plain after SGR/OSC8 reset, got %#v", window.Rows[1].Cells)
	}
}

func TestTerminalSemanticProjectorConsumesC1OSC8RawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := string([]byte{0x9d}) + "8;id=c1;https://example.test/c1" + string([]byte{0x9c}) +
		"LINK" +
		string([]byte{0x9d}) + "8;;" + string([]byte{0x9c}) +
		" plain"
	term := vterm.New(32, 4, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainTextSpan(damage.SemanticOps, "LINK") || !damageOpsContainTextSpan(damage.SemanticOps, " plain") {
		t.Fatalf("C1 OSC8 should expose linked/plain text through vterm semantic ops, damage=%#v", damage)
	}

	pipeline := newTerminalHistoryPipeline(32, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            32,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("C1 OSC8 raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(32, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "LINK plain" {
		t.Fatalf("C1 OSC8 semantic text should preserve one logical row, got %#v damage=%#v", window.Rows, damage)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 2 {
		t.Fatalf("expected linked/plain cells from C1 OSC8 vterm semantic text, got %#v rows=%#v", cells, window.Rows)
	}
	if cells[0].Text != "LINK" || cells[0].LinkURL != "https://example.test/c1" || cells[0].LinkParams != "id=c1" {
		t.Fatalf("expected C1 OSC8 link metadata from vterm semantic text, got %#v rows=%#v", cells[0], window.Rows)
	}
	if cells[1].Text != " plain" || cells[1].LinkURL != "" || cells[1].LinkParams != "" {
		t.Fatalf("expected following text to reset C1 OSC8 link, got %#v rows=%#v", cells[1], window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesOSCTitleAndWorkingDirectoryRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(40, 4, 100, nil)
	raw := "\x1b]2;termx-title\x07\x1b]7;file://host/srv/app\x1b\\prompt$ "
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(40, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            40,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("OSC title/cwd batch should use shared vterm semantic text without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(40, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "prompt$ ") {
		t.Fatalf("expected prompt text from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
	for _, forbidden := range []string{"termx-title", "file://host/srv/app"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("OSC title/cwd state must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
}

func TestTerminalSemanticProjectorConsumesOSC8ScrollOutRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ScrollbackAppends += next.ScrollbackAppends
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(20, 3, 100, nil)
	raw := "\x1b]8;id=termx;https://example.test\aLINK\x1b]8;;\a\r\nplain\r\nthird\r\nfourth"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if len(damage.ScrollbackAppend) == 0 {
		t.Fatalf("expected OSC8 batch to produce primary scroll-out, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 || stats.ScrollbackAppends == 0 {
		t.Fatalf("OSC8 scroll-out raw batch should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 6)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsAll(window, "LINK", "plain", "third", "fourth") {
		t.Fatalf("OSC8 scroll-out semantic batch should preserve logical rows, got %#v damage=%#v", window.Rows, damage)
	}
	if len(window.Rows) == 0 || len(window.Rows[0].Cells) == 0 || window.Rows[0].Cells[0].LinkURL != "https://example.test" || window.Rows[0].Cells[0].LinkParams != "id=termx" {
		t.Fatalf("expected OSC8 link metadata from vterm semantic scroll-out, got %#v rows=%#v", window.Rows[0].Cells, window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermGraphemeTextRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(20, 4, 100, nil)
	raw := "\x1b[35;1me\u0301好\x1b[0m\nx"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(20, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("wide/combining raw batch should use vterm semantic text cells without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(2, 8)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 3 || window.Rows[0].Text != "e\u0301" || window.Rows[1].Text != "好" || window.Rows[2].Text != "x" {
		t.Fatalf("grapheme semantic text should keep combining/wide boundaries, got %#v damage=%#v", window.Rows, damage)
	}
	first := window.Rows[0].Cells
	if len(first) != 1 || first[0].Text != "e\u0301" || first[0].Style.FG != "ansi:5" || !first[0].Style.Bold {
		t.Fatalf("expected styled combining grapheme cell from vterm semantic text, got %#v rows=%#v", first, window.Rows)
	}
	second := window.Rows[1].Cells
	if len(second) != 1 || second[0].Text != "好" || second[0].Width != 2 || second[0].Style.FG != "ansi:5" || !second[0].Style.Bold {
		t.Fatalf("expected styled wide grapheme cell from vterm semantic text, got %#v rows=%#v", second, window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesStyledScrollOutFootprintRawBatch(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
		stats.ScrollbackAppends += next.ScrollbackAppends
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(8, 3, 100, nil)
	raw := "seed1\nseed2\n\x1b[48;5;24mabcdefghij\x1b[0m\n"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if len(damage.ScrollbackAppend) == 0 {
		t.Fatalf("styled scroll-out batch should expose primary ownership exit, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(8, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            8,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 || stats.ScrollbackAppends == 0 {
		t.Fatalf("styled scroll-out footprint should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(8, 6)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) < 3 || window.Rows[len(window.Rows)-2].Text != "abcdefgh" || window.Rows[len(window.Rows)-1].Text != "ij" {
		t.Fatalf("styled scroll-out semantic batch should preserve wrapped text, got %#v damage=%#v", window.Rows, damage)
	}
	tailRow := window.Rows[len(window.Rows)-1]
	if len(tailRow.Cells) != 2 || tailRow.Cells[0].Style.BG != "idx:24" || tailRow.Cells[1].Style.BG != "idx:24" {
		t.Fatalf("styled continuation cells should keep bg without materialized blanks, got %#v row=%#v", tailRow.Cells, tailRow)
	}
	if tailRow.TailFill == nil || tailRow.TailFill.Style.BG != "idx:24" {
		t.Fatalf("styled scroll-out tail footprint should be row tail fill metadata, got %#v row=%#v damage=%#v", tailRow.TailFill, tailRow, damage)
	}
}

func TestTerminalSemanticProjectorConsumesRealVTermModeOnlyAltScreen(t *testing.T) {
	term := vterm.New(20, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(20, 3)
	for _, raw := range []string{"primary\n", "\x1b[?1049h", "\x1b[?1049l", "after"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            20,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "primary") || !historyWindowContainsText(window, "after") {
		t.Fatalf("mode-only alt-screen boundary should be driven by vterm semantic modes, got %#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesAltScreenModeVariantsRawWithoutFallback(t *testing.T) {
	for _, mode := range []int{47, 1047, 1049} {
		t.Run("mode_"+strconv.Itoa(mode), func(t *testing.T) {
			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.SemanticProjectors += next.SemanticProjectors
				stats.RawFallbacks += next.RawFallbacks
				stats.ModeOps += next.ModeOps
				stats.WriteSpanOps += next.WriteSpanOps
			}
			defer resetTerminalSemanticIngestTestHooks()

			term := vterm.New(20, 3, 100, nil)
			pipeline := newTerminalHistoryPipeline(20, 3)
			for _, raw := range []string{
				"primary\n",
				"\x1b[?" + strconv.Itoa(mode) + "h",
				"alt-hidden",
				"\x1b[?" + strconv.Itoa(mode) + "l",
				"after",
			} {
				_, err, damage := term.WriteWithDamage([]byte(raw))
				if err != nil {
					t.Fatalf("vterm write %q: %v", raw, err)
				}
				if strings.Contains(raw, "?"+strconv.Itoa(mode)) && firstDamageMode(damage, mode).Code != vterm.ScreenOpModes {
					t.Fatalf("alt-screen mode %d should be exposed as vterm semantic mode, raw=%q damage=%#v", mode, raw, damage)
				}
				batch := terminalSemanticBatch{
					Raw:             raw,
					Damages:         []vterm.WriteDamage{damage},
					Cols:            20,
					Rows:            3,
					FromSharedVTerm: true,
				}
				if err := pipeline.IngestSemanticBatch(batch); err != nil {
					t.Fatalf("ingest semantic batch %q: %v", raw, err)
				}
			}
			if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps < 2 {
				t.Fatalf("alt-screen mode %d raw should use vterm semantic projector without parser fallback, stats=%#v", mode, stats)
			}
			window, err := pipeline.LatestWindow(20, 5)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			if !historyWindowContainsText(window, "primary") || !historyWindowContainsText(window, "after") || historyWindowContainsText(window, "alt-hidden") {
				t.Fatalf("alt-screen mode %d should route alt content away from primary history, rows=%#v", mode, window.Rows)
			}
		})
	}
}

func TestTerminalSemanticProjectorConsumesBracketedPasteModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(20, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(20, 3)
	for _, raw := range []string{"\x1b[?2004h", "paste-ready\n", "\x1b[?2004l"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            20,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("bracketed paste mode raw should use vterm semantic projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "paste-ready") {
		t.Fatalf("bracketed paste mode should not suppress ordinary primary text, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesLegacyMouseModesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?9h\x1b[?1001hmouse-frame"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 9).Code != vterm.ScreenOpModes || firstDamageMode(damage, 1001).Code != vterm.ScreenOpModes {
		t.Fatalf("legacy mouse modes should be exposed as vterm semantic modes, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("legacy mouse modes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "mouse-frame") {
		t.Fatalf("legacy mouse modes should expose mutable primary frame without committed depth, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesFocusAndSGRMouseModesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1004h\x1b[?1006hframe\x1b[?1006l\x1b[?1004l\x1b[?1003l\x1b[?1002l\x1b[?1000l"
	term := vterm.New(24, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	for _, mode := range []int{1000, 1002, 1003, 1004, 1006} {
		if firstDamageMode(damage, mode).Code != vterm.ScreenOpModes {
			t.Fatalf("focus/SGR mouse mode %d should be exposed as vterm semantic mode, damage=%#v", mode, damage)
		}
	}
	pipeline := newTerminalHistoryPipeline(24, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            24,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 10 || stats.WriteSpanOps == 0 {
		t.Fatalf("focus/SGR mouse modes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(24, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "frame") {
		t.Fatalf("focus/SGR mouse modes should expose mutable primary frame without committed depth, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesCursorVisibilityModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?25lframe\x1b[?25h"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	firstMode := firstDamageMode(damage, 25)
	if firstMode.Code != vterm.ScreenOpModes || !firstMode.Private || firstMode.Enabled {
		t.Fatalf("cursor visibility off should be exposed as private mode 25 off, mode=%#v damage=%#v", firstMode, damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("cursor visibility raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "frame") {
		t.Fatalf("cursor visibility mode should expose mutable primary frame without committed depth, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesCursorStyleRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[5 qframe\x1b[2 q"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "decscusr").Code != vterm.ScreenOpControl {
		t.Fatalf("cursor style should expose vterm semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("cursor style raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "frame") || strings.Contains(text, "[5 q") || strings.Contains(text, "[2 q") {
		t.Fatalf("cursor style state must not enter history text, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesReportRequestsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[6n\x1b[?6n\x1b[?25$pframe"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	for _, control := range []string{"dsr", "decxcpr", "decrqm"} {
		if firstDamageControl(damage, control).Code != vterm.ScreenOpControl {
			t.Fatalf("report request should expose vterm semantic control %q, damage=%#v", control, damage)
		}
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 3 || stats.WriteSpanOps == 0 {
		t.Fatalf("report request raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"[6n", "[?6n", "[?25$p"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("terminal report request must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "frame") {
		t.Fatalf("expected text after report requests from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesDeviceAttributesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[c\x1b[>cframe"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	for _, control := range []string{"da", "da2"} {
		if firstDamageControl(damage, control).Code != vterm.ScreenOpControl {
			t.Fatalf("device attributes should expose vterm semantic control %q, damage=%#v", control, damage)
		}
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("device attributes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"[c", "[>c"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("device attributes request must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "frame") {
		t.Fatalf("expected text after device attributes from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesOSCDefaultColorsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b]10;#112233\x07\x1b]11;#445566\x07\x1b]12;#778899\x07" +
		"\x1b]10;?\x07\x1b]11;?\x07\x1b]12;?\x07" +
		"palette" +
		"\x1b]110\x07\x1b]111\x07\x1b]112\x07"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainTextSpan(damage.SemanticOps, "palette") {
		t.Fatalf("OSC default color batch should expose following text through vterm semantic ops, damage=%#v", damage)
	}
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl || op.Code == vterm.ScreenOpTitle {
			t.Fatalf("OSC default colors must remain vterm-owned state, got history semantic op %#v in %#v", op, damage.SemanticOps)
		}
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("OSC default color raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"]10;", "]11;", "]12;", "]110", "]111", "]112", "#112233", "#445566", "#778899"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("OSC default color control must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "palette") {
		t.Fatalf("expected text after OSC default colors from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesStringControlsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1bP1;2+qignored-dcs\x1b\\" +
		"\x1b_ignored-apc\x1b\\" +
		"\x1bXignored-sos\x1b\\" +
		"\x1b^ignored-pm\x1b\\" +
		"after"
	term := vterm.New(30, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainTextSpan(damage.SemanticOps, "after") {
		t.Fatalf("string controls should expose following text through vterm semantic ops, damage=%#v", damage)
	}
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl || op.Code == vterm.ScreenOpTitle {
			t.Fatalf("string controls must remain vterm parser-owned state, got history semantic op %#v in %#v", op, damage.SemanticOps)
		}
	}
	pipeline := newTerminalHistoryPipeline(30, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            30,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("string-control raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(30, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"ignored-dcs", "ignored-apc", "ignored-sos", "ignored-pm"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("string control payload must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "after") {
		t.Fatalf("expected text after string controls from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesOSCClipboardRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b]52;c;SGVsbG8=\x07" +
		"\x1b]52;p;?\x07" +
		"\x1b]52;c;\x07" +
		"clip-ok"
	term := vterm.New(30, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainTextSpan(damage.SemanticOps, "clip-ok") {
		t.Fatalf("OSC clipboard should expose following text through vterm semantic ops, damage=%#v", damage)
	}
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl || op.Code == vterm.ScreenOpTitle {
			t.Fatalf("OSC clipboard must remain vterm parser-owned state, got history semantic op %#v in %#v", op, damage.SemanticOps)
		}
	}
	pipeline := newTerminalHistoryPipeline(30, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            30,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("OSC clipboard raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(30, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"]52;", "SGVsbG8", "c;", "p;"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("OSC clipboard control must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "clip-ok") {
		t.Fatalf("expected text after OSC clipboard from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesC1StringControlsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := string([]byte{0x9d}) + "52;c;SGVsbG8=" + string([]byte{0x9c}) +
		string([]byte{0x90}) + "1;2+qignored-c1-dcs" + string([]byte{0x9c}) +
		string([]byte{0x9f}) + "ignored-c1-apc" + string([]byte{0x9c}) +
		string([]byte{0x98}) + "ignored-c1-sos" + string([]byte{0x9c}) +
		string([]byte{0x9e}) + "ignored-c1-pm" + string([]byte{0x9c}) +
		"c1-ok"
	term := vterm.New(40, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainTextSpan(damage.SemanticOps, "c1-ok") {
		t.Fatalf("C1 string controls should expose following text through vterm semantic ops, damage=%#v", damage)
	}
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl || op.Code == vterm.ScreenOpTitle {
			t.Fatalf("C1 string controls must remain vterm parser-owned state, got history semantic op %#v in %#v", op, damage.SemanticOps)
		}
	}
	pipeline := newTerminalHistoryPipeline(40, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            40,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("C1 string-control raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(40, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	for _, forbidden := range []string{"ignored-c1-dcs", "ignored-c1-apc", "ignored-c1-sos", "ignored-c1-pm", "SGVsbG8", "52;"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("C1 string control payload must not enter history text %q, got %q rows=%#v", forbidden, text, window.Rows)
		}
	}
	if !strings.Contains(text, "c1-ok") {
		t.Fatalf("expected text after C1 string controls from semantic projector, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesC1CSIRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "abcdef" + string([]byte{0x9b}) + "3DXYZ" +
		string([]byte{0x9b}) + "2;5H!" +
		string([]byte{0x9b}) + "Ktail"
	term := vterm.New(12, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "cub").Mode != 3 || firstDamageControl(damage, "cup").Row != 1 || firstDamageControl(damage, "el").Control != "el" {
		t.Fatalf("C1 CSI raw should expose vterm cursor/erase semantic controls, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 3 || stats.WriteSpanOps == 0 {
		t.Fatalf("C1 CSI raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "abcXYZ") || !strings.Contains(text, "    !tail") {
		t.Fatalf("C1 CSI semantic projector should apply vterm cursor/erase controls, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesC1ControlsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "ab" + string([]byte{0x88}) +
		string([]byte{0x84}) + "down" +
		string([]byte{0x8d}) + "\r\tZ"
	term := vterm.New(16, 2, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	hts := firstDamageControl(damage, "hts")
	ind := firstDamageControl(damage, "ind")
	ri := firstDamageControl(damage, "ri")
	ht := firstDamageControl(damage, "ht")
	if hts.Control != "hts" || hts.Col != 2 || ind.Control != "ind" || ri.Control != "ri" || ht.Control != "ht" || ht.Col != 2 {
		t.Fatalf("C1 control raw should expose vterm HTS/IND/RI/HT semantic controls, hts=%#v ind=%#v ri=%#v ht=%#v damage=%#v", hts, ind, ri, ht, damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 2)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            2,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 5 || stats.WriteSpanOps == 0 {
		t.Fatalf("C1 controls raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) < 2 || window.Rows[0].Text != "abZ" || !strings.Contains(historyWindowJoinedText(window), "down") {
		t.Fatalf("C1 control semantic projector should apply vterm IND/RI/HTS controls, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesC1NELRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "abc" + string([]byte{0x85}) + "Z"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "cr").Control != "cr" || firstDamageControl(damage, "lf").Control != "lf" {
		t.Fatalf("C1 NEL raw should expose ordered CR/LF semantic controls, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("C1 NEL raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "abc" || window.Rows[1].Text != "Z" {
		t.Fatalf("C1 NEL semantic projector should apply vterm CR/LF controls, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesApplicationKeyModesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?1h\x1b[?66hkeys\x1b[?66l\x1b[?1l"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 1).Code != vterm.ScreenOpModes || firstDamageMode(damage, 66).Code != vterm.ScreenOpModes {
		t.Fatalf("application key modes should be exposed as vterm semantic modes, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 4 || stats.WriteSpanOps == 0 {
		t.Fatalf("application key modes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "keys") {
		t.Fatalf("application key modes must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesKeypadEscModesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b=esc-keys\x1b>"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 66).Code != vterm.ScreenOpModes {
		t.Fatalf("keypad ESC modes should be exposed as vterm semantic modes, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("keypad ESC modes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "esc-keys") {
		t.Fatalf("keypad ESC modes must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesAlternateScrollModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?1007hscroll\x1b[?1007l"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 1007).Code != vterm.ScreenOpModes {
		t.Fatalf("alternate scroll mode should be exposed as vterm semantic mode, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("alternate scroll mode raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "scroll") {
		t.Fatalf("alternate scroll mode must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesUTF8MouseModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?1005hutf8-mouse\x1b[?1005l"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 1005).Code != vterm.ScreenOpModes {
		t.Fatalf("UTF-8 mouse mode should be exposed as vterm semantic mode, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("UTF-8 mouse mode raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "utf8-mouse") {
		t.Fatalf("UTF-8 mouse mode must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesExtendedMouseEncodingModesRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?1015h\x1b[?1016hext-mouse\x1b[?1016l\x1b[?1015l"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 1015).Code != vterm.ScreenOpModes || firstDamageMode(damage, 1016).Code != vterm.ScreenOpModes {
		t.Fatalf("extended mouse encoding modes should be exposed as vterm semantic modes, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 4 || stats.WriteSpanOps == 0 {
		t.Fatalf("extended mouse encoding modes raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "ext-mouse") {
		t.Fatalf("extended mouse encoding modes must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesUnicodeCoreModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?2027he\u0301好\x1b[?2027l"
	term := vterm.New(20, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 2027).Code != vterm.ScreenOpModes {
		t.Fatalf("Unicode Core mode should be exposed as vterm semantic mode, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(20, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            20,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("Unicode Core mode raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "é好") {
		t.Fatalf("Unicode Core mode must not become history truth or suppress text, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
	cells := window.Rows[0].Cells
	if len(cells) < 2 || cells[0].Text != "é" || cells[0].Width != 1 || cells[1].Text != "好" || cells[1].Width != 2 {
		t.Fatalf("Unicode Core mode text should keep vterm grapheme cell boundaries, got %#v rows=%#v", cells, window.Rows)
	}
}

func TestTerminalSemanticProjectorConsumesOriginModeRawWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cursor string
	}{
		{name: "cup", cursor: "\x1b[1;1H"},
		{name: "hvp", cursor: "\x1b[1;1f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.SemanticProjectors += next.SemanticProjectors
				stats.RawFallbacks += next.RawFallbacks
				stats.ModeOps += next.ModeOps
				stats.ControlOps += next.ControlOps
				stats.WriteSpanOps += next.WriteSpanOps
			}
			defer resetTerminalSemanticIngestTestHooks()

			raw := "\x1b[2;4r\x1b[?6h" + tc.cursor + "X"
			term := vterm.New(16, 6, 100, nil)
			_, err, damage := term.WriteWithDamage([]byte(raw))
			if err != nil {
				t.Fatalf("vterm write: %v", err)
			}
			cup := firstDamageControl(damage, "cup")
			if firstDamageMode(damage, 6).Code != vterm.ScreenOpModes || cup.Row != 1 || cup.Col != 0 {
				t.Fatalf("origin mode should expose vterm-resolved CUP coordinates, cup=%#v damage=%#v", cup, damage)
			}
			pipeline := newTerminalHistoryPipeline(16, 6)
			batch := terminalSemanticBatch{
				Raw:             raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            16,
				Rows:            6,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch: %v", err)
			}
			if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps == 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
				t.Fatalf("origin mode raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
			}
			window, err := pipeline.LatestWindow(16, 6)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			if !historyWindowContainsText(window, "X") {
				t.Fatalf("origin mode text should remain visible through vterm semantic projector, rows=%#v damage=%#v", window.Rows, damage)
			}
		})
	}
}

func TestTerminalSemanticProjectorConsumesAutowrapModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?7l123456789Z"
	term := vterm.New(6, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 7).Code != vterm.ScreenOpModes || firstDamageMode(damage, 7).Enabled {
		t.Fatalf("autowrap mode should be exposed as disabled vterm mode op, damage=%#v", damage)
	}
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl && op.Control == "soft-wrap" {
			t.Fatalf("autowrap disabled raw should rely on vterm-resolved print path without soft-wrap, damage=%#v", damage)
		}
	}
	pipeline := newTerminalHistoryPipeline(6, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            6,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps == 0 || stats.ControlOps != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("autowrap raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(6, 3)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if historyWindowContainsText(window, "123456") || !historyWindowContainsText(window, "12345Z") {
		t.Fatalf("autowrap disabled text should follow vterm overwrite semantics, rows=%#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesPrivateSaveRestoreCursorRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "abc\x1b[?1048h\x1b[2;1HZZ\x1b[?1048lX"
	term := vterm.New(10, 4, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 1048).Code != vterm.ScreenOpModes {
		t.Fatalf("private save cursor mode should be exposed as vterm mode op, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(10, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            10,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("private save/restore cursor raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(10, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "abcX") || !historyWindowContainsText(window, "ZZ") {
		t.Fatalf("private save/restore cursor should use vterm-restored write positions, rows=%#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesLeftRightMarginsRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?W\x1b[?6h\x1b[?69h\x1b[3;6s\x1b[1;2HX\x1b[ZA"
	term := vterm.New(10, 1, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 69).Code != vterm.ScreenOpModes {
		t.Fatalf("left/right margin mode should be exposed as vterm mode op, damage=%#v", damage)
	}
	region := firstDamageControl(damage, "decslrm")
	if region.Mode != 3 || region.Bottom != 6 {
		t.Fatalf("DECSLRM should expose vterm-owned horizontal margins, got %#v damage=%#v", region, damage)
	}
	cbt := firstDamageControl(damage, "cbt")
	if cbt.Col != 2 {
		t.Fatalf("CBT should expose final left-margin column from vterm, got %#v damage=%#v", cbt, damage)
	}
	pipeline := newTerminalHistoryPipeline(10, 1)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            10,
		Rows:            1,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.ControlOps < 3 || stats.WriteSpanOps == 0 {
		t.Fatalf("left/right margin raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(10, 1)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "AX") {
		t.Fatalf("left/right margin text should follow vterm-resolved CBT coordinates, rows=%#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesLinefeedNewlineModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 4, 100, nil)
	raw := "\x1b[20habc\nZ\x1b[20l"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 20).Mode != 20 {
		t.Fatalf("linefeed-newline raw should expose ANSI mode 20 semantic op, damage=%#v", damage)
	}
	if firstDamageControl(damage, "cr").Control != "cr" {
		t.Fatalf("linefeed-newline mode should let vterm expose LF followed by CR, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 4)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            4,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("linefeed-newline raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "abc" || window.Rows[1].Text != "Z" {
		t.Fatalf("linefeed-newline semantic projector should write following text at line start, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesBackwardTabRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(16, 3, 100, nil)
	raw := "123456789\x1b[ZXY"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "cbt").Control != "cbt" {
		t.Fatalf("backward tab raw should expose vterm CBT semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("backward tab raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "12345678XY" {
		t.Fatalf("backward tab semantic projector should overwrite at previous tab stop, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesForwardTabRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(24, 3, 100, nil)
	raw := "ab\x1b[2IZ"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	ht := firstDamageControl(damage, "ht")
	if ht.Control != "ht" || ht.Col != 16 {
		t.Fatalf("forward tab raw should expose vterm-resolved HT semantic control, ht=%#v damage=%#v", ht, damage)
	}
	pipeline := newTerminalHistoryPipeline(24, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            24,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("forward tab raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(24, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "ab              Z" {
		t.Fatalf("forward tab semantic projector should use vterm tabstop state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesCustomTabStopRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(16, 3, 100, nil)
	raw := "ab\x1bH\r\tZ"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	hts := firstDamageControl(damage, "hts")
	ht := firstDamageControl(damage, "ht")
	if hts.Control != "hts" || hts.Col != 2 || ht.Control != "ht" || ht.Col != 2 {
		t.Fatalf("custom tab stop raw should expose HTS and vterm-resolved HT semantic controls, hts=%#v ht=%#v damage=%#v", hts, ht, damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 3 || stats.WriteSpanOps == 0 {
		t.Fatalf("custom tab stop raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "abZ" {
		t.Fatalf("custom tab stop semantic projector should use vterm tabstop state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesTabClearRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(16, 3, 100, nil)
	raw := "ab\r\x1b[3g\tZ"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	tbc := firstDamageControl(damage, "tbc")
	ht := firstDamageControl(damage, "ht")
	if tbc.Control != "tbc" || tbc.Mode != 3 || ht.Control != "ht" || ht.Col != 15 {
		t.Fatalf("tab clear raw should expose TBC and vterm-resolved HT semantic controls, tbc=%#v ht=%#v damage=%#v", tbc, ht, damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 3 || stats.WriteSpanOps == 0 {
		t.Fatalf("tab clear raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "ab             Z" {
		t.Fatalf("tab clear semantic projector should use vterm tabstop state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesTabResetRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(16, 3, 100, nil)
	raw := "ab\x1bH\x1b[?5W\r\tZ"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	decst8c := firstDamageControl(damage, "decst8c")
	ht := firstDamageControl(damage, "ht")
	if decst8c.Control != "decst8c" || decst8c.Mode != 5 || ht.Control != "ht" || ht.Col != 8 {
		t.Fatalf("tab reset raw should expose DECST8C and vterm-resolved HT semantic controls, decst8c=%#v ht=%#v damage=%#v", decst8c, ht, damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 4 || stats.WriteSpanOps == 0 {
		t.Fatalf("tab reset raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "ab      Z" {
		t.Fatalf("tab reset semantic projector should use vterm default tabstop state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesEraseCharacterRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 3, 100, nil)
	raw := "ABCDE\x1b[2G\x1b[2X"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "ech").Control != "ech" {
		t.Fatalf("erase character raw should expose vterm ECH semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("erase character raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "A  DE" {
		t.Fatalf("erase character semantic projector should blank exact range, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesDeleteCharacterRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 3, 100, nil)
	raw := "ABCDE\x1b[2G\x1b[2P"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "dch").Control != "dch" {
		t.Fatalf("delete character raw should expose vterm DCH semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("delete character raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "ADE" {
		t.Fatalf("delete character semantic projector should shift cells left, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesInsertCharacterRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(12, 3, 100, nil)
	raw := "ABCDE\x1b[2G\x1b[2@"
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "ich").Control != "ich" {
		t.Fatalf("insert character raw should expose vterm ICH semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("insert character raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "A  BCDE" {
		t.Fatalf("insert character semantic projector should shift cells right, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesRepeatCharacterRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "AB\x1b[3bC"
	term := vterm.New(12, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainText(damage.SemanticOps, "B") {
		t.Fatalf("REP raw should expose repeated vterm text semantic ops, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("repeat character raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "ABBBBC" {
		t.Fatalf("REP semantic projector should repeat previous character, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesInBandResizeModeRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?2048hresize\x1b[?2048l"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 2048).Code != vterm.ScreenOpModes {
		t.Fatalf("in-band resize mode should be exposed as vterm mode op, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
		t.Fatalf("in-band resize raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if window.TotalLines != 0 || !historyWindowContainsText(window, "resize") {
		t.Fatalf("in-band resize mode should stay vterm-owned no-op while text remains visible, total=%d rows=%#v damage=%#v", window.TotalLines, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesInputReportPrivateModesRawWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode int
		text string
	}{
		{name: "backarrow-key", mode: 67, text: "backarrow"},
		{name: "light-dark-report", mode: 2031, text: "theme"},
		{name: "win32-input", mode: 9001, text: "win32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.SemanticProjectors += next.SemanticProjectors
				stats.RawFallbacks += next.RawFallbacks
				stats.ModeOps += next.ModeOps
				stats.WriteSpanOps += next.WriteSpanOps
			}
			defer resetTerminalSemanticIngestTestHooks()

			raw := "\x1b[?" + strconv.Itoa(tc.mode) + "h" + tc.text + "\x1b[?" + strconv.Itoa(tc.mode) + "l"
			term := vterm.New(16, 3, 100, nil)
			_, err, damage := term.WriteWithDamage([]byte(raw))
			if err != nil {
				t.Fatalf("vterm write: %v", err)
			}
			if firstDamageMode(damage, tc.mode).Code != vterm.ScreenOpModes {
				t.Fatalf("input/report mode should be exposed as vterm mode op, mode=%d damage=%#v", tc.mode, damage)
			}
			pipeline := newTerminalHistoryPipeline(16, 3)
			batch := terminalSemanticBatch{
				Raw:             raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            16,
				Rows:            3,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch: %v", err)
			}
			if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps < 2 || stats.WriteSpanOps == 0 {
				t.Fatalf("input/report private mode raw should use vterm semantic projector without parser fallback, mode=%d stats=%#v damage=%#v", tc.mode, stats, damage)
			}
			window, err := pipeline.LatestWindow(16, 4)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			if window.TotalLines != 0 || !historyWindowContainsText(window, tc.text) {
				t.Fatalf("input/report mode should stay vterm-owned no-op while text remains visible, mode=%d total=%d rows=%#v damage=%#v", tc.mode, window.TotalLines, window.Rows, damage)
			}
		})
	}
}

func TestTerminalSemanticProjectorRejectsUnknownPrivateModeRaw(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b[?7777hunknown"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageMode(damage, 7777).Code != vterm.ScreenOpModes {
		t.Fatalf("unknown private mode should still be visible as vterm mode op, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors != 0 || stats.RawFallbacks == 0 {
		t.Fatalf("unknown private mode should keep parser fallback boundary, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "unknown") {
		t.Fatalf("fallback should still preserve raw text, rows=%#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesSpecialDrawingCharsetRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b(0lqk\x1b(Bok"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	for _, want := range []string{"┌", "─", "┐"} {
		if !damageOpsContainText(damage.SemanticOps, want) {
			t.Fatalf("SCS raw should expose mapped vterm text %q, damage=%#v", want, damage)
		}
	}
	if firstDamageControl(damage, "scs").Control != "scs" {
		t.Fatalf("SCS raw should expose ordered charset designation control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("SCS raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "┌─┐ok" {
		t.Fatalf("SCS semantic projector should use vterm mapped glyphs, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesLockingShiftCharsetRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b)0\x0eq\x0fq"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainText(damage.SemanticOps, "─") {
		t.Fatalf("locking shift raw should expose mapped vterm text, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("locking shift raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "─q" {
		t.Fatalf("locking shift semantic projector should use vterm GL charset state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesG2G3LockingShiftRawWithoutFallback(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "ls2", raw: "\x1b*0\x1bnq\x0fq"},
		{name: "ls3", raw: "\x1b+0\x1boq\x0fq"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.SemanticProjectors += next.SemanticProjectors
				stats.RawFallbacks += next.RawFallbacks
				stats.WriteSpanOps += next.WriteSpanOps
			}
			defer resetTerminalSemanticIngestTestHooks()

			term := vterm.New(16, 3, 100, nil)
			_, err, damage := term.WriteWithDamage([]byte(tt.raw))
			if err != nil {
				t.Fatalf("vterm write: %v", err)
			}
			if !damageOpsContainText(damage.SemanticOps, "─") {
				t.Fatalf("G2/G3 locking shift raw should expose mapped vterm text, damage=%#v", damage)
			}
			pipeline := newTerminalHistoryPipeline(16, 3)
			batch := terminalSemanticBatch{
				Raw:             tt.raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            16,
				Rows:            3,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch: %v", err)
			}
			if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
				t.Fatalf("G2/G3 locking shift raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
			}
			window, err := pipeline.LatestWindow(16, 4)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			if len(window.Rows) != 1 || window.Rows[0].Text != "─q" {
				t.Fatalf("G2/G3 locking shift semantic projector should use vterm GL charset state, got %#v damage=%#v", window.Rows, damage)
			}
		})
	}
}

func TestTerminalSemanticProjectorConsumesSingleShiftCharsetRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "\x1b*0\x1bNqq"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if !damageOpsContainText(damage.SemanticOps, "─") {
		t.Fatalf("single shift raw should expose mapped vterm text, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("single shift raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "─q" {
		t.Fatalf("single shift semantic projector should use vterm one-shot charset state, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesResetInitialStateRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "abc\x1bcZ"
	term := vterm.New(16, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	if firstDamageControl(damage, "ris").Code != vterm.ScreenOpControl {
		t.Fatalf("RIS raw should expose reset semantic control, damage=%#v", damage)
	}
	pipeline := newTerminalHistoryPipeline(16, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            16,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("RIS raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(16, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "Z" {
		t.Fatalf("RIS semantic projector should reset mutable frontier before later text, got %#v damage=%#v", window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesSaveRestoreCursorRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ControlOps += next.ControlOps
		stats.WriteSpanOps += next.WriteSpanOps
	}
	defer resetTerminalSemanticIngestTestHooks()

	raw := "abc\x1b7\x1b[2;5HZZ\x1b8X"
	term := vterm.New(12, 3, 100, nil)
	_, err, damage := term.WriteWithDamage([]byte(raw))
	if err != nil {
		t.Fatalf("vterm write: %v", err)
	}
	pipeline := newTerminalHistoryPipeline(12, 3)
	batch := terminalSemanticBatch{
		Raw:             raw,
		Damages:         []vterm.WriteDamage{damage},
		Cols:            12,
		Rows:            3,
		FromSharedVTerm: true,
	}
	if err := pipeline.IngestSemanticBatch(batch); err != nil {
		t.Fatalf("ingest semantic batch: %v", err)
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
		t.Fatalf("save/restore cursor raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
	}
	window, err := pipeline.LatestWindow(12, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if !strings.Contains(text, "abcX") || !strings.Contains(text, "ZZ") || strings.Contains(text, "abcZZX") {
		t.Fatalf("save/restore cursor semantic projector should use restored vterm write positions, got %q rows=%#v damage=%#v", text, window.Rows, damage)
	}
}

func TestTerminalSemanticProjectorConsumesLineOperationsRawWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		raw      string
		control  string
		expected []string
	}{
		{
			name:     "insert-line",
			raw:      "\x1b[2;1H\x1b[1Lafter",
			control:  "il",
			expected: []string{"top", "after", "middle"},
		},
		{
			name:     "delete-line",
			raw:      "\x1b[2;1H\x1b[1MAFTER!",
			control:  "dl",
			expected: []string{"top", "AFTER!"},
		},
		{
			name:     "scroll-up",
			raw:      "\x1b[1S\x1b[3;1Hafter",
			control:  "su",
			expected: []string{"top", "middle", "bottom", "after"},
		},
		{
			name:     "scroll-down",
			raw:      "\x1b[1T\x1b[1;1Hafter",
			control:  "sd",
			expected: []string{"after", "top", "middle"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := vterm.New(12, 3, 100, nil)
			pipeline := newTerminalHistoryPipeline(12, 3)
			seedRaw := "top\r\nmiddle\r\nbottom"
			_, err, seedDamage := term.WriteWithDamage([]byte(seedRaw))
			if err != nil {
				t.Fatalf("seed vterm write: %v", err)
			}
			if err := pipeline.IngestSemanticBatch(terminalSemanticBatch{
				Raw:             seedRaw,
				Damages:         []vterm.WriteDamage{seedDamage},
				Cols:            12,
				Rows:            3,
				FromSharedVTerm: true,
			}); err != nil {
				t.Fatalf("seed semantic batch: %v", err)
			}

			resetTerminalSemanticIngestTestHooks()
			var stats terminalSemanticProjectorStats
			terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
				stats.SemanticProjectors += next.SemanticProjectors
				stats.RawFallbacks += next.RawFallbacks
				stats.ControlOps += next.ControlOps
				stats.WriteSpanOps += next.WriteSpanOps
			}
			defer resetTerminalSemanticIngestTestHooks()

			_, err, damage := term.WriteWithDamage([]byte(tc.raw))
			if err != nil {
				t.Fatalf("vterm write: %v", err)
			}
			if firstDamageControl(damage, tc.control).Control != tc.control {
				t.Fatalf("line operation raw should expose vterm %s semantic control, damage=%#v", tc.control, damage)
			}
			batch := terminalSemanticBatch{
				Raw:             tc.raw,
				Damages:         []vterm.WriteDamage{damage},
				Cols:            12,
				Rows:            3,
				FromSharedVTerm: true,
			}
			if err := pipeline.IngestSemanticBatch(batch); err != nil {
				t.Fatalf("ingest semantic batch: %v", err)
			}
			if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ControlOps == 0 || stats.WriteSpanOps == 0 {
				t.Fatalf("line operation raw should use vterm semantic projector without parser fallback, stats=%#v damage=%#v", stats, damage)
			}
			window, err := pipeline.LatestWindow(12, 6)
			if err != nil {
				t.Fatalf("latest: %v", err)
			}
			text := historyWindowJoinedText(window)
			for _, want := range tc.expected {
				if strings.Count(text, want) != 1 {
					t.Fatalf("%s semantic projector should contain %q once, got %q rows=%#v damage=%#v", tc.control, want, text, window.Rows, damage)
				}
			}
			for _, unexpected := range unexpectedLineOperationTexts(tc.expected) {
				if strings.Contains(text, unexpected) {
					t.Fatalf("%s semantic projector should not contain %q, got %q rows=%#v damage=%#v", tc.control, unexpected, text, window.Rows, damage)
				}
			}
		})
	}
}

func TestTerminalSemanticProjectorConsumesAltScreenRunningRawWithoutFallback(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.SemanticProjectors += next.SemanticProjectors
		stats.RawFallbacks += next.RawFallbacks
		stats.ModeOps += next.ModeOps
		stats.WriteSpanOps += next.WriteSpanOps
		stats.AlternateAppends += next.AlternateAppends
	}
	defer resetTerminalSemanticIngestTestHooks()

	term := vterm.New(20, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(20, 3)
	for _, raw := range []string{"primary\n", "\x1b[?1049halt-running", "\nalt-scroll-one\nalt-scroll-two\nalt-scroll-three"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            20,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	if stats.SemanticProjectors == 0 || stats.RawFallbacks != 0 || stats.ModeOps == 0 || stats.WriteSpanOps == 0 || stats.AlternateAppends == 0 {
		t.Fatalf("alt-screen running raw should use vterm semantic projector without parser fallback, stats=%#v", stats)
	}
	window, err := pipeline.LatestWindow(20, 5)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !historyWindowContainsText(window, "primary") || historyWindowContainsText(window, "alt-running") || historyWindowContainsText(window, "alt-scroll") {
		t.Fatalf("alt-screen running content must not enter primary history, rows=%#v", window.Rows)
	}
}

func TestTerminalSemanticProjectorUsesSharedVTermAltExitFrameOnce(t *testing.T) {
	resetTerminalSemanticIngestTestHooks()
	var stats terminalSemanticProjectorStats
	terminalSemanticProjectorHook = func(next terminalSemanticProjectorStats) {
		stats.ModeOps += next.ModeOps
		stats.AltExitFrames += next.AltExitFrames
	}
	defer resetTerminalSemanticIngestTestHooks()

	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-alt-shared",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-alt-shared", "primary\n\x1b[?1049h\x1b[2Jalt-final\x1b[?1049l"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if stats.AltExitFrames != 1 {
		t.Fatalf("shared vterm alt final frame should reach projector once, stats=%#v", stats)
	}
	window, err := server.LatestWindow("term-alt-shared", 20, 10)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	text := historyWindowJoinedText(window)
	if strings.Count(text, "alt-final") != 1 || strings.Count(text, "primary") != 1 {
		t.Fatalf("alt final frame should be appended once from shared vterm batch, got %q rows=%#v", text, window.Rows)
	}
	if window.TotalLines != 2 {
		t.Fatalf("alt final frame should commit exactly once with primary page, total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalSemanticProjectorShadowParsesRawTextState(t *testing.T) {
	term := vterm.New(20, 3, 100, nil)
	pipeline := newTerminalHistoryPipeline(20, 3)
	for _, raw := range []string{"\x1b[", "31mred ", "tail\x1b[0m"} {
		_, err, damage := term.WriteWithDamage([]byte(raw))
		if err != nil {
			t.Fatalf("vterm write %q: %v", raw, err)
		}
		batch := terminalSemanticBatch{
			Raw:             raw,
			Damages:         []vterm.WriteDamage{damage},
			Cols:            20,
			Rows:            3,
			FromSharedVTerm: true,
		}
		if err := pipeline.IngestSemanticBatch(batch); err != nil {
			t.Fatalf("ingest semantic batch %q: %v", raw, err)
		}
	}
	window, err := pipeline.LatestWindow(20, 4)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "red tail" {
		t.Fatalf("vterm semantic text should own raw text projection, got %#v", window.Rows)
	}
	for _, cell := range window.Rows[0].Cells {
		if cell.Style.FG != "ansi:1" {
			t.Fatalf("shadow parser must not corrupt vterm styled text projection, got %#v", window.Rows[0].Cells)
		}
	}
	if len(window.Rows[0].Cells) == 0 {
		t.Fatalf("shadow parser must not corrupt vterm styled text projection, got %#v", window.Rows[0].Cells)
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

func firstDamageControl(damage vterm.WriteDamage, control string) vterm.DamageOp {
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpControl && op.Control == control {
			return op
		}
	}
	return vterm.DamageOp{}
}

func damageOpsContainText(ops []vterm.DamageOp, text string) bool {
	for _, op := range ops {
		if op.Code != vterm.ScreenOpWriteSpan {
			continue
		}
		for _, cell := range op.Cells {
			if cell.Content == text {
				return true
			}
		}
	}
	return false
}

func damageOpsContainTextSpan(ops []vterm.DamageOp, text string) bool {
	for _, op := range ops {
		if op.Code != vterm.ScreenOpWriteSpan {
			continue
		}
		var b strings.Builder
		for _, cell := range op.Cells {
			b.WriteString(cell.Content)
		}
		if strings.Contains(b.String(), text) {
			return true
		}
	}
	return false
}

func unexpectedLineOperationTexts(expected []string) []string {
	candidates := []string{"top", "middle", "bottom", "after", "AFTER!"}
	unexpected := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		found := false
		for _, want := range expected {
			if candidate == want {
				found = true
				break
			}
		}
		if !found {
			unexpected = append(unexpected, candidate)
		}
	}
	return unexpected
}

func firstDamageMode(damage vterm.WriteDamage, mode int) vterm.DamageOp {
	for _, op := range damage.SemanticOps {
		if op.Code == vterm.ScreenOpModes && op.Mode == mode {
			return op
		}
	}
	return vterm.DamageOp{}
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
