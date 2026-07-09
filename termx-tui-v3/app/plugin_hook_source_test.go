package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestClientHookSourceAfterEventPublishesPanelBoundWithTerminalRef(t *testing.T) {
	now := time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC)
	source := NewClientHookSource(ClientHookSourceConfig{
		SourceSession: "tui-1",
		ClientKind:    plugin.ClientKindTUI,
		WorkspaceID:   "workspace-a",
		Now:           func() time.Time { return now },
	})
	ref := state.NewTerminalRef("remote-a", "term-1")
	cause := plugin.MessageTrace{TraceID: "trace-plugin", ActorPath: []plugin.PluginID{"acme.deploy"}}
	event, ok := source.AfterEvent(ClientHookAfterEvent{
		ObjectKind:  ClientHookObjectPanel,
		Verb:        ClientHookVerbBound,
		ObjectID:    "pane-1",
		TabID:       "tab-1",
		TerminalRef: &ref,
		Action:      "terminal.attach",
		Source:      string(state.PaneCommandSourceKeyboard),
		Trace:       cause,
	})
	if !ok {
		t.Fatal("expected panel bound hook")
	}
	if event.Type != plugin.SystemEventClientPanelBound || event.SourceHost != plugin.HostClient || event.SourceSession != "tui-1" || event.ClientKind != plugin.ClientKindTUI {
		t.Fatalf("unexpected client hook envelope %#v", event)
	}
	if event.WorkspaceID != "workspace-a" || event.ObjectKind != plugin.ObjectKindPanel || event.ObjectID != "pane-1" {
		t.Fatalf("unexpected object/workspace fields %#v", event)
	}
	if event.DaemonID != "" || event.DaemonTerminalID != "" {
		t.Fatalf("client hook must not claim daemon-local lifecycle identity, got %#v", event)
	}
	if event.EndpointID != "remote-a" || event.TerminalRef == nil || event.TerminalRef.EndpointID != "remote-a" || event.TerminalRef.TerminalID != "term-1" {
		t.Fatalf("expected client TerminalRef in hook, got %#v", event)
	}
	if event.Lossy || event.Sequence != 1 {
		t.Fatalf("unexpected lossy/sequence %#v", event)
	}
	if event.Trace.TraceID != "trace-plugin" || !event.Trace.ContainsActor("acme.deploy") {
		t.Fatalf("hook should inherit cause trace for loop protection, got %#v", event.Trace)
	}
	var payload ClientHookPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.WorkspaceID != "workspace-a" || payload.TabID != "tab-1" || payload.EndpointID != "remote-a" || payload.TerminalID != "term-1" || payload.Action != "terminal.attach" {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestClientHookSourceCommandAdaptersEmitSuccessfulPanelFloatAndTabEvents(t *testing.T) {
	now := time.Date(2026, 7, 9, 11, 1, 0, 0, time.UTC)
	source := NewClientHookSource(ClientHookSourceConfig{SourceSession: "tui-1", WorkspaceID: "workspace-a", Now: func() time.Time { return now }})
	ref := state.NewTerminalRef("local", "term-1")

	panel, ok := source.AfterPaneCommand(state.PaneCommand{
		Action: state.PaneCommandSplit,
		Target: state.PaneCommandTarget{
			WorkspaceID: "workspace-a",
			TabID:       "tab-1",
			PaneID:      "pane-1",
		},
		NewPane: state.PaneState{ID: "pane-2"},
		Source:  state.PaneCommandSourceKeyboard,
	}, state.PaneCommandResult{Status: state.PaneCommandOK, Action: state.PaneCommandSplit}, nil)
	if !ok || panel.Type != plugin.SystemEventClientPanelCreated || panel.ObjectID != "pane-2" || panel.Lossy {
		t.Fatalf("unexpected panel created hook %#v ok=%v", panel, ok)
	}

	closed, ok := source.AfterPaneCommand(state.PaneCommand{
		Action: state.PaneCommandCloseAndKill,
		Target: state.PaneCommandTarget{WorkspaceID: "workspace-a", TabID: "tab-1", PaneID: "pane-2"},
		Source: state.PaneCommandSourceKeyboard,
	}, state.PaneCommandResult{Status: state.PaneCommandOK, Action: state.PaneCommandCloseAndKill}, &ref)
	if !ok || closed.Type != plugin.SystemEventClientPanelClosed || closed.DaemonTerminalID != "" {
		t.Fatalf("close-and-kill should only emit client panel.closed, got %#v ok=%v", closed, ok)
	}

	floating, ok := source.AfterFloatingCommand(state.FloatingCommand{
		Action: state.FloatingCommandCreate,
		Rect:   state.FloatingRect{X: 2, Y: 3, W: 80, H: 20},
		Source: state.PaneCommandSourceMouse,
	}, state.FloatingCommandResult{Status: state.FloatingCommandOK, Action: state.FloatingCommandCreate, ID: "float-1"}, &ref)
	if !ok || floating.Type != plugin.SystemEventClientFloatCreated || floating.ObjectID != "float-1" || floating.EndpointID != "local" {
		t.Fatalf("unexpected floating created hook %#v ok=%v", floating, ok)
	}
	var floatingPayload ClientHookPayload
	if err := json.Unmarshal(floating.Payload, &floatingPayload); err != nil {
		t.Fatalf("decode floating payload: %v", err)
	}
	if floatingPayload.Rect == nil || floatingPayload.Rect.W != 80 || floatingPayload.Source != string(state.PaneCommandSourceMouse) {
		t.Fatalf("unexpected floating payload %#v", floatingPayload)
	}

	resized, ok := source.AfterFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandResize,
		TargetID: "float-1",
		Source:   state.PaneCommandSourceKeyboard,
	}, state.FloatingCommandResult{Status: state.FloatingCommandOK, Action: state.FloatingCommandResize, ID: "float-1"}, nil)
	if !ok || resized.Type != plugin.SystemEventClientFloatResized || !resized.Lossy {
		t.Fatalf("floating resize should be lossy, got %#v ok=%v", resized, ok)
	}

	tab, ok := source.AfterWorkbenchCommand(state.WorkbenchCommand{
		Action: state.WorkbenchCommandTabCreate,
		Target: state.PaneCommandTarget{
			WorkspaceID: "workspace-a",
		},
		Source: state.PaneCommandSourcePalette,
	}, state.WorkbenchCommandResult{Status: state.WorkbenchCommandOK, Action: state.WorkbenchCommandTabCreate, ID: "tab-2"})
	if !ok || tab.Type != plugin.SystemEventClientTabCreated || tab.ObjectID != "tab-2" {
		t.Fatalf("unexpected tab created hook %#v ok=%v", tab, ok)
	}

	activated, ok := source.AfterWorkbenchCommand(state.WorkbenchCommand{
		Action: state.WorkbenchCommandTabSwitch,
		Target: state.PaneCommandTarget{WorkspaceID: "workspace-a"},
		Source: state.PaneCommandSourcePalette,
	}, state.WorkbenchCommandResult{Status: state.WorkbenchCommandOK, Action: state.WorkbenchCommandTabSwitch, ID: "tab-main"})
	if !ok || activated.Type != plugin.SystemEventClientTabActivated || activated.Lossy {
		t.Fatalf("unexpected tab activated hook %#v ok=%v", activated, ok)
	}

	focused, ok := source.AfterPaneCommand(state.PaneCommand{
		Action: state.PaneCommandFocus,
		Target: state.PaneCommandTarget{WorkspaceID: "workspace-a", TabID: "tab-1", PaneID: "pane-1"},
	}, state.PaneCommandResult{Status: state.PaneCommandOK, Action: state.PaneCommandFocus}, nil)
	if !ok || focused.Type != plugin.SystemEventClientPanelFocused || !focused.Lossy {
		t.Fatalf("panel focus should be lossy focus hook, got %#v ok=%v", focused, ok)
	}

	panelResized, ok := source.AfterPaneCommand(state.PaneCommand{
		Action: state.PaneCommandResize,
		Target: state.PaneCommandTarget{WorkspaceID: "workspace-a", TabID: "tab-1", PaneID: "pane-1"},
	}, state.PaneCommandResult{Status: state.PaneCommandOK, Action: state.PaneCommandResize}, nil)
	if !ok || panelResized.Type != plugin.SystemEventClientPanelResized || !panelResized.Lossy {
		t.Fatalf("panel resize should be lossy resize hook, got %#v ok=%v", panelResized, ok)
	}

	floatFocused, ok := source.AfterFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandSummon,
		TargetID: "float-1",
	}, state.FloatingCommandResult{Status: state.FloatingCommandOK, Action: state.FloatingCommandSummon, ID: "float-1"}, nil)
	if !ok || floatFocused.Type != plugin.SystemEventClientFloatFocused || !floatFocused.Lossy {
		t.Fatalf("floating summon should be lossy focus hook, got %#v ok=%v", floatFocused, ok)
	}

	floatClosed, ok := source.AfterFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandClose,
		TargetID: "float-1",
	}, state.FloatingCommandResult{Status: state.FloatingCommandOK, Action: state.FloatingCommandClose, ID: "float-1"}, nil)
	if !ok || floatClosed.Type != plugin.SystemEventClientFloatClosed || floatClosed.Lossy {
		t.Fatalf("unexpected floating closed hook %#v ok=%v", floatClosed, ok)
	}

	tabNext, ok := source.AfterWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabNext}, state.WorkbenchCommandResult{Status: state.WorkbenchCommandOK, Action: state.WorkbenchCommandTabNext, ID: "tab-2"})
	if !ok || tabNext.Type != plugin.SystemEventClientTabActivated {
		t.Fatalf("tab next should become tab activated hook, got %#v ok=%v", tabNext, ok)
	}
}

