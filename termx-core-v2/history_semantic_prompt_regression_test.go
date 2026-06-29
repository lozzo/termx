package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
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

func TestR334SyncEndAndPromptInSameTransactionDoesNotRepublishScreen(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r334-sync-end-prompt",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r334-sync-end-prompt",
		"\x1b[?2026h\x1b[2J\x1b[H"+
			"S03 099/100 | seq=099\r\n"+
			"S03 100/100 | seq=100 | 中文编号100中文",
	); err != nil {
		t.Fatalf("seed active primary frame: %v", err)
	}
	terminal, err := server.Terminal("term-r334-sync-end-prompt")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}

	tx := history.TerminalSemanticTransaction{
		Seq:                99,
		Size:               history.TerminalSemanticSize{Cols: 40, Rows: 6},
		SynchronizedEnd:    true,
		SynchronizedActive: false,
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: false},
			{
				Code:  vterm.ScreenOpWriteSpan,
				Row:   3,
				Col:   0,
				Cells: historyCellsForRegression("PROMPT_AFTER"),
			},
		},
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 40,
			Rows: [][]history.TerminalSemanticCell{
				historyCellsForRegression("S03 099/100 | seq=099"),
				historyCellsForRegression("S03 100/100 | seq=100 | 中文编号100中文"),
				nil,
				historyCellsForRegression("PROMPT_AFTER"),
			},
		},
	}

	terminal.historyMu.Lock()
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply sync-end prompt transaction: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store sync-end prompt transaction: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r334-sync-end-prompt", 40, 3)
	if historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("sync end followed by prompt must close current frame, not republish final screen, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("S03 final row should appear once, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("prompt after sync end should enter ordinary history once, count=%d rows=%#v", got, rows)
	}
}

func TestR334FullReplacePromptAfterPrimaryFrameDoesNotRepublishScreen(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r334-full-replace-prompt",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r334-full-replace-prompt",
		"\x1b[?2026h\x1b[2J\x1b[H"+
			"S03 099/100 | seq=099\r\n"+
			"S03 100/100 | seq=100 | 中文编号100中文"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}
	terminal, err := server.Terminal("term-r334-full-replace-prompt")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}

	tx := history.TerminalSemanticTransaction{
		Seq:                 99,
		Size:                history.TerminalSemanticSize{Cols: 40, Rows: 6},
		RequiresFullReplace: true,
		FullReplaceReason:   "broad_direct_cell_damage",
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 40,
			Rows: [][]history.TerminalSemanticCell{
				historyCellsForRegression("S03 099/100 | seq=099"),
				historyCellsForRegression("S03 100/100 | seq=100 | 中文编号100中文"),
				nil,
				historyCellsForRegression("PROMPT_AFTER"),
			},
		},
	}

	terminal.historyMu.Lock()
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply prompt full replace: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store prompt full replace: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r334-full-replace-prompt", 40, 3)
	if historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("ordinary prompt full-replace damage must not republish the whole screen as current frame, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("final S03 screen row should appear once after prompt, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 0 {
		t.Fatalf("full-replace side proof must not invent ordinary prompt history without ordered ops, count=%d rows=%#v", got, rows)
	}
}

func TestR334FullReplacePromptWithOrderedOpsClosesFrameAndKeepsPrompt(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r334-full-replace-prompt-ops",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r334-full-replace-prompt-ops",
		"\x1b[?2026h\x1b[2J\x1b[H"+
			"S03 099/100 | seq=099\r\n"+
			"S03 100/100 | seq=100 | 中文编号100中文"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}
	terminal, err := server.Terminal("term-r334-full-replace-prompt-ops")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}

	tx := history.TerminalSemanticTransaction{
		Seq:                 99,
		Size:                history.TerminalSemanticSize{Cols: 40, Rows: 6},
		RequiresFullReplace: true,
		FullReplaceReason:   "broad_direct_cell_damage",
		Ops: []history.TerminalSemanticOp{{
			Code:  vterm.ScreenOpWriteSpan,
			Row:   3,
			Col:   0,
			Cells: historyCellsForRegression("PROMPT_AFTER"),
		}},
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 40,
			Rows: [][]history.TerminalSemanticCell{
				historyCellsForRegression("S03 099/100 | seq=099"),
				historyCellsForRegression("S03 100/100 | seq=100 | 中文编号100中文"),
				nil,
				historyCellsForRegression("PROMPT_AFTER"),
			},
		},
	}

	terminal.historyMu.Lock()
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply prompt full replace: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store prompt full replace: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r334-full-replace-prompt-ops", 40, 3)
	if historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("ordered ordinary prompt must close old primary frame instead of republishing screen, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("final S03 screen row should appear once after prompt, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("ordered prompt should enter ordinary history once, count=%d rows=%#v", got, rows)
	}
}

func TestR334ED0PromptAfterPrimaryFrameDoesNotRepublishScreen(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r334-ed0-prompt",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r334-ed0-prompt",
		"\x1b[?2026h\x1b[2J\x1b[H"+
			"S03 099/100 | seq=099\r\n"+
			"S03 100/100 | seq=100 | 中文编号100中文"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}
	terminal, err := server.Terminal("term-r334-ed0-prompt")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}

	tx := history.TerminalSemanticTransaction{
		Seq:  99,
		Size: history.TerminalSemanticSize{Cols: 40, Rows: 6},
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 0, Row: 3, Col: 0},
			{
				Code:  vterm.ScreenOpWriteSpan,
				Row:   3,
				Col:   0,
				Cells: historyCellsForRegression("PROMPT_AFTER"),
			},
		},
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 40,
			Rows: [][]history.TerminalSemanticCell{
				historyCellsForRegression("S03 099/100 | seq=099"),
				historyCellsForRegression("S03 100/100 | seq=100 | 中文编号100中文"),
				nil,
				historyCellsForRegression("PROMPT_AFTER"),
			},
		},
	}

	terminal.historyMu.Lock()
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("apply ED0 prompt transaction: %v", err)
	}
	if err := terminal.historyStore.Apply(batch); err != nil {
		terminal.historyMu.Unlock()
		t.Fatalf("store ED0 prompt transaction: %v", err)
	}
	terminal.historyMu.Unlock()

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r334-ed0-prompt", 40, 3)
	if historyRowsContainSegment(rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("ED0 prompt must not be classified as screen repaint, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("S03 final row should appear once, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("ED0 prompt should enter ordinary history once, count=%d rows=%#v", got, rows)
	}
}
