package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestHandleCopyModeLocalActionModeGuards(t *testing.T) {
	model := setupModel(t, modelOpts{})

	if handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionPasteBuffer}); handled || cmd != nil {
		t.Fatalf("expected paste-buffer guard to reject outside display mode, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleCopyModeLocalActionHandlesNavigationActions(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeDisplay})

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModeTop})
	if !handled {
		t.Fatalf("expected copy-mode navigation action handled, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleCopyModeLocalActionCopySelectionExitResetsModeWithoutSelection(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeDisplay})

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModeCopySelectionExit})
	if !handled || cmd != nil {
		t.Fatalf("expected copy-selection-exit handled synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected mode reset to normal, got %q", got)
	}
}

func TestHandleCopyModeLocalActionBeginSelectionMarksCurrentPoint(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		Cursor:     protocol.CursorState{Row: 0, Col: 0},
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	handled, cmd := model.handleCopyModeLocalAction(input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})
	if !handled || cmd != nil {
		t.Fatalf("expected begin-selection handled synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected copy mode mark to be set")
	}
	if *model.copyMode.Mark != model.copyMode.Cursor {
		t.Fatalf("expected mark to equal current cursor, mark=%#v cursor=%#v", *model.copyMode.Mark, model.copyMode.Cursor)
	}
}
