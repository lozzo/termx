package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

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
	if !historyRowsContainCurrentScreenFrame(rows) {
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

func TestR333RepeatedED2SmallUIPaintsDoNotMultiplyHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r333-ed2-small-ui",
		Command: []string{"shell"},
		Size:    Size{Cols: 64, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for i := 0; i < 4; i++ {
		payload := "\x1b[?2026h\x1b[2J\x1b[H" +
			"Update available! 0.142.2 -> 0.142.3\r\n\r\n" +
			"Release notes: https://github.com/openai/codex/releases/latest\r\n\r\n" +
			"1. Update now\r\n" +
			"2. Skip\r\n" +
			"3. Skip until next version\r\n\r\n" +
			"Press enter to continue" +
			"\x1b[?2026l"
		if err := server.IngestOutput(context.Background(), "term-r333-ed2-small-ui", payload); err != nil {
			t.Fatalf("ingest repaint %d: %v", i, err)
		}
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r333-ed2-small-ui", 64, 4)
	joined := strings.Join(historyRowTexts(rows), "\n")
	if got := strings.Count(joined, "Update available!"); got != 4 {
		t.Fatalf("ED2 is scrollback-preserving; each cleared primary frame plus latest frame should appear once, count=%d:\n%s\nrows=%#v", got, joined, rows)
	}
	if got := len(currentPrimaryFrameRowTexts(rows)); got == 0 {
		t.Fatalf("latest repaint should still leave a current primary frame, rows=%#v", rows)
	}
}

func TestR336ED2ClearPreservesPreviousPrimaryFrameInScrollableHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r336-ed2-primary-scrollback",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r336-ed2-primary-scrollback",
		"\x1b[?2026h"+
			"before clear 1\r\n"+
			"before clear 2"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r336-ed2-primary-scrollback",
		"\x1b[?2026h\x1b[H\x1b[2J"+
			"after clear only"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("ingest ED2 clear redraw: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r336-ed2-primary-scrollback", 40, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"before clear 1", "before clear 2", "after clear only"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ED2 clear must keep %q in authoritative history/curr frame:\n%s\nrows=%#v", want, text, rows)
		}
	}
	committed := strings.Join(committedHistoryRowTexts(rows), "\n")
	for _, want := range []string{"before clear 1", "before clear 2"} {
		if got := strings.Count(committed, want); got != 1 {
			t.Fatalf("clear-time primary frame history must appear once in sealed timeline, %q count=%d:\n%s\nrows=%#v", want, got, committed, rows)
		}
	}
	if got := historyTextCount(rows, "after clear only"); got != 1 {
		t.Fatalf("post-clear primary frame should appear once, count=%d:\n%s\nrows=%#v", got, text, rows)
	}
}

