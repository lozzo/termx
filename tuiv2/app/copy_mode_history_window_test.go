package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestCopyModeBufferPrefersAuthoritativeHistoryWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedTerminalSnapshotFixture(t, model, []string{"legacy"}, []string{"screen"})
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
	seedTerminalSnapshotFixture(t, model, []string{"legacy"}, []string{"screen"})
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

func TestCopyModeSelectionJoinsClippedAuthoritativeLineSegments(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 50, 50, []string{"left-", "right"}, 2, false)
	window.Rows[0].Cells = protocol.CompactRowFromCells(protocolRowFromText("left-", 5))
	window.Rows[1].Cells = protocol.CompactRowFromCells(protocolRowFromText("right", 5))
	window.Lines = []historyview.LineSpan{
		{StartRow: 0, EndRow: 0, Kind: historyview.RowKindPersisted, LogicalLineID: 50, ClippedAfter: true},
		{StartRow: 1, EndRow: 1, Kind: historyview.RowKindPersisted, LogicalLineID: 50, ClippedBefore: true},
	}
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative window to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    "term-1",
		WindowToken:   "token-1",
		Cursor:        copyModePoint{Row: 1, Col: 5},
		CursorLogical: copyModeLogicalPos{Line: 1, Offset: 5},
		Mark:          &copyModePoint{Row: 0, Col: 0},
		MarkLogical:   &copyModeLogicalPos{Line: 0, Offset: 0},
	}
	model.saveCurrentCopyModeState()

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected copy-mode selection")
	}
	if text != "left-right" {
		t.Fatalf("expected clipped segments to copy as one logical line, got %q", text)
	}
}

func TestCopyModeSelectionSeparatesDistinctAuthoritativeLines(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 60, 61, []string{"alpha", "bravo"}, 2, false)
	window.Rows[0].Cells = protocol.CompactRowFromCells(protocolRowFromText("alpha", 5))
	window.Rows[1].Cells = protocol.CompactRowFromCells(protocolRowFromText("bravo", 5))
	if !model.HistoryStore().ApplyHistoryWindow(window) {
		t.Fatal("expected authoritative window to be accepted")
	}
	model.copyMode = copyModeState{
		PaneID:        "pane-1",
		TerminalID:    "term-1",
		WindowToken:   "token-1",
		Cursor:        copyModePoint{Row: 1, Col: 5},
		CursorLogical: copyModeLogicalPos{Line: 1, Offset: 5},
		Mark:          &copyModePoint{Row: 0, Col: 0},
		MarkLogical:   &copyModeLogicalPos{Line: 0, Offset: 0},
	}
	model.saveCurrentCopyModeState()

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected copy-mode selection")
	}
	if text != "alpha\nbravo" {
		t.Fatalf("expected distinct authoritative lines to keep newline, got %q", text)
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
	if vms[0].Projection == nil || len(vms[0].Projection.Rows) != 1 {
		t.Fatalf("expected render-native authoritative projection, got %#v", vms[0].Projection)
	}
	if got := rowTextFromCells(vms[0].Projection.Rows[0].Cells); got != "auth-only" {
		t.Fatalf("expected authoritative render row, got %q", got)
	}
	if vms[0].Projection.Token != "token-1" || vms[0].Projection.FirstBoundaryID != window.FirstBoundaryID || vms[0].Projection.LastBoundaryID != window.LastBoundaryID {
		t.Fatalf("expected authoritative metadata in projection, got %#v", vms[0].Projection)
	}
}

func TestRenderCopyModeProjectionPreservesMetadata(t *testing.T) {
	window := appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 40, 41, []string{"one", "two"}, 2, true)
	window.Rows[1].Kind = historyview.RowKindLiveTailReclaimed
	window.Rows[1].Wrapped = true
	projection := renderCopyModeProjectionFromHistoryWindow(window)

	if projection == nil {
		t.Fatal("expected render projection")
	}
	if projection.TerminalID != "term-1" || projection.TotalRows != window.TotalRows || !projection.HasMore {
		t.Fatalf("unexpected projection header: %#v", projection)
	}
	if projection.Generation != window.Generation || projection.FirstBoundaryID != window.FirstBoundaryID || projection.LastBoundaryID != window.LastBoundaryID {
		t.Fatalf("unexpected projection boundary metadata: %#v", projection)
	}
	if projection.Rows[1].Kind != string(historyview.RowKindLiveTailReclaimed) || !projection.Rows[1].Wrapped {
		t.Fatalf("unexpected row metadata kind=%#v wrapped=%#v", projection.Rows[1].Kind, projection.Rows[1].Wrapped)
	}
	if rowTextFromCells(projection.Rows[1].Cells) != "two" {
		t.Fatalf("unexpected row projection: %#v", projection.Rows[1].Cells)
	}
}

func rowTextFromCells(cells []protocol.Cell) string {
	return rowTextFromCompactRow(protocol.CompactRowFromCells(cells))
}