func TestClientHookSourceRejectsFailedAndUnsupportedAfterEvents(t *testing.T) {
	source := NewClientHookSource(ClientHookSourceConfig{SourceSession: "tui-1"})
	if event, ok := source.AfterPaneCommand(state.PaneCommand{
		Action:  state.PaneCommandSplit,
		Target:  state.PaneCommandTarget{WorkspaceID: "workspace-a", TabID: "tab-1", PaneID: "pane-1"},
		NewPane: state.PaneState{ID: "pane-2"},
	}, state.PaneCommandResult{Status: state.PaneCommandInvalid, Action: state.PaneCommandSplit}, nil); ok {
		t.Fatalf("failed reducer result must not emit hook: %#v", event)
	}
	if event, ok := source.AfterWorkbenchCommand(state.WorkbenchCommand{
		Action: state.WorkbenchCommandTabClose,
	}, state.WorkbenchCommandResult{Status: state.WorkbenchCommandOK, Action: state.WorkbenchCommandTabClose, ID: "tab-1"}); ok {
		t.Fatalf("unsupported tab close hook should not emit in PL006: %#v", event)
	}
	if event, ok := source.AfterEvent(ClientHookAfterEvent{
		ObjectKind: ClientHookObjectTab,
		Verb:       ClientHookVerbFocused,
		ObjectID:   "tab-1",
	}); ok {
		t.Fatalf("unknown event type combination should not emit: %#v", event)
	}
	if event, ok := source.AfterFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: "float-1"}, state.FloatingCommandResult{Status: state.FloatingCommandInvalid, Action: state.FloatingCommandClose}, nil); ok {
		t.Fatalf("failed floating result must not emit hook: %#v", event)
	}
	if event, ok := source.AfterEvent(ClientHookAfterEvent{
		ObjectKind: ClientHookObjectPanel,
		Verb:       ClientHookVerbFocused,
	}); ok {
		t.Fatalf("empty object id must not emit hook: %#v", event)
	}
}

