package bt

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestIntentMapperLayoutResolveMapsSelectionAndSubmit(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayLayoutResolve,
		Data: layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api"),
	}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "down",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			want: intent.LayoutResolveMoveIntent{Delta: 1},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.LayoutResolveSubmitIntent{},
		},
		{
			name: "cancel",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
			want: intent.CloseOverlayIntent{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapKey(state, tc.key)
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestE2EModelScenarioLayoutResolveConnectExistingFlow(t *testing.T) {
	initial := newManagerAppState()
	ws := initial.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotWaiting
	pane.TerminalID = ""
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	initial.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	delete(initial.Domain.Connections, types.TerminalID("term-1"))
	initial.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayLayoutResolve,
		Data:        layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api"),
		ReturnFocus: initial.UI.Focus,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayLayoutResolve
	initial.UI.Mode = types.ModeState{Active: types.ModePicker}

	model := NewModel(ModelConfig{
		InitialState:  initial,
		Mapper:        NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:       reducer.New(),
		EffectHandler: NoopEffectHandler{},
		Renderer:      StaticRenderer{},
	})

	current := model
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune("ops")},
		{Type: tea.KeyEnter},
	} {
		next, _ := current.Update(key)
		current = next.(*Model)
	}

	state := current.State()
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after resolve flow, got %q", state.UI.Overlay.Kind)
	}
	pane = state.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-3") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected layout resolve flow to connect selected terminal, got %+v", pane)
	}
}
