package termxcorev2

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
)

func TestTerminalLifecycleAndPipeline(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 3},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if info.State != TerminalStateRunning {
		t.Fatalf("unexpected state %q", info.State)
	}
	process := factory.process("term-1")
	if process == nil {
		t.Fatal("expected process to be spawned")
	}
	if err := server.WriteInput(context.Background(), "term-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inputs, _, _, _ := process.snapshot()
	if got := inputs[0]; string(got) != "echo hi\n" {
		t.Fatalf("unexpected input %q", string(got))
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\nworld"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) != 2 || rows[0] != "hello" || !strings.Contains(rows[1], "world") {
		t.Fatalf("unexpected live rows %#v", rows)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "hello" || window.Rows[1].Text != "world" {
		t.Fatalf("unexpected history window %#v", window)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 20, 5); err != nil {
		t.Fatalf("resize: %v", err)
	}
	_, resizes, _, _ := process.snapshot()
	if got := resizes[0]; got != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("unexpected resize %#v", got)
	}
	info, err = server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal after resize: %v", err)
	}
	if info.Size != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("expected registry size to follow resize, got %#v", info.Size)
	}
}

func TestTerminalIngestOutputPublishesLiveChangedEvent(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalChanged}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "live update\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	event := assertEventValue(t, events, EventTerminalChanged, "term-1")
	if event.Terminal == nil || event.Terminal.State != TerminalStateRunning {
		t.Fatalf("expected running terminal info on live changed event, got %#v", event)
	}
}

func TestTerminalIngestOutputNormalizesPTYCRLF(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) < 2 || rows[0] != "alpha" || rows[1] != "beta" {
		t.Fatalf("expected CRLF-normalized live rows, got %#v", rows)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "alpha" || window.Rows[1].Text != "beta" {
		t.Fatalf("expected CRLF-normalized history rows, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputNewlineOnlySealsUntilLineLeavesPrimaryScreen(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("lookup terminal: %v", err)
	}
	terminal.mu.Lock()
	committed := terminal.history.CommittedIDs()
	frontier := terminal.history.FrontierIDs()
	committable := terminal.history.CommittableIDs()
	terminal.mu.Unlock()

	if got := committed; len(got) != 1 || got[0] != 1 {
		t.Fatalf("only oldest sealed line should be committed after scroll-out, got %v", got)
	}
	if got := frontier; len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("newer sealed lines should remain in frontier ownership, got %v", got)
	}
	if len(committable) != 0 {
		t.Fatalf("after commit, visible sealed lines must not remain committable, got %v", committable)
	}
}

func TestTerminalIngestOutputCarriageReturnOverwritesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\rT"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "Tne" {
		t.Fatalf("history latest should reflect CR overwrite in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("carriage return overwrite must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputEraseInLineMutatesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\rhe\x1b[K"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || strings.TrimRight(window.Rows[0].Text, " ") != "he" {
		t.Fatalf("history latest should reflect EL mutation in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("erase-in-line must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputClearScreenResetsMutableFrontierOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	before, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest before clear: %v", err)
	}
	if before.TotalLines == 0 {
		t.Fatalf("expected committed history before clear, got %#v", before)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[2J"); err != nil {
		t.Fatalf("ingest clear screen: %v", err)
	}
	after, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after clear: %v", err)
	}
	if len(after.Rows) != 1 || after.Rows[0].Text != "one" || after.TotalLines != before.TotalLines {
		t.Fatalf("ED 2 should clear mutable frontier but preserve committed history, got %#v", after)
	}
}

func TestTerminalIngestOutputClearScrollbackTruncatesCommittedHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("ingest clear scrollback: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after clear scrollback: %v", err)
	}
	if len(window.Rows) != 3 || window.Rows[0].Text != "two" || window.Rows[1].Text != "three" || window.Rows[2].Text != "four" || window.TotalLines != 0 {
		t.Fatalf("ED 3 should clear committed history but keep mutable tail, got %#v", window)
	}
}

