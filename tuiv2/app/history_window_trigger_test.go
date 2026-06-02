package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestWheelEnteringCopyModeRequestsLatestAuthoritativeWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"legacy"}, []string{"screen"})
	source := &appHistoryFakeSource{
		latest: appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 101, []string{"latest"}, 1, true),
	}
	model.historySource = source

	cmd := model.localScrollbackWheelCmd("pane-1", localMouseWheelScrollLines)
	if cmd == nil {
		t.Fatal("expected wheel entry command")
	}
	msgs := collectBatchMessages(cmd)
	if !containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("expected latest history window load message, got %#v", msgs)
	}
	if source.latestRequests != 1 {
		t.Fatalf("expected one latest request, got %d", source.latestRequests)
	}
	if source.latestRequest.TerminalID != "term-1" || source.latestRequest.Limit < defaultAuthoritativeHistoryWindowRows || source.latestRequest.Cols <= 0 {
		t.Fatalf("unexpected latest request: %#v", source.latestRequest)
	}
	if model.effectiveInputMode() != input.ModeDisplay {
		t.Fatalf("expected wheel to enter display/copy mode, got %q", model.effectiveInputMode())
	}
}

func TestCopyModePageUpAtTopRequestsOlderAuthoritativeWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 101, []string{"new-a", "new-b"}, 2, true)
	if !model.HistoryStore().ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window seed to be accepted")
	}
	source := &appHistoryFakeSource{
		older: appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old-a", "old-b"}, 4, false),
	}
	model.historySource = source
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    "term-1",
		WindowToken:   "token-1",
		Cursor:        copyModePoint{Row: 0, Col: 0},
		CursorLogical: copyModeLogicalPos{Line: 0, Offset: 0},
		ViewTopRow:    0,
	}
	model.saveCurrentCopyModeState()

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModePageUp})
	if !handled || cmd == nil {
		t.Fatalf("expected page-up handled with older request, handled=%v cmd=%#v", handled, cmd)
	}
	msgs := collectBatchMessages(cmd)
	if !containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("expected older history window load message, got %#v", msgs)
	}
	if source.olderRequests != 1 {
		t.Fatalf("expected one older request, got %d", source.olderRequests)
	}
	if source.olderRequest.Token != "token-1" || source.olderRequest.BeforeCursor != latest.BeforeCursor {
		t.Fatalf("unexpected older request: %#v", source.olderRequest)
	}
}

func TestCopyModePageUpAwayFromTopDoesNotRequestOlderWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 105, []string{"r0", "r1", "r2", "r3", "r4", "r5"}, 6, true)
	if !model.HistoryStore().ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window seed to be accepted")
	}
	source := &appHistoryFakeSource{
		older: appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old"}, 7, false),
	}
	model.historySource = source
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    "term-1",
		WindowToken:   "token-1",
		Cursor:        copyModePoint{Row: 5, Col: 0},
		CursorLogical: copyModeLogicalPos{Line: 5, Offset: 0},
		ViewTopRow:    2,
	}
	model.saveCurrentCopyModeState()

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModePageUp})
	if !handled {
		t.Fatal("expected page-up handled")
	}
	msgs := collectBatchMessages(cmd)
	if containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("did not expect older history request away from top, got %#v", msgs)
	}
	if source.olderRequests != 0 {
		t.Fatalf("expected no older requests, got %d", source.olderRequests)
	}
}

func TestCopyModeWheelUpAtTopRequestsOlderAuthoritativeWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 101, []string{"new-a", "new-b"}, 2, true)
	seedAuthoritativeCopyModeWindow(t, model, latest, copyModeLogicalPos{Line: 0, Offset: 0}, 0)
	source := &appHistoryFakeSource{
		older: appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old-a", "old-b"}, 4, false),
	}
	model.historySource = source
	model.setMode(input.ModeState{Kind: input.ModeDisplay})

	cmd := model.handleMouseWheelRepeated(tea.MouseMsg{Button: tea.MouseButtonWheelUp}, 1)
	if cmd == nil {
		t.Fatal("expected wheel-up command")
	}
	msgs := collectBatchMessages(cmd)
	if !containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("expected older history window load message, got %#v", msgs)
	}
	if source.olderRequests != 1 {
		t.Fatalf("expected one older request, got %d", source.olderRequests)
	}
	if source.olderRequest.Token != "token-1" || source.olderRequest.BeforeCursor != latest.BeforeCursor {
		t.Fatalf("unexpected older request: %#v", source.olderRequest)
	}
}

