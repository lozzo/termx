package bt

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type stubIntentMapper struct {
	intents []intent.Intent
	keys    []tea.KeyMsg
}

func (m *stubIntentMapper) MapKey(_ types.AppState, msg tea.KeyMsg) []intent.Intent {
	m.keys = append(m.keys, msg)
	return m.intents
}

type stubReducer struct {
	result  reducer.Result
	intents []intent.Intent
}

func (r *stubReducer) Reduce(state types.AppState, in intent.Intent) reducer.Result {
	r.intents = append(r.intents, in)
	if reflect.DeepEqual(r.result.State, types.AppState{}) {
		return reducer.Result{State: state}
	}
	return r.result
}

type stubEffectHandler struct {
	effects []reducer.Effect
}

func (h *stubEffectHandler) Handle(effects []reducer.Effect) tea.Cmd {
	h.effects = append([]reducer.Effect(nil), effects...)
	if len(effects) == 0 {
		return nil
	}
	return func() tea.Msg {
		return effectsHandledMsg{Count: len(effects)}
	}
}

type stubRenderer struct {
	seen []types.AppState
	view string
}

func (r *stubRenderer) Render(state types.AppState) string {
	r.seen = append(r.seen, state)
	return r.view
}

type effectsHandledMsg struct {
	Count int
}

func TestModelInitReturnsNilCommand(t *testing.T) {
	model := NewModel(ModelConfig{
		InitialState: newAppStateWithSinglePane(),
		Mapper:       NewIntentMapper(Config{}),
		Reducer:      reducer.New(),
	})

	if cmd := model.Init(); cmd != nil {
		t.Fatalf("expected nil init command, got %v", cmd)
	}
}

func TestModelUpdateRunsMapperReducerAndEffectHandler(t *testing.T) {
	initial := newAppStateWithSinglePane()
	next := newAppStateWithSinglePane()
	next.UI.Overlay = types.OverlayState{Kind: types.OverlayWorkspacePicker}

	mapper := &stubIntentMapper{
		intents: []intent.Intent{intent.OpenWorkspacePickerIntent{}},
	}
	rd := &stubReducer{
		result: reducer.Result{
			State:   next,
			Effects: []reducer.Effect{reducer.OpenPromptEffect{PromptKind: reducer.PromptKindCreateWorkspace}},
		},
	}
	effects := &stubEffectHandler{}

	model := NewModel(ModelConfig{
		InitialState:   initial,
		Mapper:         mapper,
		Reducer:        rd,
		EffectHandler:  effects,
		Renderer:       &stubRenderer{view: "state"},
	})

	updatedModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	updated, ok := updatedModel.(*Model)
	if !ok {
		t.Fatalf("expected updated model type, got %T", updatedModel)
	}
	if len(mapper.keys) != 1 || mapper.keys[0].Type != tea.KeyCtrlW {
		t.Fatalf("expected mapper to receive ctrl+w, got %+v", mapper.keys)
	}
	if len(rd.intents) != 1 {
		t.Fatalf("expected reducer to receive one intent, got %d", len(rd.intents))
	}
	if _, ok := rd.intents[0].(intent.OpenWorkspacePickerIntent); !ok {
		t.Fatalf("expected open workspace picker intent, got %T", rd.intents[0])
	}
	if updated.State().UI.Overlay.Kind != types.OverlayWorkspacePicker {
		t.Fatalf("expected model state to update, got %+v", updated.State().UI.Overlay)
	}
	if len(effects.effects) != 1 {
		t.Fatalf("expected effect handler to receive one effect, got %d", len(effects.effects))
	}
	msg := cmd()
	handled, ok := msg.(effectsHandledMsg)
	if !ok || handled.Count != 1 {
		t.Fatalf("expected effect cmd to yield handled msg, got %#v", msg)
	}
}

func TestModelUpdateIgnoresNonKeyMessages(t *testing.T) {
	initial := newAppStateWithSinglePane()
	model := NewModel(ModelConfig{
		InitialState: initial,
		Mapper:       NewIntentMapper(Config{}),
		Reducer:      reducer.New(),
	})

	updatedModel, cmd := model.Update(effectsHandledMsg{Count: 1})
	updated := updatedModel.(*Model)
	if cmd != nil {
		t.Fatalf("expected nil command for non-key msg, got %v", cmd)
	}
	if !reflect.DeepEqual(updated.State(), initial) {
		t.Fatalf("expected state to remain unchanged, got %+v", updated.State())
	}
}

func TestModelViewDelegatesToRenderer(t *testing.T) {
	renderer := &stubRenderer{view: "workspace-picker"}
	model := NewModel(ModelConfig{
		InitialState: newAppStateWithSinglePane(),
		Mapper:       NewIntentMapper(Config{}),
		Reducer:      reducer.New(),
		Renderer:     renderer,
	})

	if got := model.View(); got != "workspace-picker" {
		t.Fatalf("expected renderer output, got %q", got)
	}
	if len(renderer.seen) != 1 {
		t.Fatalf("expected renderer to see one state, got %d", len(renderer.seen))
	}
}

func TestE2EModelScenarioCtrlWQueryAndEnterJumpsToPane(t *testing.T) {
	model := NewModel(ModelConfig{
		InitialState:  newAppStateWithTwoWorkspaces(),
		Mapper:        NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:       reducer.New(),
		EffectHandler: NoopEffectHandler{},
		Renderer:      StaticRenderer{},
	})

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlW},
		{Type: tea.KeyRunes, Runes: []rune("float-dev")},
		{Type: tea.KeyEnter},
	}

	current := model
	for _, key := range sequence {
		next, _ := current.Update(key)
		current = next.(*Model)
	}

	state := current.State()
	if state.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected active workspace to switch to ws-2, got %q", state.Domain.ActiveWorkspaceID)
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close, got %q", state.UI.Overlay.Kind)
	}
	if state.UI.Focus.PaneID != types.PaneID("pane-float") || state.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected focus to land on target pane, got %+v", state.UI.Focus)
	}
}