func TestTerminalIngestOutputPreservesANSIStylesInHistoryCells(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "\x1b[1;31mERR\x1b[0m \x1b[4;38;2;255;204;0m好\x1b[0m \x1b[48;5;12mBG\x1b[49m\nplain"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "ERR 好 BG" || window.Rows[1].Text != "plain" {
		t.Fatalf("unexpected styled history rows %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 5 {
		t.Fatalf("expected styled runs to survive ingest, got %#v", cells)
	}
	if cells[0].Text != "ERR" || cells[0].Width != 3 || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("expected red bold ERR cell, got %#v", cells[0])
	}
	if cells[2].Text != "好" || cells[2].Width != 2 || cells[2].Style.FG != "#ffcc00" || !cells[2].Style.Underline {
		t.Fatalf("expected truecolor underlined wide cell, got %#v", cells[2])
	}
	if cells[4].Text != "BG" || cells[4].Style.BG != "idx:12" || cells[4].Style.FG != "" {
		t.Fatalf("expected indexed background cell with reset foreground, got %#v", cells[4])
	}
	if window.Rows[1].Cells[0].Text != "plain" || window.Rows[1].Cells[0].Style != (history.CellStyle{}) {
		t.Fatalf("expected SGR reset to keep following line plain, got %#v", window.Rows[1].Cells)
	}
}

