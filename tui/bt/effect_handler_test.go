package bt

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type stubRuntimeExecutor struct {
	effects       []reducer.Effect
	intentsByName map[string][]intent.Intent
}

func (e *stubRuntimeExecutor) Execute(effect reducer.Effect) ([]intent.Intent, error) {
	e.effects = append(e.effects, effect)
	if e.intentsByName == nil {
		return nil, nil
	}
	switch effect.(type) {
	case reducer.OpenPromptEffect:
		return e.intentsByName["open_prompt"], nil
	default:
		return nil, nil
	}
}

type stubTerminalService struct {
	connectCalls    []connectCall
	createCalls     []createCall
	stopCalls       []types.TerminalID
	metadataCalls   []metadataCall
	newTabCalls     []newTabCall
	floatingCalls   []floatingCall
}

type connectCall struct {
	paneID     types.PaneID
	terminalID types.TerminalID
}

type createCall struct {
	paneID  types.PaneID
	command []string
	name    string
}

type metadataCall struct {
	terminalID types.TerminalID
	name       string
	tags       map[string]string
}

type newTabCall struct {
	workspaceID types.WorkspaceID
	terminalID  types.TerminalID
}

type floatingCall struct {
	workspaceID types.WorkspaceID
	tabID       types.TabID
	terminalID  types.TerminalID
}

func (s *stubTerminalService) ConnectTerminal(paneID types.PaneID, terminalID types.TerminalID) error {
	s.connectCalls = append(s.connectCalls, connectCall{paneID: paneID, terminalID: terminalID})
	return nil
}

func (s *stubTerminalService) CreateTerminal(paneID types.PaneID, command []string, name string) error {
	s.createCalls = append(s.createCalls, createCall{
		paneID:  paneID,
		command: append([]string(nil), command...),
		name:    name,
	})
	return nil
}

func (s *stubTerminalService) StopTerminal(terminalID types.TerminalID) error {
	s.stopCalls = append(s.stopCalls, terminalID)
	return nil
}

func (s *stubTerminalService) UpdateTerminalMetadata(terminalID types.TerminalID, name string, tags map[string]string) error {
	cloned := make(map[string]string, len(tags))
	for key, value := range tags {
		cloned[key] = value
	}
	s.metadataCalls = append(s.metadataCalls, metadataCall{
		terminalID: terminalID,
		name:       name,
		tags:       cloned,
	})
	return nil
}

func (s *stubTerminalService) ConnectTerminalInNewTab(workspaceID types.WorkspaceID, terminalID types.TerminalID) error {
	s.newTabCalls = append(s.newTabCalls, newTabCall{
		workspaceID: workspaceID,
		terminalID:  terminalID,
	})
	return nil
}

func (s *stubTerminalService) ConnectTerminalInFloatingPane(workspaceID types.WorkspaceID, tabID types.TabID, terminalID types.TerminalID) error {
	s.floatingCalls = append(s.floatingCalls, floatingCall{
		workspaceID: workspaceID,
		tabID:       tabID,
		terminalID:  terminalID,
	})
	return nil
}

func TestRuntimeEffectHandlerReturnsFeedbackIntentMessage(t *testing.T) {
	executor := &stubRuntimeExecutor{
		intentsByName: map[string][]intent.Intent{
			"open_prompt": {
				intent.OpenPromptIntent{
					PromptKind: reducer.PromptKindCreateWorkspace,
				},
			},
		},
	}
	handler := RuntimeEffectHandler{Executor: executor}

	cmd := handler.Handle([]reducer.Effect{
		reducer.OpenPromptEffect{PromptKind: reducer.PromptKindCreateWorkspace},
	})
	if cmd == nil {
		t.Fatalf("expected runtime effect handler command")
	}
	msg := cmd()
	result, ok := msg.(effectIntentsMsg)
	if !ok {
		t.Fatalf("expected effect intents msg, got %T", msg)
	}
	if len(result.Intents) != 1 {
		t.Fatalf("expected one feedback intent, got %d", len(result.Intents))
	}
	if _, ok := result.Intents[0].(intent.OpenPromptIntent); !ok {
		t.Fatalf("expected open prompt feedback, got %T", result.Intents[0])
	}
	if len(executor.effects) != 1 {
		t.Fatalf("expected one executed effect, got %d", len(executor.effects))
	}
}

