package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/historyview"
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
	model.historySource = &appHistoryFakeSource{
		latest: appHistoryFakeWindow("term-1", historyview.WindowOpReplace, "token-1", 10, 10, []string{"x"}, 1, false),
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
