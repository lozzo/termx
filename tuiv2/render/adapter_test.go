package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestAdaptVisibleStateProjectsWorkbenchAndRuntime(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"

	state := AdaptVisibleState(wb, rt)
	if state.Workbench == nil || state.Workbench.WorkspaceName != "main" {
		t.Fatalf("unexpected workbench projection: %#v", state.Workbench)
	}
	if len(state.Workbench.Tabs) != 1 || len(state.Workbench.Tabs[0].Panes) != 1 {
		t.Fatalf("unexpected pane projection: %#v", state.Workbench)
	}
	if state.Runtime == nil || len(state.Runtime.Terminals) != 1 {
		t.Fatalf("unexpected runtime projection: %#v", state.Runtime)
	}
	if state.Runtime.Terminals[0].TerminalID != "term-1" {
		t.Fatalf("unexpected runtime terminal: %#v", state.Runtime.Terminals[0])
	}
}

func TestVisibleStateSurfaceAndOverlayAttachors(t *testing.T) {
	state := AdaptVisibleState(nil, nil)
	if state.Surface.Kind != VisibleSurfaceWorkbench {
		t.Fatalf("expected default workbench surface, got %v", state.Surface.Kind)
	}
	if state.Overlay.Kind != VisibleOverlayNone {
		t.Fatalf("expected default empty overlay, got %v", state.Overlay.Kind)
	}

	pool := &modal.TerminalManagerState{Title: "Terminal Pool"}
	state = AttachTerminalPool(state, pool)
	if state.Surface.Kind != VisibleSurfaceTerminalPool || state.Surface.TerminalPool != pool {
		t.Fatalf("expected terminal-pool surface, got %#v", state.Surface)
	}

	help := modal.DefaultHelp()
	state = AttachHelp(state, help)
	if state.Overlay.Kind != VisibleOverlayHelp || state.Overlay.Help != help {
		t.Fatalf("expected help overlay, got %#v", state.Overlay)
	}

	prompt := &modal.PromptState{Title: "Prompt"}
	state = AttachPrompt(state, prompt)
	if state.Overlay.Kind != VisibleOverlayPrompt || state.Overlay.Prompt != prompt {
		t.Fatalf("expected prompt overlay, got %#v", state.Overlay)
	}
}

func TestAttachModalHostProjectsActiveOverlay(t *testing.T) {
	tests := []struct {
		name string
		host *modal.ModalHost
		kind VisibleOverlayKind
	}{
		{
			name: "picker",
			host: &modal.ModalHost{
				Session: &modal.ModalSession{Kind: input.ModePicker},
				Picker:  &modal.PickerState{Title: "picker"},
			},
			kind: VisibleOverlayPicker,
		},
		{
			name: "workspace picker",
			host: &modal.ModalHost{
				Session:         &modal.ModalSession{Kind: input.ModeWorkspacePicker},
				WorkspacePicker: &modal.WorkspacePickerState{Title: "workspace"},
			},
			kind: VisibleOverlayWorkspacePicker,
		},
		{
			name: "help",
			host: &modal.ModalHost{
				Session: &modal.ModalSession{Kind: input.ModeHelp},
				Help:    modal.DefaultHelp(),
			},
			kind: VisibleOverlayHelp,
		},
		{
			name: "prompt",
			host: &modal.ModalHost{
				Session: &modal.ModalSession{Kind: input.ModePrompt},
				Prompt:  &modal.PromptState{Title: "prompt"},
			},
			kind: VisibleOverlayPrompt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := AttachModalHost(AdaptVisibleState(nil, nil), tt.host)
			if state.Overlay.Kind != tt.kind {
				t.Fatalf("expected overlay kind %v, got %#v", tt.kind, state.Overlay)
			}
		})
	}
}

func TestAttachModalHostWithoutSessionClearsOverlay(t *testing.T) {
	state := AttachPrompt(AdaptVisibleState(nil, nil), &modal.PromptState{Title: "prompt"})
	state = AttachModalHost(state, &modal.ModalHost{})
	if state.Overlay.Kind != VisibleOverlayNone {
		t.Fatalf("expected empty overlay when modal session is missing, got %#v", state.Overlay)
	}
}
