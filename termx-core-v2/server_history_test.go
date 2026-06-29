package termxcorev2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestServerHistoryWindowReadsAuthoritativeStore(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history",
		Command: []string{"sh"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	if err := server.IngestOutput(context.Background(), info.ID, "hello history\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), info.ID, history.HistoryWindowRequest{
		Cols:  80,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) == 0 {
		t.Fatalf("history window should expose rows")
	}
	if got := historyRowsText(window.Rows); !strings.Contains(got, "hello history") {
		t.Fatalf("history window should read authoritative payload, got %q rows=%#v", got, window.Rows)
	}
}

func TestServerHistoryDisabledReturnsExplicitError(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryDisabled())
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-no-history",
		Command: []string{"sh"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	_, err = server.TerminalHistoryWindow(context.Background(), info.ID, history.HistoryWindowRequest{Cols: 80, Limit: 10})
	if !errors.Is(err, ErrHistoryDisabled) {
		t.Fatalf("disabled history should return ErrHistoryDisabled, got %v", err)
	}
}

func TestServerHistoryOldestReturnsFrozenHeadWindow(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-oldest",
		Command: []string{"sh"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-oldest", "line-1\r\nline-2\r\nline-3\r\nline-4\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	snapshot, err := server.TerminalHistoryFreeze(context.Background(), "term-history-oldest", history.FreezeHistoryRequest{
		TerminalID: "term-history-oldest",
		Cols:       30,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("history.freeze: %v", err)
	}
	oldest, err := server.TerminalHistoryWindow(context.Background(), "term-history-oldest", history.HistoryWindowRequest{
		TerminalID: "term-history-oldest",
		Mode:       history.HistoryWindowModeOldest,
		Token:      snapshot.Token,
		Cols:       30,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("oldest history window: %v", err)
	}
	if oldest.Op != history.HistoryWindowReplace {
		t.Fatalf("oldest window must replace local visible window, got %s", oldest.Op)
	}
	if got := historyRowTextList(oldest.Rows); strings.Join(got, "|") != "line-1|line-2" {
		t.Fatalf("oldest should return frozen head rows, got %q window=%#v", strings.Join(got, "|"), oldest)
	}
	if oldest.HasMore {
		t.Fatalf("oldest head window must not advertise older pages, cursor=%#v", oldest.Boundary.Cursor)
	}
	if err := server.TerminalHistoryRelease(context.Background(), "term-history-oldest", snapshot.Token); err != nil {
		t.Fatalf("history.release: %v", err)
	}
}

func TestServerHistoryUsesFileBackedStoreWhenConfigured(t *testing.T) {
	historyDir := t.TempDir()
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(historyDir))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-file-backed",
		Command: []string{"sh"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-file-backed", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	payloadPath := filepath.Join(historyDir, "term-history-file-backed.history-lines.bin")
	if info, err := os.Stat(payloadPath); err != nil || info.Size() == 0 {
		t.Fatalf("expected file-backed history payload at %s, info=%#v err=%v", payloadPath, info, err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-history-file-backed", history.HistoryWindowRequest{
		TerminalID: "term-history-file-backed",
		Mode:       history.HistoryWindowModeLatest,
		Limit:      2,
		Cols:       30,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := historyRowTextList(window.Rows); strings.Join(got, "|") != "alpha|beta" {
		t.Fatalf("file-backed terminal history window mismatch: %q", strings.Join(got, "|"))
	}
}

func TestServerHistoryDoesNotFallbackWhenBackendCannotOpen(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "blocking-file")
	badDir := filepath.Join(blockingFile, "child")
	if err := os.WriteFile(blockingFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(badDir))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-history-bad-backend", Command: []string{"sh"}}); err == nil {
		t.Fatal("register terminal should fail when configured file-backed history cannot be created")
	}
}

func TestServerHistoryRemoveSealsOpenLineBeforeClosingTerminal(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-remove",
		Command: []string{"sh"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-remove", "partial"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	terminal, err := server.Terminal("term-history-remove")
	if err != nil {
		t.Fatalf("terminal before remove: %v", err)
	}
	if err := server.RemoveTerminal("term-history-remove"); err != nil {
		t.Fatalf("remove terminal: %v", err)
	}
	window, err := terminal.HistoryWindow(history.HistoryWindowRequest{
		TerminalID: "term-history-remove",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after remove should read closed store: %v", err)
	}
	if got := historyRowTextList(window.Rows); strings.Join(got, "|") != "partial" {
		t.Fatalf("remove should seal open line before process close, got %q", strings.Join(got, "|"))
	}
}

func TestServerHistoryPrimaryFrameAltResizeAndExitStayAuthoritative(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-screen",
		Command: []string{"sh"},
		Size:    Size{Cols: 8, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-screen", "\x1b[?2026hline1\r\nline2\r\nline3\r\nline4\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{
		TerminalID: "term-history-screen",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       8,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after sync output: %v", err)
	}
	if !historyRowsContainSegment(window.Rows, history.HistorySegmentCommitted) ||
		!historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) ||
		!historyRowsContainText(window.Rows, "line4") {
		t.Fatalf("history should include sealed scroll-out proof and current frame, rows=%v", historyRowTextList(window.Rows))
	}

	if err := server.IngestOutput(context.Background(), "term-history-screen", "\x1b[?1049hALT\x1b[?1049l"); err != nil {
		t.Fatalf("ingest alt transient: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{
		TerminalID: "term-history-screen",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       8,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after alt: %v", err)
	}
	for _, row := range window.Rows {
		if strings.Contains(historyCellsText(row.Cells), "ALT") && row.Segment != history.HistorySegmentCurrentAltFrame {
			t.Fatalf("alt content must not enter primary timeline, row=%#v rows=%v", row, historyRowTextList(window.Rows))
		}
	}
	if err := server.ResizeTerminal(context.Background(), "term-history-screen", 12, 4); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
	windowAfterResize, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{
		TerminalID: "term-history-screen",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       12,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after resize: %v", err)
	}
	if len(windowAfterResize.Rows) < len(window.Rows) {
		t.Fatalf("resize boundary must not delete history rows: before=%v after=%v", historyRowTextList(window.Rows), historyRowTextList(windowAfterResize.Rows))
	}

	if len(factory.processes) != 1 {
		t.Fatal("expected recording process")
	}
	if err := factory.processes[0].Close(); err != nil {
		t.Fatalf("close recording process output: %v", err)
	}
	factory.processes[0].wait <- ProcessExit{Code: 0}
	waitForTerminalState(t, server, "term-history-screen", TerminalStateExited)
	windowAfterExit, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{
		TerminalID: "term-history-screen",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       12,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("history.window after exit: %v", err)
	}
	if len(windowAfterExit.Rows) == 0 {
		t.Fatal("process exit should leave authoritative history rows")
	}
}

func historyRowsText(rows []history.HistoryRow) string {
	var builder strings.Builder
	for _, row := range rows {
		for _, cell := range row.Cells {
			builder.WriteString(cell.Text)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func historyRowTextList(rows []history.HistoryRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		var builder strings.Builder
		for _, cell := range row.Cells {
			builder.WriteString(cell.Text)
		}
		texts = append(texts, builder.String())
	}
	return texts
}

func historyRowsContainText(rows []history.HistoryRow, needle string) bool {
	for _, row := range rows {
		if strings.Contains(historyCellsText(row.Cells), needle) {
			return true
		}
	}
	return false
}
