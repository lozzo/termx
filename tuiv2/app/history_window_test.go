package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

func TestModelLoadLatestAuthoritativeHistoryWindowAppliesStore(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	source := &appHistoryFakeSource{
		latest: appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 11, []string{"latest-a", "latest-b"}, 10, true),
	}
	model.historySource = source

	cmd := model.loadLatestHistoryWindowCmd("term-1", 50, 120)
	if cmd == nil {
		t.Fatal("expected latest history window command")
	}
	msg, ok := cmd().(historyWindowLoadedMsg)
	if !ok {
		t.Fatalf("expected historyWindowLoadedMsg, got %T", msg)
	}
	if source.latestRequest != (historyview.WindowRequest{TerminalID: "term-1", Limit: 50, Cols: 120}) {
		t.Fatalf("unexpected latest request: %#v", source.latestRequest)
	}

	_, next := model.Update(msg)
	if next != nil {
		t.Fatalf("expected no follow-up command, got %#v", next)
	}
	window, ok := model.HistoryStore().HistoryWindow("term-1")
	if !ok {
		t.Fatal("expected stored authoritative history window")
	}
	if window.Token != "token-1" || window.Op != historyview.WindowOpReplace || len(window.Rows) != 2 {
		t.Fatalf("unexpected stored window: %#v", window)
	}
	if got := appHistoryRowText(window.Rows[0]); got != "latest-a" {
		t.Fatalf("unexpected first row text %q", got)
	}
}

func TestModelLoadOlderAuthoritativeHistoryWindowPrependsStore(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 11, []string{"latest-a", "latest-b"}, 10, true)
	if !model.HistoryStore().ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window seed to be accepted")
	}
	source := &appHistoryFakeSource{
		older: appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 8, 9, []string{"older-a", "older-b"}, 8, false),
	}
	model.historySource = source

	cmd := model.loadOlderHistoryWindowCmd("term-1", 25, 80)
	if cmd == nil {
		t.Fatal("expected older history window command")
	}
	if pending := model.HistoryStore().PendingRequest("term-1"); pending != "token-1" {
		t.Fatalf("expected pending request token, got %q", pending)
	}
	msg, ok := cmd().(historyWindowLoadedMsg)
	if !ok {
		t.Fatalf("expected historyWindowLoadedMsg, got %T", msg)
	}
	if source.olderRequest != (historyview.WindowRequest{TerminalID: "term-1", Token: "token-1", BeforeCursor: 10, Limit: 25, Cols: 80}) {
		t.Fatalf("unexpected older request: %#v", source.olderRequest)
	}

	_, next := model.Update(msg)
	if next != nil {
		t.Fatalf("expected no follow-up command, got %#v", next)
	}
	if pending := model.HistoryStore().PendingRequest("term-1"); pending != "" {
		t.Fatalf("expected pending request cleared, got %q", pending)
	}
	window, ok := model.HistoryStore().HistoryWindow("term-1")
	if !ok {
		t.Fatal("expected merged authoritative history window")
	}
	if len(window.Rows) != 4 {
		t.Fatalf("expected prepended rows, got %#v", window.Rows)
	}
	if got := appHistoryRowText(window.Rows[0]); got != "older-a" {
		t.Fatalf("unexpected first row text %q", got)
	}
	if got := appHistoryRowText(window.Rows[2]); got != "latest-a" {
		t.Fatalf("unexpected merged latest row text %q", got)
	}
}

func TestModelLoadOlderEmptyAuthoritativeWindowMarksExhausted(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 11, []string{"latest-a", "latest-b"}, 10, true)
	if !model.HistoryStore().ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window seed to be accepted")
	}
	source := &appHistoryFakeSource{
		older: historyview.HistoryWindow{
			TerminalID: "term-1",
			Op:         historyview.WindowOpPrepend,
			HasMore:    false,
		},
	}
	model.historySource = source

	cmd := model.loadOlderHistoryWindowCmd("term-1", 25, 80)
	if cmd == nil {
		t.Fatal("expected older history window command")
	}
	if pending := model.HistoryStore().PendingRequest("term-1"); pending != "token-1" {
		t.Fatalf("expected pending request token, got %q", pending)
	}
	msg, ok := cmd().(historyWindowLoadedMsg)
	if !ok {
		t.Fatalf("expected historyWindowLoadedMsg, got %T", msg)
	}
	if msg.RequestToken != "token-1" || msg.RequestBeforeCursor != latest.BeforeCursor {
		t.Fatalf("expected request boundary metadata on loaded message, got %#v", msg)
	}

	_, next := model.Update(msg)
	if next != nil {
		t.Fatalf("expected no follow-up command, got %#v", next)
	}
	if pending := model.HistoryStore().PendingRequest("term-1"); pending != "" {
		t.Fatalf("expected pending request cleared, got %q", pending)
	}
	window, ok := model.HistoryStore().HistoryWindow("term-1")
	if !ok {
		t.Fatal("expected existing authoritative history window")
	}
	if window.HasMore {
		t.Fatalf("expected empty older response to mark window exhausted, got %#v", window)
	}
	if window.Token != latest.Token || window.Generation != latest.Generation {
		t.Fatalf("expected token/generation preserved, got %#v", window)
	}
	if len(window.Rows) != len(latest.Rows) || appHistoryRowText(window.Rows[0]) != "latest-a" {
		t.Fatalf("expected current rows preserved, got %#v", window.Rows)
	}
}

