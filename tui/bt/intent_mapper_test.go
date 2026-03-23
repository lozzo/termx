package bt

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	"github.com/lozzow/termx/tui/domain/types"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestIntentMapperRootCtrlWOpensWorkspacePicker(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlW})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenWorkspacePickerIntent); !ok {
		t.Fatalf("expected open workspace picker intent, got %T", intents[0])
	}
}

func TestIntentMapperRootCtrlFOpensTerminalPicker(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlF})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenTerminalPickerIntent); !ok {
		t.Fatalf("expected open terminal picker intent, got %T", intents[0])
	}
}

func TestIntentMapperRootCtrlGArmsGlobalModeAndTOpensTerminalManager(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: now},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlG})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	activate, ok := intents[0].(intent.ActivateModeIntent)
	if !ok {
		t.Fatalf("expected activate mode intent, got %T", intents[0])
	}
	if activate.Mode != types.ModeGlobal || activate.DeadlineAt == nil || !activate.DeadlineAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("unexpected activate mode payload: %+v", activate)
	}

	state := newAppStateWithSinglePane()
	state.UI.Mode = types.ModeState{
		Active:     types.ModeGlobal,
		DeadlineAt: activate.DeadlineAt,
	}
	intents = mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenTerminalManagerIntent); !ok {
		t.Fatalf("expected open terminal manager intent, got %T", intents[0])
	}
}

func TestIntentMapperWorkspacePickerMapsNavigationAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayWorkspacePicker}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "down",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			want: intent.WorkspacePickerMoveIntent{Delta: 1},
		},
		{
			name: "left",
			key:  tea.KeyMsg{Type: tea.KeyLeft},
			want: intent.WorkspacePickerCollapseIntent{},
		},
		{
			name: "enter",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.WorkspacePickerSubmitIntent{},
		},
		{
			name: "backspace",
			key:  tea.KeyMsg{Type: tea.KeyBackspace},
			want: intent.WorkspacePickerBackspaceIntent{},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")},
			want: intent.WorkspacePickerAppendQueryIntent{Text: "ops"},
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

func TestIntentMapperTerminalPickerMapsNavigationAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalPicker}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "down",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			want: intent.TerminalPickerMoveIntent{Delta: 1},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.TerminalPickerSubmitIntent{},
		},
		{
			name: "backspace",
			key:  tea.KeyMsg{Type: tea.KeyBackspace},
			want: intent.TerminalPickerBackspaceIntent{},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")},
			want: intent.TerminalPickerAppendQueryIntent{Text: "ops"},
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

func TestIntentMapperPromptMapsStructuredFieldKeys(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind: promptdomain.KindEditTerminalMetadata,
			Fields: []promptdomain.Field{
				{Key: "name", Value: "build-log"},
				{Key: "tags", Value: "group=build"},
			},
		},
	}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "tab",
			key:  tea.KeyMsg{Type: tea.KeyTab},
			want: intent.PromptNextFieldIntent{},
		},
		{
			name: "shift tab",
			key:  tea.KeyMsg{Type: tea.KeyShiftTab},
			want: intent.PromptPreviousFieldIntent{},
		},
		{
			name: "input",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-v2")},
			want: intent.PromptAppendInputIntent{Text: "-v2"},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.SubmitPromptIntent{},
		},
		{
			name: "cancel",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
			want: intent.CancelPromptIntent{},
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

func TestIntentMapperTerminalManagerMapsSelectionAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalManager}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "up",
			key:  tea.KeyMsg{Type: tea.KeyUp},
			want: intent.TerminalManagerMoveIntent{Delta: -1},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("build")},
			want: intent.TerminalManagerAppendQueryIntent{Text: "build"},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.TerminalManagerConnectHereIntent{},
		},
		{
			name: "new tab",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")},
			want: intent.TerminalManagerConnectInNewTabIntent{},
		},
		{
			name: "floating",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")},
			want: intent.TerminalManagerConnectInFloatingPaneIntent{},
		},
		{
			name: "edit",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")},
			want: intent.TerminalManagerEditMetadataIntent{},
		},
		{
			name: "acquire owner",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")},
			want: intent.TerminalManagerAcquireOwnerIntent{},
		},
		{
			name: "stop",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			want: intent.TerminalManagerStopIntent{},
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

func TestE2EIntentMapperScenarioWorkspacePickerSearchesAndJumpsToPane(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})
	state := newAppStateWithTwoWorkspaces()
	rd := reducer.New()

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlW},
		{Type: tea.KeyRunes, Runes: []rune("float-dev")},
		{Type: tea.KeyEnter},
	}
	for _, key := range sequence {
		for _, in := range mapper.MapKey(state, key) {
			state = rd.Reduce(state, in).State
		}
	}

	if state.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected active workspace to switch to ws-2, got %q", state.Domain.ActiveWorkspaceID)
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close, got %q", state.UI.Overlay.Kind)
	}
	if state.UI.Focus.PaneID != types.PaneID("pane-float") || state.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected focus to land on floating pane, got %+v", state.UI.Focus)
	}
}

func newAppStateWithSinglePane() types.AppState {
	return types.AppState{
		Domain: types.DomainState{
			ActiveWorkspaceID: types.WorkspaceID("ws-1"),
			WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
			Workspaces: map[types.WorkspaceID]types.WorkspaceState{
				types.WorkspaceID("ws-1"): {
					ID:          types.WorkspaceID("ws-1"),
					Name:        "ws-1",
					ActiveTabID: types.TabID("tab-1"),
					TabOrder:    []types.TabID{types.TabID("tab-1")},
					Tabs: map[types.TabID]types.TabState{
						types.TabID("tab-1"): {
							ID:           types.TabID("tab-1"),
							Name:         "tab-1",
							ActivePaneID: types.PaneID("pane-1"),
							ActiveLayer:  types.FocusLayerTiled,
							Panes: map[types.PaneID]types.PaneState{
								types.PaneID("pane-1"): {
									ID:        types.PaneID("pane-1"),
									Kind:      types.PaneKindTiled,
									SlotState: types.PaneSlotEmpty,
								},
							},
						},
					},
				},
			},
			Terminals:   map[types.TerminalID]types.TerminalRef{},
			Connections: map[types.TerminalID]types.ConnectionState{},
		},
		UI: types.UIState{
			Focus: types.FocusState{
				Layer:       types.FocusLayerTiled,
				WorkspaceID: types.WorkspaceID("ws-1"),
				TabID:       types.TabID("tab-1"),
				PaneID:      types.PaneID("pane-1"),
			},
		},
	}
}

func newAppStateWithTwoWorkspaces() types.AppState {
	state := newAppStateWithSinglePane()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ws-2",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "tab-2",
				ActivePaneID: types.PaneID("pane-float"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("pane-float"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-float"): {
						ID:         types.PaneID("pane-float"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-float"),
					},
				},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-float")] = types.TerminalRef{
		ID:    types.TerminalID("term-float"),
		Name:  "float-dev",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-float")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-float"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-float")},
		OwnerPaneID:      types.PaneID("pane-float"),
	}
	return state
}