func TestClientHookEffectsAreEmittedFromReducerAfterSuccessfulMutations(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell(), ClientHooks: state.ClientHookStore{Enabled: true}}
	next, effects := reducePaneCommand(root, state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "pane", Kind: state.PaneEmpty},
		Source:         state.PaneCommandSourceKeyboard,
	})
	if next.Shell.ActivePaneID != "pane-2" {
		t.Fatalf("split should create and focus pane-2, got %q", next.Shell.ActivePaneID)
	}
	hook := mustFindClientHookEffect(t, effects, ClientHookObjectPanel, ClientHookVerbCreated)
	if hook.ObjectID != "pane-2" || hook.WorkspaceID != state.DefaultWorkspaceID || hook.TabID != state.DefaultTabID {
		t.Fatalf("unexpected pane created after-event %#v", hook)
	}

	next, effects = reduceWorkbenchCommand(next, state.WorkbenchCommand{Action: state.WorkbenchCommandTabNext, Source: state.PaneCommandSourceKeyboard})
	hook = mustFindClientHookEffect(t, effects, ClientHookObjectTab, ClientHookVerbActivated)
	if hook.ObjectID != state.DefaultTabID || hook.WorkspaceID != state.DefaultWorkspaceID {
		t.Fatalf("unexpected tab activated after-event %#v", hook)
	}

	var result state.FloatingCommandResult
	next.Shell, result = next.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane", Title: "float", Kind: state.PaneEmpty},
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("setup floating: %#v", result)
	}
	next, effects = reduceFloatingCommand(next, state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: "float-1", Source: state.PaneCommandSourceKeyboard})
	hook = mustFindClientHookEffect(t, effects, ClientHookObjectFloat, ClientHookVerbFocused)
	if hook.ObjectID != "float-1" || !hook.Lossy {
		t.Fatalf("unexpected floating focused after-event %#v", hook)
	}
}

func TestClientHookTerminalRefEffectsPreserveEndpointScope(t *testing.T) {
	source := NewClientHookSource(ClientHookSourceConfig{SourceSession: "tui-1"})
	for _, endpointID := range []state.EndpointID{"west", "east", ""} {
		ref := state.NewTerminalRef(endpointID, "term-1")
		event, ok := source.AfterEvent(ClientHookAfterEvent{
			ObjectKind:  ClientHookObjectPanel,
			Verb:        ClientHookVerbBound,
			ObjectID:    "pane-" + string(state.NormalizeEndpointID(endpointID)),
			TerminalRef: &ref,
		})
		if !ok {
			t.Fatalf("expected hook for endpoint %q", endpointID)
		}
		if event.TerminalRef == nil || event.TerminalRef.EndpointID != plugin.EndpointID(state.NormalizeEndpointID(endpointID)) || event.TerminalRef.TerminalID != "term-1" {
			t.Fatalf("unexpected terminal ref for endpoint %q: %#v", endpointID, event.TerminalRef)
		}
	}
}

func mustFindClientHookEffect(t *testing.T, effects []Effect, kind ClientHookObjectKind, verb ClientHookVerb) ClientHookAfterEvent {
	t.Helper()
	for _, effect := range effects {
		hook, ok := effect.(ClientHookAfterEventEffect)
		if !ok {
			continue
		}
		if hook.After.ObjectKind == kind && hook.After.Verb == verb {
			return hook.After
		}
	}
	t.Fatalf("missing client hook after-event kind=%s verb=%s in %#v", kind, verb, effects)
	return ClientHookAfterEvent{}
}
