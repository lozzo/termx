package reducer

import (
	"testing"

	"github.com/lozzow/termx/tui/app/intent"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestReducerOpenLayoutResolveMovesFocusToOverlay(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	result := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})

	if result.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected layout resolve overlay, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay {
		t.Fatalf("expected overlay focus layer, got %+v", result.State.UI.Focus)
	}
	resolve, ok := result.State.UI.Overlay.Data.(*layoutresolvedomain.State)
	if !ok {
		t.Fatalf("expected layout resolve data, got %T", result.State.UI.Overlay.Data)
	}
	if resolve.PaneID != types.PaneID("pane-1") || resolve.Role != "backend-dev" {
		t.Fatalf("unexpected resolve overlay payload: %+v", resolve)
	}
}

func TestReducerLayoutResolveConnectExistingOpensTerminalPicker(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	opened := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})
	result := reducer.Reduce(opened.State, intent.LayoutResolveSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalPicker {
		t.Fatalf("expected connect existing to hand off to terminal picker, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("expected connect existing to stay pure UI handoff, got %d effects", len(result.Effects))
	}
}

func TestReducerLayoutResolveCreateNewKeepsOverlayOpenUntilSuccessAndEmitsCreateEffect(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	opened := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})
	moved := reducer.Reduce(opened.State, intent.LayoutResolveMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.LayoutResolveSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected create new to keep overlay open until success, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one create effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create terminal effect, got %T", result.Effects[0])
	}
	if effect.PaneID != types.PaneID("pane-1") || effect.Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("unexpected create effect payload: %+v", effect)
	}
}

func TestReducerLayoutResolveCreateTerminalSucceededClosesOverlayAndRegistersTerminal(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	opened := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})
	moved := reducer.Reduce(opened.State, intent.LayoutResolveMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.CreateTerminalSucceededIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-created"),
		Name:       "ws-1-tab-1-pane-1",
		Command:    []string{"sh", "-l"},
		State:      types.TerminalRunStateRunning,
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected create success to close layout resolve overlay, got %q", result.State.UI.Overlay.Kind)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-created")]
	if terminal.ID != types.TerminalID("term-created") || terminal.Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("unexpected terminal after layout resolve create success: %+v", terminal)
	}
}

func TestReducerLayoutResolveSkipKeepsWaitingPaneAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	opened := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})
	moved := reducer.Reduce(opened.State, intent.LayoutResolveMoveIntent{Delta: 2})
	result := reducer.Reduce(moved.State, intent.LayoutResolveSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected skip to close overlay, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotWaiting || pane.TerminalID != "" {
		t.Fatalf("expected waiting pane to remain unresolved after skip, got %+v", pane)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("expected skip to emit no effects, got %d", len(result.Effects))
	}
}

func TestE2EReducerScenarioLayoutResolveConnectExistingFlow(t *testing.T) {
	reducer := New()
	state := newWaitingPaneAppState()

	opened := reducer.Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   "backend-dev",
		Hint:   "env=dev service=api",
	})
	resolveSubmitted := reducer.Reduce(opened.State, intent.LayoutResolveSubmitIntent{})
	typed := reducer.Reduce(resolveSubmitted.State, intent.TerminalPickerAppendQueryIntent{Text: "ops"})
	submitted := reducer.Reduce(typed.State, intent.TerminalPickerSubmitIntent{})

	if submitted.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after resolve flow, got %q", submitted.State.UI.Overlay.Kind)
	}
	pane := submitted.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-3") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected waiting pane to connect selected terminal, got %+v", pane)
	}
}

func newWaitingPaneAppState() types.AppState {
	state := newManagerAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotWaiting
	pane.TerminalID = ""
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	delete(state.Domain.Connections, types.TerminalID("term-1"))
	return state
}
