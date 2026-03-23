package bt

import (
	"errors"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	"github.com/lozzow/termx/tui/domain/types"
)

var errBoom = errors.New("boom")

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

type stubNoticeScheduler struct {
	ids       []string
	durations []time.Duration
	msg       tea.Msg
}

func (s *stubNoticeScheduler) ScheduleTimeout(id string, after time.Duration) tea.Cmd {
	s.ids = append(s.ids, id)
	s.durations = append(s.durations, after)
	if s.msg == nil {
		return nil
	}
	return func() tea.Msg {
		return s.msg
	}
}

type failingTerminalService struct {
	stopErr error
}

func (f *failingTerminalService) ConnectTerminal(types.PaneID, types.TerminalID) error {
	return nil
}

func (f *failingTerminalService) CreateTerminal(types.PaneID, []string, string) error {
	return nil
}

func (f *failingTerminalService) StopTerminal(types.TerminalID) error {
	return f.stopErr
}

func (f *failingTerminalService) UpdateTerminalMetadata(types.TerminalID, string, map[string]string) error {
	return nil
}

func (f *failingTerminalService) ConnectTerminalInNewTab(types.WorkspaceID, types.TerminalID) error {
	return nil
}

func (f *failingTerminalService) ConnectTerminalInFloatingPane(types.WorkspaceID, types.TabID, types.TerminalID) error {
	return nil
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

func TestModelUpdateStoresNoticesFromEffectFeedback(t *testing.T) {
	scheduler := &stubNoticeScheduler{}
	model := NewModel(ModelConfig{
		InitialState:    newAppStateWithSinglePane(),
		Mapper:          NewIntentMapper(Config{}),
		Reducer:         reducer.New(),
		NoticeScheduler: scheduler,
	})

	updatedModel, cmd := model.Update(effectResultMsg{
		Notices: []Notice{{Level: NoticeLevelError, Text: "stop terminal failed"}},
	})
	updated := updatedModel.(*Model)
	if cmd != nil {
		t.Fatalf("expected nil command for notice-only feedback, got %v", cmd)
	}
	if len(updated.Notices()) != 1 {
		t.Fatalf("expected one notice, got %d", len(updated.Notices()))
	}
	if len(scheduler.ids) != 1 || scheduler.ids[0] == "" {
		t.Fatalf("expected one scheduled notice timeout, got %+v", scheduler.ids)
	}
	if updated.Notices()[0].Text != "stop terminal failed" {
		t.Fatalf("unexpected notice payload: %+v", updated.Notices()[0])
	}
}

func TestModelUpdateNoticeTimeoutRemovesMatchingNotice(t *testing.T) {
	scheduler := &stubNoticeScheduler{msg: noticeTimeoutMsg{ID: "notice-1"}}
	model := NewModel(ModelConfig{
		InitialState:    newAppStateWithSinglePane(),
		Mapper:          NewIntentMapper(Config{}),
		Reducer:         reducer.New(),
		NoticeScheduler: scheduler,
	})

	updatedModel, cmd := model.Update(effectResultMsg{
		Notices: []Notice{{ID: "notice-1", Level: NoticeLevelError, Text: "stop terminal failed"}},
	})
	updated := updatedModel.(*Model)
	if cmd == nil {
		t.Fatalf("expected notice timeout command")
	}
	timeoutMsg := cmd()
	nextModel, nextCmd := updated.Update(timeoutMsg)
	next := nextModel.(*Model)
	if nextCmd != nil {
		t.Fatalf("expected nil command after timeout handling, got %v", nextCmd)
	}
	if len(next.Notices()) != 0 {
		t.Fatalf("expected timed out notice removed, got %+v", next.Notices())
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

func TestE2EModelScenarioCtrlFSearchesAndConnectsTerminal(t *testing.T) {
	model := NewModel(ModelConfig{
		InitialState:  newManagerAppState(),
		Mapper:        NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:       reducer.New(),
		EffectHandler: RuntimeEffectHandler{Executor: DefaultRuntimeExecutor{}},
		Renderer:      StaticRenderer{},
	})

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlF},
		{Type: tea.KeyRunes, Runes: []rune("ops")},
		{Type: tea.KeyEnter},
	}

	current := model
	for _, key := range sequence {
		next, _ := current.Update(key)
		current = next.(*Model)
	}

	state := current.State()
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after picker submit, got %q", state.UI.Overlay.Kind)
	}
	pane := state.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-3") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected picker flow to connect searched terminal, got %+v", pane)
	}
}

