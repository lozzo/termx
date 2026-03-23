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

func TestPromptStateStructuredFieldsAppendSwitchAndBackspace(t *testing.T) {
	state := State{
		Kind: KindEditTerminalMetadata,
		Fields: []Field{
			{Key: "name", Value: "build-log"},
			{Key: "tags", Value: "group=build"},
		},
	}

	state.AppendInput("-v2")
	if state.Fields[0].Value != "build-log-v2" {
		t.Fatalf("expected append on active field, got %+v", state.Fields)
	}

	if !state.NextField() {
		t.Fatalf("expected next field to switch focus")
	}
	state.AppendInput(",env=prod")
	if state.Fields[1].Value != "group=build,env=prod" {
		t.Fatalf("expected append on second field, got %+v", state.Fields)
	}

	state.BackspaceInput()
	if state.Fields[1].Value != "group=build,env=pro" {
		t.Fatalf("expected backspace on second field, got %+v", state.Fields)
	}
}

func TestPromptStateActiveValueFallsBackToDraftWhenNoFields(t *testing.T) {
	state := State{Draft: "ops-center"}

	if state.ActiveValue() != "ops-center" {
		t.Fatalf("expected active value to use draft, got %q", state.ActiveValue())
	}
}