func TestCopyModeWheelUpAwayFromTopDoesNotRequestOlderWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 105, []string{"r0", "r1", "r2", "r3", "r4", "r5"}, 6, true)
	seedAuthoritativeCopyModeWindow(t, model, latest, copyModeLogicalPos{Line: 5, Offset: 0}, 2)
	source := &appHistoryFakeSource{
		older: appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old"}, 7, false),
	}
	model.historySource = source
	model.setMode(input.ModeState{Kind: input.ModeDisplay})

	cmd := model.handleMouseWheelRepeated(tea.MouseMsg{Button: tea.MouseButtonWheelUp}, 1)
	msgs := collectBatchMessages(cmd)
	if containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("did not expect older history request away from top, got %#v", msgs)
	}
	if source.olderRequests != 0 {
		t.Fatalf("expected no older requests, got %d", source.olderRequests)
	}
}

func TestCopyModePageDownStaysWithinAuthoritativeWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 6})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 107, []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7"}, 8, true)
	seedAuthoritativeCopyModeWindow(t, model, latest, copyModeLogicalPos{Line: 0, Offset: 0}, 0)
	source := &appHistoryFakeSource{
		latest: appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-2", 200, 201, []string{"new-a", "new-b"}, 2, true),
		older:  appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old-a", "old-b"}, 4, false),
	}
	model.historySource = source

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModePageDown})
	if !handled {
		t.Fatal("expected page-down handled")
	}
	msgs := collectBatchMessages(cmd)
	if containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("did not expect page-down to request history window, got %#v", msgs)
	}
	if source.latestRequests != 0 || source.olderRequests != 0 {
		t.Fatalf("expected no history requests, got latest=%d older=%d", source.latestRequests, source.olderRequests)
	}
	if model.copyMode.CursorLogical.Line <= 0 || model.copyMode.CursorLogical.Line >= len(latest.Lines) {
		t.Fatalf("expected page-down to move within authoritative window, got cursor %#v", model.copyMode.CursorLogical)
	}
}

func TestCopyModeBottomStaysWithinAuthoritativeWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 6})
	latest := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 100, 107, []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7"}, 8, true)
	seedAuthoritativeCopyModeWindow(t, model, latest, copyModeLogicalPos{Line: 0, Offset: 0}, 0)
	source := &appHistoryFakeSource{
		latest: appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-2", 200, 201, []string{"new-a", "new-b"}, 2, true),
		older:  appHistoryFakeWindow("term-1", historyview.WindowOpPrepend, "token-1", 98, 99, []string{"old-a", "old-b"}, 4, false),
	}
	model.historySource = source

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModeBottom})
	if !handled {
		t.Fatal("expected bottom handled")
	}
	msgs := collectBatchMessages(cmd)
	if containsHistoryWindowLoadedMsg(msgs, "term-1") {
		t.Fatalf("did not expect bottom to request history window, got %#v", msgs)
	}
	if source.latestRequests != 0 || source.olderRequests != 0 {
		t.Fatalf("expected no history requests, got latest=%d older=%d", source.latestRequests, source.olderRequests)
	}
	if got, want := model.copyMode.CursorLogical.Line, len(latest.Lines)-1; got != want {
		t.Fatalf("expected bottom to jump to authoritative window bottom line %d, got %d", want, got)
	}
}

func seedAuthoritativeCopyModeWindow(t *testing.T, model *Model, window historyview.HistoryWindow, cursor copyModeLogicalPos, viewTopRow int) {
	t.Helper()
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative history window seed to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    window.TerminalID,
		WindowToken:   string(window.Token),
		Cursor:        copyModePoint{Row: cursor.Line, Col: 0},
		CursorLogical: cursor,
		ViewTopRow:    viewTopRow,
	}
	model.saveCurrentCopyModeState()
}

func collectBatchMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		out := make([]tea.Msg, 0, len(batch))
		for _, nested := range batch {
			out = append(out, collectBatchMessages(nested)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func containsHistoryWindowLoadedMsg(msgs []tea.Msg, terminalID string) bool {
	for _, msg := range msgs {
		loaded, ok := msg.(historyWindowLoadedMsg)
		if ok && loaded.TerminalID == terminalID {
			return true
		}
	}
	return false
}