func TestE2EModelScenarioTerminalManagerEditOpensMetadataPrompt(t *testing.T) {
	model := NewModel(ModelConfig{
		InitialState:  newManagerAppState(),
		Mapper:        NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:       reducer.New(),
		EffectHandler: RuntimeEffectHandler{Executor: DefaultRuntimeExecutor{}},
		Renderer:      StaticRenderer{},
	})

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlG},
		{Type: tea.KeyRunes, Runes: []rune("t")},
		{Type: tea.KeyRunes, Runes: []rune("e")},
	}

	current := model
	var feedback tea.Msg
	for _, key := range sequence {
		next, cmd := current.Update(key)
		current = next.(*Model)
		if cmd != nil {
			feedback = cmd()
		}
	}
	if feedback == nil {
		t.Fatalf("expected prompt feedback after edit action")
	}
	next, _ := current.Update(feedback)
	current = next.(*Model)

	state := current.State()
	if state.UI.Overlay.Kind != types.OverlayPrompt {
		t.Fatalf("expected prompt overlay after metadata edit, got %q", state.UI.Overlay.Kind)
	}
	prompt, ok := state.UI.Overlay.Data.(*promptdomain.State)
	if !ok {
		t.Fatalf("expected prompt overlay data, got %T", state.UI.Overlay.Data)
	}
	if prompt.Kind != promptdomain.KindEditTerminalMetadata || prompt.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("unexpected prompt payload: %+v", prompt)
	}
}

func TestE2EModelScenarioFailedStopRecordsErrorNotice(t *testing.T) {
	service := &failingTerminalService{stopErr: errBoom}
	scheduler := &stubNoticeScheduler{msg: noticeTimeoutMsg{ID: "notice-1"}}
	model := NewModel(ModelConfig{
		InitialState:    newManagerAppState(),
		Mapper:          NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:         reducer.New(),
		EffectHandler:   RuntimeEffectHandler{Executor: DefaultRuntimeExecutor{TerminalService: service}},
		Renderer:        StaticRenderer{},
		NoticeScheduler: scheduler,
	})

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlG},
		{Type: tea.KeyRunes, Runes: []rune("t")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
	}

	current := model
	var feedback tea.Msg
	for _, key := range sequence {
		next, cmd := current.Update(key)
		current = next.(*Model)
		if cmd != nil {
			feedback = cmd()
		}
	}
	if feedback == nil {
		t.Fatalf("expected feedback message after failed stop")
	}
	next, _ := current.Update(feedback)
	current = next.(*Model)

	if len(current.Notices()) != 1 {
		t.Fatalf("expected one notice after failed stop, got %d", len(current.Notices()))
	}
	if current.Notices()[0].Level != NoticeLevelError {
		t.Fatalf("expected error notice, got %+v", current.Notices()[0])
	}
}

func TestE2EModelScenarioNoticeTimeoutClearsErrorNotice(t *testing.T) {
	service := &failingTerminalService{stopErr: errBoom}
	scheduler := &stubNoticeScheduler{msg: noticeTimeoutMsg{ID: "notice-1"}}
	model := NewModel(ModelConfig{
		InitialState:    newManagerAppState(),
		Mapper:          NewIntentMapper(Config{Clock: fixedClock{}}),
		Reducer:         reducer.New(),
		EffectHandler:   RuntimeEffectHandler{Executor: DefaultRuntimeExecutor{TerminalService: service}},
		Renderer:        StaticRenderer{},
		NoticeScheduler: scheduler,
	})

	current := model
	var feedback tea.Msg
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlG},
		{Type: tea.KeyRunes, Runes: []rune("t")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
	} {
		next, cmd := current.Update(key)
		current = next.(*Model)
		if cmd != nil {
			feedback = cmd()
		}
	}
	next, timeoutCmd := current.Update(feedback)
	current = next.(*Model)
	if timeoutCmd == nil {
		t.Fatalf("expected timeout command after notice feedback")
	}
	timeoutMsg := timeoutCmd()
	next, _ = current.Update(timeoutMsg)
	current = next.(*Model)

	if len(current.Notices()) != 0 {
		t.Fatalf("expected notice to clear after timeout, got %+v", current.Notices())
	}
}

func newManagerAppState() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotConnected
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws

	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Tags:    map[string]string{"group": "api"},
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Command: []string{"tail", "-f", "build.log"},
		Tags:    map[string]string{"group": "build"},
	}
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:      types.TerminalID("term-3"),
		Name:    "ops-watch",
		State:   types.TerminalRunStateRunning,
		Command: []string{"journalctl", "-f"},
		Tags:    map[string]string{"team": "ops"},
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}
