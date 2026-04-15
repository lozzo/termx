package app

import (
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
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

func TestPaneResizeSynchronizesLayoutAndPTYBeforeFirstRedraw(t *testing.T) {
	model := setupTwoPaneModel(t)
	client, ok := model.runtime.Client().(*recordingBridgeClient)
	if !ok || client == nil {
		t.Fatal("expected recording bridge client")
	}
	before := xansi.Strip(model.View())

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionResizePaneRight, PaneID: "pane-1"})
	after := xansi.Strip(model.View())
	if after == before {
		t.Fatalf("expected pane resize to redraw updated layout immediately, still got:\n%s", after)
	}
	if len(client.resizes) == 0 {
		t.Fatalf("expected pane resize to synchronize PTY size during update, got %#v", client.resizes)
	}
	if cmd != nil {
		drainCmd(t, model, cmd, 20)
	}
}
