package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
)

func TestSemanticActionPolicyMapsResizeAndSaveFollowUps(t *testing.T) {
	model := setupModel(t, modelOpts{statePath: t.TempDir() + "/workspace-state.json"})
	policy := model.semanticActionPolicy()
	if policy == nil {
		t.Fatal("expected semantic action policy")
	}

	if cmd := policy.resizeCmd(input.SemanticAction{Kind: input.ActionSplitPane}); cmd == nil {
		t.Fatal("expected split pane to request resize follow-up")
	}
	if cmd := policy.saveCmd(input.SemanticAction{Kind: input.ActionSplitPane}); cmd == nil {
		t.Fatal("expected split pane to request save follow-up")
	}
	if cmd := policy.resizeCmd(input.SemanticAction{Kind: input.ActionResizeFloatingLeft, PaneID: "pane-1"}); cmd == nil {
		t.Fatal("expected floating resize to request pane resize follow-up")
	}
	if cmd := policy.saveCmd(input.SemanticAction{Kind: input.ActionResizeFloatingLeft, PaneID: "pane-1"}); cmd != nil {
		t.Fatal("expected floating resize not to request save follow-up")
	}
	if cmd := policy.resizeCmd(input.SemanticAction{Kind: input.ActionOpenHelp}); cmd != nil {
		t.Fatal("expected unrelated action not to request resize follow-up")
	}
	if cmd := policy.saveCmd(input.SemanticAction{Kind: input.ActionOpenHelp}); cmd != nil {
		t.Fatal("expected unrelated action not to request save follow-up")
	}
}