func TestModelHistoryWindowLoadErrorDoesNotApplyAndClearsPending(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 11, []string{"latest"}, 10, true)
	if !model.HistoryStore().ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window seed to be accepted")
	}
	model.HistoryStore().SetPendingRequest("term-1", "token-1")

	_, cmd := model.Update(historyWindowLoadedMsg{
		TerminalID:   "term-1",
		RequestToken: "token-1",
		Err:          errors.New("history failed"),
	})
	if cmd == nil {
		t.Fatal("expected error clear command")
	}
	if model.err == nil || model.err.Error() != "history failed" {
		t.Fatalf("expected model error to be set, got %#v", model.err)
	}
	if pending := model.HistoryStore().PendingRequest("term-1"); pending != "" {
		t.Fatalf("expected pending request cleared, got %q", pending)
	}
	window, ok := model.HistoryStore().HistoryWindow("term-1")
	if !ok || len(window.Rows) != 1 || appHistoryRowText(window.Rows[0]) != "latest" {
		t.Fatalf("expected existing window preserved, ok=%v window=%#v", ok, window)
	}
}

type appHistoryFakeSource struct {
	latest         historyview.HistoryWindow
	older          historyview.HistoryWindow
	err            error
	latestRequest  historyview.WindowRequest
	olderRequest   historyview.WindowRequest
	latestRequests int
	olderRequests  int
}

func (s *appHistoryFakeSource) LiveSurface(context.Context, string) (historyview.LiveSurface, error) {
	return historyview.LiveSurface{}, nil
}

func (s *appHistoryFakeSource) LatestHistoryWindow(_ context.Context, request historyview.WindowRequest) (historyview.HistoryWindow, error) {
	s.latestRequests++
	s.latestRequest = request
	if s.err != nil {
		return historyview.HistoryWindow{}, s.err
	}
	if s.latest.TerminalID == "" {
		return historyview.HistoryWindow{}, errors.New("missing latest fake window")
	}
	return s.latest, nil
}

func (s *appHistoryFakeSource) OlderHistoryWindow(_ context.Context, request historyview.WindowRequest) (historyview.HistoryWindow, error) {
	s.olderRequests++
	s.olderRequest = request
	if s.err != nil {
		return historyview.HistoryWindow{}, s.err
	}
	if request.BeforeCursor <= 0 {
		return historyview.HistoryWindow{}, errors.New("missing older before cursor")
	}
	if s.older.TerminalID == "" {
		return historyview.HistoryWindow{}, errors.New("missing older fake window")
	}
	return s.older, nil
}

func appHistoryFakeWindow(terminalID string, op historyview.WindowOp, token historyview.WindowToken, firstID, lastID uint64, texts []string, beforeCursor int, hasMore bool) historyview.HistoryWindow {
	rows := make([]historyview.HistoryRow, len(texts))
	lines := make([]historyview.LineSpan, len(texts))
	for i, text := range texts {
		id := firstID + uint64(i)
		rows[i] = historyview.HistoryRow{
			Cells:     protocol.CompactRowFromCells([]protocol.Cell{{Content: text, Width: len(text)}}),
			Kind:      historyview.RowKindPersisted,
			Timestamp: time.Date(2026, 6, 2, 5, i, 0, 0, time.UTC),
		}
		lines[i] = historyview.LineSpan{
			StartRow:      i,
			EndRow:        i,
			Kind:          historyview.RowKindPersisted,
			LogicalLineID: id,
		}
	}
	return historyview.HistoryWindow{
		TerminalID:      terminalID,
		Token:           token,
		Op:              op,
		Size:            protocol.Size{Cols: 80, Rows: 24},
		Rows:            rows,
		Lines:           lines,
		BeforeCursor:    beforeCursor,
		LoadedRows:      len(rows),
		TotalRows:       len(rows) + beforeCursor,
		LoadedLines:     len(lines),
		TotalLines:      len(lines) + beforeCursor,
		HasMore:         hasMore,
		Generation:      1,
		FirstLineID:     firstID,
		LastLineID:      lastID,
		FirstBoundaryID: firstID,
		LastBoundaryID:  lastID,
		Timestamp:       time.Date(2026, 6, 2, 6, 0, 0, 0, time.UTC),
	}
}

func appHistoryRowText(row historyview.HistoryRow) string {
	var out string
	for _, cell := range row.Cells.DecodeCells() {
		out += cell.Content
	}
	return out
}
