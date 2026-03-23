package prompt

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestPromptStateImplementsOverlayClone(t *testing.T) {
	state := State{
		Kind:       KindCreateWorkspace,
		Title:      "create workspace",
		TerminalID: types.TerminalID("term-1"),
	}

	cloned, ok := state.CloneOverlayData().(*State)
	if !ok {
		t.Fatalf("expected cloned prompt state, got %T", state.CloneOverlayData())
	}
	if cloned.Kind != KindCreateWorkspace || cloned.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected prompt state fields to survive clone, got %+v", cloned)
	}
}
