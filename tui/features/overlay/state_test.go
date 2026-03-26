package overlay

import (
	"testing"

	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
	"github.com/lozzow/termx/tui/core/types"
)

func TestOverlayOpenAndClear(t *testing.T) {
	state := State{}
	state = state.OpenConnectPicker([]featureterminalpool.Item{{ID: types.TerminalID("term-1"), Name: "shell-1"}, {ID: types.TerminalID("term-2"), Name: "shell-2"}}, types.TerminalID("term-1"))
	if state.Active.Kind != KindConnectPicker || state.Active.Title != "connect" || state.Active.Selected != types.TerminalID("term-1") {
		t.Fatalf("expected connect picker overlay, got %#v", state.Active)
	}

	state = state.SelectNext()
	if state.Active.Selected != types.TerminalID("term-2") {
		t.Fatalf("expected next connect selection, got %q", state.Active.Selected)
	}
	state = state.SelectPrev()
	if state.Active.Selected != types.TerminalID("term-1") {
		t.Fatalf("expected previous connect selection, got %q", state.Active.Selected)
	}

	state = state.OpenHelp()
	if state.Active.Kind != KindHelp || state.Active.Title != "help" {
		t.Fatalf("expected help overlay, got %#v", state.Active)
	}

	state = state.OpenPrompt("remove terminal")
	if state.Active.Kind != KindPrompt || state.Active.Title != "remove terminal" {
		t.Fatalf("expected prompt overlay, got %#v", state.Active)
	}

	state = state.Clear()
	if state.Active.Kind != "" {
		t.Fatalf("expected cleared overlay, got %q", state.Active.Kind)
	}
}