func TestTerminalIngestOutputCarriesANSIStateAcrossChunks(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, chunk := range []string{"\x1b[", "31mred ", "tail\x1b[0m\n"} {
		if err := server.IngestOutput(context.Background(), "term-1", chunk); err != nil {
			t.Fatalf("ingest output chunk %q: %v", chunk, err)
		}
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "red tail" {
		t.Fatalf("unexpected history rows %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 2 {
		t.Fatalf("expected same SGR style to carry across output chunks, got %#v", cells)
	}
	if cells[0].Text != "red " || cells[0].Style.FG != "ansi:1" || cells[1].Text != "tail" || cells[1].Style.FG != "ansi:1" {
		t.Fatalf("expected red style across chunks, got %#v", cells)
	}
}

func TestTerminalIngestOutputPreservesOSC8LinksAndSkipsControls(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "\x1b]2;ignored title\a\x1b]8;id=termx;https://example.test\a"
	output += "linked\x1b]8;;\aplain\x1b[?25l\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "linkedplain" {
		t.Fatalf("control sequences must not leak into history text, got %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 2 {
		t.Fatalf("expected link and plain cells, got %#v", cells)
	}
	if cells[0].Text != "linked" || cells[0].LinkURL != "https://example.test" || cells[0].LinkParams != "id=termx" {
		t.Fatalf("expected OSC 8 link metadata on first cell, got %#v", cells[0])
	}
	if cells[1].Text != "plain" || cells[1].LinkURL != "" || cells[1].LinkParams != "" {
		t.Fatalf("expected OSC 8 reset before plain text, got %#v", cells[1])
	}
}

func TestTerminalIngestOutputSkipsStringControlsAcrossChunks(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, chunk := range []string{"before", "\x1bPignored ", "payload\x1b\\after\n"} {
		if err := server.IngestOutput(context.Background(), "term-1", chunk); err != nil {
			t.Fatalf("ingest output chunk %q: %v", chunk, err)
		}
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "beforeafter" {
		t.Fatalf("DCS payload must not leak into history text, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputSGRTrailingDefaultResetsStyle(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[31;mplain\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || len(window.Rows[0].Cells) != 1 || window.Rows[0].Cells[0].Style != (history.CellStyle{}) {
		t.Fatalf("trailing empty SGR parameter should reset style, got %#v", window.Rows)
	}
}

func TestTerminalRestartResetsHistoryIngestParserState(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[31mred \x1b]8;id=old;https://old.test\alinked \x1b["); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "plain\n"); err != nil {
		t.Fatalf("ingest output after restart: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "plain" || len(window.Rows[0].Cells) != 1 {
		t.Fatalf("unexpected history after restart %#v", window.Rows)
	}
	cell := window.Rows[0].Cells[0]
	if cell.Style != (history.CellStyle{}) || cell.LinkURL != "" || cell.LinkParams != "" {
		t.Fatalf("restart should reset style/link/pending parser state, got %#v", cell)
	}
}

func TestTerminalResizeProcessFailureDoesNotChangeRegistryOrLiveSize(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	resizeErr := errors.New("resize failed")
	process.setResizeErr(resizeErr)
	if err := server.ResizeTerminal(context.Background(), "term-1", 20, 5); !errors.Is(err, resizeErr) {
		t.Fatalf("expected resize failure, got %v", err)
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.Size != (Size{Cols: 10, Rows: 3}) {
		t.Fatalf("expected registry size to remain unchanged, got %#v", info.Size)
	}
	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("get terminal handle: %v", err)
	}
	if got := terminal.live.Size(); got != (live.SurfaceSize{Cols: 10, Rows: 3}) {
		t.Fatalf("expected live size to remain unchanged, got %#v", got)
	}
}

func TestTerminalResizeAppliesHistoryDirection(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	beforeGrow, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest before grow: %v", err)
	}
	if beforeGrow.TotalLines != 0 || len(beforeGrow.Rows) != 2 || beforeGrow.Rows[0].Text != "one" || beforeGrow.Rows[1].Text != "two" {
		t.Fatalf("sealed visible lines should remain in live tail before grow, got %#v", beforeGrow)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 3); err != nil {
		t.Fatalf("grow resize: %v", err)
	}
	grown, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after grow: %v", err)
	}
	if grown.TotalLines != 0 || len(grown.Rows) != 2 || grown.Rows[0].Text != "one" || grown.Rows[1].Text != "two" {
		t.Fatalf("grow resize should reveal hidden/visible frontier without manufacturing committed rows, got %#v", grown)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 2); err != nil {
		t.Fatalf("shrink resize: %v", err)
	}
	shrunk, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after shrink: %v", err)
	}
	if shrunk.TotalLines != 0 || len(shrunk.Rows) != 1 || shrunk.Rows[0].Text != "two" {
		t.Fatalf("shrink resize should hide the oldest visible frontier row, got %#v", shrunk)
	}
}

func TestTerminalRestartReplacesProcessAndClearsLiveAndHistory(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	first := factory.process("term-1")
	if err := server.IngestOutput(context.Background(), "term-1", "before\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	_, _, _, firstClosed := first.snapshot()
	if !firstClosed {
		t.Fatal("expected old process to be closed")
	}
	second := factory.process("term-1")
	if second == nil || second == first {
		t.Fatal("expected replacement process")
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) != 1 || rows[0] != "" {
		t.Fatalf("expected live rows reset, got %#v", rows)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 0 {
		t.Fatalf("expected history reset after restart, got %#v", window)
	}
}

func TestTerminalExitForceCommitsOpenLineAndRejectsMutation(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	if err := server.IngestOutput(context.Background(), "term-1", "open-tail"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	process.exit(7)
	event := assertEventValue(t, events, EventTerminalExited, "term-1")
	if event.Terminal == nil || event.Terminal.ExitCode == nil || *event.Terminal.ExitCode != 7 {
		t.Fatalf("unexpected exit event %#v", event)
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.State != TerminalStateExited || info.ExitCode == nil || *info.ExitCode != 7 {
		t.Fatalf("unexpected terminal info after exit %#v", info)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "open-tail" || window.TotalLines != 1 {
		t.Fatalf("expected process exit to force commit open line, got %#v", window)
	}
	if err := server.WriteInput(context.Background(), "term-1", []byte("nope")); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for input, got %v", err)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 80, 24); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for resize, got %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "late-output"); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for late output, got %v", err)
	}
	window, err = server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window after late output: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "open-tail" || window.TotalLines != 1 {
		t.Fatalf("late output after exit must not create history, got %#v", window)
	}
}

func TestTerminalProcessDrainsOutputBeforeExit(t *testing.T) {
	factory := &exitBeforeOutputProcessFactory{}
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process
	process.exitThenOutput(0, "tail\n")
	event := assertEventValue(t, events, EventTerminalExited, "term-1")
	if event.Terminal == nil || event.Terminal.ExitCode == nil || *event.Terminal.ExitCode != 0 {
		t.Fatalf("unexpected exit event %#v", event)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "tail" || window.TotalLines != 1 {
		t.Fatalf("expected output produced before channel close to be committed before exit, got %#v", window)
	}
}

func TestTerminalKillAndRemoveCloseProcess(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	if err := server.KillTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}
	_, _, killed, _ := process.snapshot()
	if !killed {
		t.Fatal("expected process to be killed")
	}
	if err := server.RemoveTerminal("term-1"); err != nil {
		t.Fatalf("remove terminal: %v", err)
	}
	_, _, _, closed := process.snapshot()
	if !closed {
		t.Fatal("expected process to be closed")
	}
	if _, err := server.Terminal("term-1"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected ErrTerminalNotFound, got %v", err)
	}
}

func TestServerShutdownRejectsLaterTerminalRegistration(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", err)
	}
	if process := factory.process("term-1"); process != nil {
		t.Fatal("shutdown server must not spawn process for later registration")
	}
}

type recordingProcessFactory struct {
	mu        sync.Mutex
	processes map[string][]*recordingProcess
}

func newRecordingProcessFactory() *recordingProcessFactory {
	return &recordingProcessFactory{processes: make(map[string][]*recordingProcess)}
}

func (factory *recordingProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := &recordingProcess{
		id:       spec.TerminalID,
		outputCh: make(chan []byte, 16),
		waitCh:   make(chan ProcessExit, 1),
	}
	factory.mu.Lock()
	factory.processes[spec.TerminalID] = append(factory.processes[spec.TerminalID], process)
	factory.mu.Unlock()
	return process, nil
}

func (factory *recordingProcessFactory) process(id string) *recordingProcess {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	processes := factory.processes[id]
	if len(processes) == 0 {
		return nil
	}
	return processes[len(processes)-1]
}

type recordingProcess struct {
	mu         sync.Mutex
	id         string
	inputs     [][]byte
	resizes    []Size
	resizeErr  error
	outputCh   chan []byte
	waitCh     chan ProcessExit
	exitOnce   sync.Once
	outputOnce sync.Once
	killed     bool
	closed     bool
}

func (process *recordingProcess) Input(data []byte) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.inputs = append(process.inputs, append([]byte(nil), data...))
	return nil
}

func (process *recordingProcess) Resize(size Size) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	if process.resizeErr != nil {
		return process.resizeErr
	}
	process.resizes = append(process.resizes, size)
	return nil
}

func (process *recordingProcess) setResizeErr(err error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.resizeErr = err
}

func (process *recordingProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *recordingProcess) emitOutput(output string) {
	process.outputCh <- []byte(output)
}

func (process *recordingProcess) Kill() error {
	process.mu.Lock()
	process.killed = true
	process.mu.Unlock()
	process.exit(-1)
	return nil
}

func (process *recordingProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *recordingProcess) Close() error {
	process.mu.Lock()
	process.closed = true
	process.mu.Unlock()
	process.closeOutput()
	process.exit(-1)
	return nil
}

func (process *recordingProcess) snapshot() ([][]byte, []Size, bool, bool) {
	process.mu.Lock()
	defer process.mu.Unlock()
	inputs := make([][]byte, len(process.inputs))
	for i, input := range process.inputs {
		inputs[i] = append([]byte(nil), input...)
	}
	resizes := append([]Size(nil), process.resizes...)
	return inputs, resizes, process.killed, process.closed
}

func (process *recordingProcess) exit(code int) {
	process.exitOnce.Do(func() {
		process.closeOutput()
		process.waitCh <- ProcessExit{Code: code}
		close(process.waitCh)
	})
}

func (process *recordingProcess) closeOutput() {
	process.outputOnce.Do(func() {
		close(process.outputCh)
	})
}

type exitBeforeOutputProcessFactory struct {
	process *exitBeforeOutputProcess
}

func (factory *exitBeforeOutputProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := &exitBeforeOutputProcess{
		id:       spec.TerminalID,
		outputCh: make(chan []byte, 1),
		waitCh:   make(chan ProcessExit, 1),
	}
	factory.process = process
	return process, nil
}

type exitBeforeOutputProcess struct {
	id       string
	outputCh chan []byte
	waitCh   chan ProcessExit
}

func (process *exitBeforeOutputProcess) Input([]byte) error {
	return nil
}

func (process *exitBeforeOutputProcess) Resize(Size) error {
	return nil
}

func (process *exitBeforeOutputProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *exitBeforeOutputProcess) Kill() error {
	process.exitThenOutput(-1, "")
	return nil
}

func (process *exitBeforeOutputProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *exitBeforeOutputProcess) Close() error {
	process.exitThenOutput(-1, "")
	return nil
}

func (process *exitBeforeOutputProcess) exitThenOutput(code int, output string) {
	process.waitCh <- ProcessExit{Code: code}
	close(process.waitCh)
	if output != "" {
		process.outputCh <- []byte(output)
	}
	close(process.outputCh)
}