func TestR339CodexClearKeepsShellAndPreviousPrimaryHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r339-codex-clear-iterm2",
		Command: []string{"shell"},
		Size:    Size{Cols: 44, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r339-codex-clear-iterm2",
		"shell history 1\r\n"+
			"shell history 2\r\n",
	); err != nil {
		t.Fatalf("seed shell history: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r339-codex-clear-iterm2",
		"\x1b[?2026h"+
			"codex before clear 1\r\n"+
			"codex before clear 2"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed codex primary frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r339-codex-clear-iterm2",
		"\x1b[?2026h\x1b[H\x1b[2J"+
			"codex after clear only"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("ingest codex /clear repaint: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r339-codex-clear-iterm2", 44, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{
		"shell history 1",
		"shell history 2",
		"codex before clear 1",
		"codex before clear 2",
		"codex after clear only",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("iTerm2-style /clear history missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	committed := strings.Join(committedHistoryRowTexts(rows), "\n")
	for _, want := range []string{"shell history 1", "shell history 2", "codex before clear 1", "codex before clear 2"} {
		if got := strings.Count(committed, want); got != 1 {
			t.Fatalf("/clear must keep %q exactly once in scrollable history, count=%d:\n%s\nrows=%#v", want, got, committed, rows)
		}
	}
	current := strings.Join(currentPrimaryFrameRowTexts(rows), "\n")
	if current != "codex after clear only" {
		t.Fatalf("live/current primary frame after /clear should only contain post-clear Codex screen, got %q rows=%#v", current, rows)
	}
}

func TestR340CodexClearWithED2AndED3CreatesNewPageWithoutDroppingHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r340-codex-ed2-ed3-clear",
		Command: []string{"shell"},
		Size:    Size{Cols: 44, Rows: 6},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r340-codex-ed2-ed3-clear",
		"shell page before codex\r\n",
	); err != nil {
		t.Fatalf("seed shell history: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r340-codex-ed2-ed3-clear",
		"\x1b[?2026h"+
			"codex page before clear 1\r\n"+
			"codex page before clear 2"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("seed codex primary frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r340-codex-ed2-ed3-clear",
		"\x1b[?2026h\x1b[H\x1b[2J\x1b[3J"+
			"codex page after clear"+
			"\x1b[?2026l",
	); err != nil {
		t.Fatalf("ingest codex ED2+ED3 clear repaint: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r340-codex-ed2-ed3-clear", 44, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{
		"shell page before codex",
		"codex page before clear 1",
		"codex page before clear 2",
		"codex page after clear",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ED2+ED3 clear must create a new page without dropping %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	committed := strings.Join(committedHistoryRowTexts(rows), "\n")
	for _, want := range []string{"shell page before codex", "codex page before clear 1", "codex page before clear 2"} {
		if got := strings.Count(committed, want); got != 1 {
			t.Fatalf("clear-scrollback soft boundary must keep prior page exactly once, %q count=%d:\n%s\nrows=%#v", want, got, committed, rows)
		}
	}
	if current := strings.Join(currentPrimaryFrameRowTexts(rows), "\n"); current != "codex page after clear" {
		t.Fatalf("post-clear page should remain current primary frame, got %q rows=%#v", current, rows)
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
	if historyRowsContainCurrentScreenFrame(window.Rows) {
		t.Fatalf("synchronized begin without repaint payload must not publish existing shell screen as current frame, rows=%#v", window.Rows)
	}
}

func TestR333SynchronizedEndAloneDoesNotRepublishCurrentFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r333-sync-end",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-sync-end", "\x1b[?2026hcurrent\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized payload: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r333-sync-end", "\x1b[?2026h\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized end-only boundary: %v", err)
	}

	rows, _ := r326CollectAllHistoryRows(t, server, "term-r333-sync-end", 24, 2)
	if got := historyTextCount(rows, "current"); got != 1 {
		t.Fatalf("sync end-only boundary must not republish current frame, count=%d rows=%#v", got, rows)
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
	if historyRowsContainCurrentScreenFrame(rows) {
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
	if historyRowsContainCurrentScreenFrame(rows) {
		t.Fatalf("ordered ordinary prompt must close old primary frame instead of republishing screen, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("final S03 screen row should appear once after prompt, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("ordered prompt should enter ordinary history once, count=%d rows=%#v", got, rows)
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
	if historyRowsContainCurrentScreenFrame(rows) {
		t.Fatalf("sync end followed by prompt must close current frame, not republish final screen, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("S03 final row should appear once, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("prompt after sync end should enter ordinary history once, count=%d rows=%#v", got, rows)
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
	if historyRowsContainCurrentScreenFrame(rows) {
		t.Fatalf("ED0 prompt must not be classified as screen repaint, rows=%#v", rows)
	}
	if got := historyTextCount(rows, "S03 100/100"); got != 1 {
		t.Fatalf("S03 final row should appear once, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "PROMPT_AFTER"); got != 1 {
		t.Fatalf("ED0 prompt should enter ordinary history once, count=%d rows=%#v", got, rows)
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
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame && row.Kind == history.LineKindScreenFrame {
			out = append(out, historyCellsText(row.Cells))
		}
	}
	return out
}

func historyRowsContainCurrentScreenFrame(rows []history.HistoryRow) bool {
	for _, row := range rows {
		if row.Segment == history.HistorySegmentCurrentPrimaryFrame && row.Kind == history.LineKindScreenFrame {
			return true
		}
	}
	return false
}

func historyCellsForRegression(text string) []history.TerminalSemanticCell {
	cells := make([]history.TerminalSemanticCell, 0, len([]rune(text)))
	for _, r := range text {
		cells = append(cells, history.TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return cells
}
