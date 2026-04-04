package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestResolvePaneTitlePrefersTerminalTitle(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		Title:      "Pane Title",
		TerminalID: "term-1",
	}

	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{
			{
				TerminalID: "term-1",
				Title:      "Terminal OSC Title",
			},
		},
	}

	title := resolvePaneTitle(pane, runtimeState)
	if title != "Terminal OSC Title" {
		t.Errorf("Expected 'Terminal OSC Title', got '%s'", title)
	}
}

func TestResolvePaneTitleFallsBackToRuntimeName(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		Title:      "Pane Title",
		TerminalID: "term-1",
	}

	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{
			{
				TerminalID: "term-1",
				Name:       "Runtime Name",
			},
		},
	}

	title := resolvePaneTitle(pane, runtimeState)
	if title != "Runtime Name" {
		t.Errorf("Expected 'Runtime Name', got '%s'", title)
	}
}

func TestResolvePaneTitleFallsBackToPaneTitle(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		Title:      "Pane Title",
		TerminalID: "term-1",
	}

	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{
			{
				TerminalID: "term-1",
			},
		},
	}

	title := resolvePaneTitle(pane, runtimeState)
	if title != "Pane Title" {
		t.Errorf("Expected 'Pane Title', got '%s'", title)
	}
}

func TestResolvePaneTitleWithNoTerminal(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		Title:      "Pane Title",
		TerminalID: "",
	}

	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{},
	}

	title := resolvePaneTitle(pane, runtimeState)
	if title != "unconnected" {
		t.Errorf("Expected 'unconnected', got '%s'", title)
	}
}

func TestResolvePaneTitleWithNilRuntime(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		Title:      "Pane Title",
		TerminalID: "term-1",
	}

	title := resolvePaneTitle(pane, nil)
	if title != "Pane Title" {
		t.Errorf("Expected 'Pane Title', got '%s'", title)
	}
}

func TestPaneMetaUsesPaneBindingRole(t *testing.T) {
	runtimeState := &runtime.VisibleRuntime{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID:   "term-1",
			State:        "running",
			OwnerPaneID:  "pane-1",
			BoundPaneIDs: []string{"pane-1", "pane-2"},
		}},
		Bindings: []runtime.VisiblePaneBinding{
			{PaneID: "pane-1", Role: "owner", Connected: true},
			{PaneID: "pane-2", Role: "follower", Connected: true},
		},
	}

	ownerMeta := paneMeta(workbench.VisiblePane{ID: "pane-1", TerminalID: "term-1"}, runtimeState)
	followerMeta := paneMeta(workbench.VisiblePane{ID: "pane-2", TerminalID: "term-1"}, runtimeState)

	if ownerMeta != "● owner ⧉ 2" {
		t.Fatalf("unexpected owner meta: %q", ownerMeta)
	}
	if followerMeta != "● follow:pane-1 ⧉ 2" {
		t.Fatalf("unexpected follower meta: %q", followerMeta)
	}
}
