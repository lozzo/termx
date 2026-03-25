package layoutresolve

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestStateMoveSelectionAndSelectedAction(t *testing.T) {
	state := NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api")

	row, ok := state.SelectedRow()
	if !ok || row.Action != ActionConnectExisting {
		t.Fatalf("expected default selection to be connect existing, got %+v ok=%v", row, ok)
	}

	state.MoveSelection(1)
	row, ok = state.SelectedRow()
	if !ok || row.Action != ActionCreateNew {
		t.Fatalf("expected selection to move to create new, got %+v ok=%v", row, ok)
	}

	state.MoveSelection(10)
	row, ok = state.SelectedRow()
	if !ok || row.Action != ActionSkip {
		t.Fatalf("expected selection to clamp to skip, got %+v ok=%v", row, ok)
	}
}

func TestStateClonePreservesResolveMetadata(t *testing.T) {
	state := NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api")
	state.MoveSelection(1)

	clone, ok := state.CloneOverlayData().(*State)
	if !ok {
		t.Fatalf("expected clone type *State, got %T", state.CloneOverlayData())
	}
	if clone.PaneID != types.PaneID("pane-1") || clone.Role != "backend-dev" || clone.Hint != "env=dev service=api" {
		t.Fatalf("expected resolve metadata to survive clone, got %+v", clone)
	}
	row, ok := clone.SelectedRow()
	if !ok || row.Action != ActionCreateNew {
		t.Fatalf("expected clone selection to stay on create new, got %+v ok=%v", row, ok)
	}
}