func TestDefaultRuntimeExecutorCallsTerminalServiceAndTranslatesOverlayEffects(t *testing.T) {
	service := &stubTerminalService{}
	executor := DefaultRuntimeExecutor{TerminalService: service}

	intents, err := executor.Execute(reducer.ConnectTerminalEffect{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-1"),
	})
	if err != nil {
		t.Fatalf("unexpected connect error: %v", err)
	}
	if len(intents) != 0 || len(service.connectCalls) != 1 {
		t.Fatalf("expected connect service call only, got intents=%d calls=%d", len(intents), len(service.connectCalls))
	}

	_, _ = executor.Execute(reducer.CreateTerminalEffect{
		PaneID:  types.PaneID("pane-1"),
		Command: []string{"sh", "-l"},
		Name:    "ws-tab-pane",
	})
	_, _ = executor.Execute(reducer.StopTerminalEffect{TerminalID: types.TerminalID("term-2")})
	_, _ = executor.Execute(reducer.UpdateTerminalMetadataEffect{
		TerminalID: types.TerminalID("term-3"),
		Name:       "build-log",
		Tags:       map[string]string{"group": "build"},
	})
	_, _ = executor.Execute(reducer.ConnectTerminalInNewTabEffect{
		WorkspaceID: types.WorkspaceID("ws-1"),
		TerminalID:  types.TerminalID("term-4"),
	})
	_, _ = executor.Execute(reducer.ConnectTerminalInFloatingPaneEffect{
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		TerminalID:  types.TerminalID("term-5"),
	})

	intents, err = executor.Execute(reducer.OpenPromptEffect{
		PromptKind: reducer.PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-6"),
	})
	if err != nil {
		t.Fatalf("unexpected open prompt error: %v", err)
	}
	if len(intents) != 1 {
		t.Fatalf("expected one open prompt feedback intent, got %d", len(intents))
	}
	openPrompt, ok := intents[0].(intent.OpenPromptIntent)
	if !ok {
		t.Fatalf("expected open prompt intent, got %T", intents[0])
	}
	if openPrompt.PromptKind != reducer.PromptKindEditTerminalMetadata || openPrompt.TerminalID != types.TerminalID("term-6") {
		t.Fatalf("unexpected prompt feedback payload: %+v", openPrompt)
	}
	if len(service.createCalls) != 1 || len(service.stopCalls) != 1 || len(service.metadataCalls) != 1 {
		t.Fatalf("expected create/stop/metadata service calls, got create=%d stop=%d metadata=%d", len(service.createCalls), len(service.stopCalls), len(service.metadataCalls))
	}
	if len(service.newTabCalls) != 1 || len(service.floatingCalls) != 1 {
		t.Fatalf("expected new-tab/floating service calls, got newTab=%d floating=%d", len(service.newTabCalls), len(service.floatingCalls))
	}
}

func TestRuntimeEffectHandlerReturnsNilWhenExecutorProducesNoWork(t *testing.T) {
	handler := RuntimeEffectHandler{Executor: &stubRuntimeExecutor{}}

	if cmd := handler.Handle(nil); cmd != nil {
		t.Fatalf("expected nil command for empty effects, got %v", cmd)
	}
	if cmd := handler.Handle([]reducer.Effect{reducer.StopTerminalEffect{TerminalID: types.TerminalID("term-1")}}); cmd == nil {
		t.Fatalf("expected async command for runtime effects")
	} else if msg := cmd(); msg != nil {
		if _, ok := msg.(effectIntentsMsg); !ok {
			t.Fatalf("expected nil or effect intents msg, got %T", msg)
		}
	}
}

func TestE2EModelScenarioCreateWorkspaceEffectFeedbackOpensPrompt(t *testing.T) {
	model := NewModel(ModelConfig{
		InitialState:  newAppStateWithSinglePane(),
		Mapper:        NewIntentMapper(Config{}),
		Reducer:       reducer.New(),
		EffectHandler: RuntimeEffectHandler{Executor: DefaultRuntimeExecutor{}},
		Renderer:      StaticRenderer{},
	})

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlW},
		{Type: tea.KeyUp},
		{Type: tea.KeyUp},
		{Type: tea.KeyUp},
		{Type: tea.KeyEnter},
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
		t.Fatalf("expected effect feedback after create-workspace submit")
	}
	next, _ := current.Update(feedback)
	current = next.(*Model)

	state := current.State()
	if state.UI.Overlay.Kind != types.OverlayPrompt {
		t.Fatalf("expected prompt overlay after feedback, got %q", state.UI.Overlay.Kind)
	}
}
