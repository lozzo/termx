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
		Draft:      "ops-center",
	}

	cloned, ok := state.CloneOverlayData().(*State)
	if !ok {
		t.Fatalf("expected cloned prompt state, got %T", state.CloneOverlayData())
	}
	if cloned.Kind != KindCreateWorkspace || cloned.TerminalID != types.TerminalID("term-1") || cloned.Draft != "ops-center" {
		t.Fatalf("expected prompt state fields to survive clone, got %+v", cloned)
	}
}

func TestPromptStateAppendAndBackspaceDraft(t *testing.T) {
	state := State{Kind: KindCreateWorkspace}

	state.AppendInput("ops")
	state.AppendInput("-center")
	if state.Draft != "ops-center" {
		t.Fatalf("expected draft to append, got %q", state.Draft)
	}

	state.BackspaceInput()
	if state.Draft != "ops-cente" {
		t.Fatalf("expected draft to backspace, got %q", state.Draft)
	}
}
