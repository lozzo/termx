package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestCopyModeBufferPrefersAuthoritativeHistoryWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"legacy"}, []string{"screen"})
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 12, []string{"auth-a", "auth-b", "auth-c"}, 3, false)
	window.Lines = []historyview.LineSpan{
		{StartRow: 0, EndRow: 1, Kind: historyview.RowKindPersisted, LogicalLineID: 10},
		{StartRow: 2, EndRow: 2, Kind: historyview.RowKindPersisted, LogicalLineID: 12},
	}
	window.Rows[0].Wrapped = true
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative window to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:      "pane-1",
		TerminalID:  "term-1",
		WindowToken: "token-1",
		Cursor:      copyModePoint{Row: 2, Col: 0},
	}
	model.saveCurrentCopyModeState()

	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	if buffer.window == nil {
		t.Fatal("expected buffer backed by authoritative history window")
	}
	if got := buffer.logicalLineCount(); got != 2 {
		t.Fatalf("expected logical line count from line spans, got %d", got)
	}
	line, ok := buffer.logicalLineByIndex(0)
	if !ok {
		t.Fatal("expected first logical line")
	}
	if line.StartRow != 0 || line.EndRow != 1 || line.Text != "auth-aauth-b" {
		t.Fatalf("unexpected first authoritative line: %#v", line)
	}
}

func TestCopyModeSelectionUsesAuthoritativeHistoryWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"legacy"}, []string{"screen"})
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 20, 21, []string{"alpha", "bravo"}, 2, false)
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative window to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    "term-1",
		WindowToken:   "token-1",
		Cursor:        copyModePoint{Row: 1, Col: 1},
		CursorLogical: copyModeLogicalPos{Line: 1, Offset: 1},
		Mark:          &copyModePoint{Row: 0, Col: 1},
		MarkLogical:   &copyModeLogicalPos{Line: 0, Offset: 1},
	}
	model.saveCurrentCopyModeState()

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected copy-mode selection")
	}
	if text != "alpha\nb" {
		t.Fatalf("expected selection from authoritative window, got %q", text)
	}
}

func TestRenderCopyModeVMUsesAuthoritativeWindowProjection(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 30, 30, []string{"auth-only"}, 1, false)
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative window to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:      "pane-1",
		TerminalID:  "term-1",
		WindowToken: "token-1",
		Cursor:      copyModePoint{Row: 0, Col: 0},
	}
	model.setMode(input.ModeState{Kind: input.ModeDisplay})
	model.saveCurrentCopyModeState()

	vms := model.renderCopyModeVMs()
	if len(vms) != 1 {
		t.Fatalf("expected one render copy-mode VM, got %#v", vms)
	}
	if vms[0].Snapshot == nil || len(vms[0].Snapshot.Screen.Cells) != 1 {
		t.Fatalf("expected render snapshot projection, got %#v", vms[0].Snapshot)
	}
	if got := rowTextFromCells(vms[0].Snapshot.Screen.Cells[0]); got != "auth-only" {
		t.Fatalf("expected authoritative render row, got %q", got)
	}
	if vms[0].Snapshot.Scrollback != nil {
		t.Fatalf("expected no scrollback truth in authoritative render projection, got %#v", vms[0].Snapshot.Scrollback)
	}
}

func TestSnapshotFromHistoryWindowPreservesMetadata(t *testing.T) {
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 40, 41, []string{"one", "two"}, 2, true)
	window.Rows[1].Kind = historyview.RowKindLiveTailReclaimed
	window.Rows[1].Wrapped = true
	snapshot := snapshotFromHistoryWindow(window)

	if snapshot == nil {
		t.Fatal("expected snapshot projection")
	}
	if snapshot.TerminalID != "term-1" || snapshot.ScrollbackTotal != window.TotalRows || !snapshot.ScrollbackHasMore {
		t.Fatalf("unexpected snapshot header: %#v", snapshot)
	}
	if snapshot.HistoryGeneration != window.Generation || snapshot.ScrollbackFirstRowID != window.FirstBoundaryID || snapshot.ScrollbackLastRowID != window.LastBoundaryID {
		t.Fatalf("unexpected snapshot boundary metadata: %#v", snapshot)
	}
	if snapshot.ScreenOwnership[1] != string(historyview.RowKindLiveTailReclaimed) || !snapshot.ScreenWrapped[1] {
		t.Fatalf("unexpected row metadata ownership=%#v wrapped=%#v", snapshot.ScreenOwnership, snapshot.ScreenWrapped)
	}
	if rowTextFromCells(snapshot.Screen.Cells[1]) != "two" {
		t.Fatalf("unexpected row projection: %#v", snapshot.Screen.Cells[1])
	}
}

func rowTextFromCells(cells []protocol.Cell) string {
	return rowTextFromCompactRow(protocol.CompactRowFromCells(cells))
}
