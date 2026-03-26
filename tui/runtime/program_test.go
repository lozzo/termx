package runtime

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
)

type stubEffectRunner struct {
	messages []app.Message
	effects  []app.Effect
}

func (s *stubEffectRunner) Run(ctx context.Context, effect app.Effect) app.Message {
	s.effects = append(s.effects, effect)
	if len(s.messages) == 0 {
		return nil
	}
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message
}

func TestRenderModelMapsWorkbenchKeysThroughRouter(t *testing.T) {
	model := app.NewModel("main")
	model.Workbench.BindActivePane(coreterminal.Metadata{ID: types.TerminalID("term-1"), Name: "shell", State: coreterminal.StateRunning})
	model.Pool.Visible = []featureterminalpool.Item{}
	render := NewRenderModel(model).(*renderModel)

	next, _ := render.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	updated := next.(*renderModel)
	if updated.model.Overlay.Active.Kind != "connect-picker" {
		t.Fatalf("expected connect overlay after key routing, got %q", updated.model.Overlay.Active.Kind)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated = next.(*renderModel)
	if updated.model.Screen != app.ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen after key routing, got %q", updated.model.Screen)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = next.(*renderModel)
	if updated.model.Screen != app.ScreenWorkbench {
		t.Fatalf("expected workbench after esc, got %q", updated.model.Screen)
	}
}

func TestRenderModelMapsConnectOverlayKeysThroughRouter(t *testing.T) {
	model := app.NewModel("main")
	model.Pool.Visible = []featureterminalpool.Item{}
	model.Workbench.BindActivePane(coreterminal.Metadata{ID: types.TerminalID("term-1"), Name: "shell-1", State: coreterminal.StateRunning})
	model, _ = app.Reduce(model, app.MessageTerminalPoolLoaded{Terminals: []coreterminal.Metadata{{ID: types.TerminalID("term-1"), Name: "shell-1", State: coreterminal.StateRunning}, {ID: types.TerminalID("term-2"), Name: "shell-2", State: coreterminal.StateRunning}}})
	model, _ = app.Reduce(model, app.IntentOpenConnectOverlay)
	render := NewRenderModel(model).(*renderModel)

	next, _ := render.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	updated := next.(*renderModel)
	if updated.model.Overlay.Active.Selected != types.TerminalID("term-2") {
		t.Fatalf("expected overlay selection moved to term-2, got %q", updated.model.Overlay.Active.Selected)
	}
}

func TestRenderModelRunsEffectsAndAppliesReturnedMessage(t *testing.T) {
	model := app.NewModel("main")
	runner := &stubEffectRunner{messages: []app.Message{app.MessageTerminalPoolLoaded{Terminals: []coreterminal.Metadata{{ID: types.TerminalID("term-1"), Name: "shell-1", State: coreterminal.StateRunning}}}}}
	render := NewRenderModelWithRunner(model, runner).(*renderModel)

	next, cmd := render.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated := next.(*renderModel)
	if updated.model.Screen != app.ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen after intent, got %q", updated.model.Screen)
	}
	if cmd == nil {
		t.Fatal("expected effect command")
	}
	msg := cmd()
	if _, ok := msg.(effectResultMsg); !ok {
		t.Fatalf("expected effect result msg, got %T", msg)
	}
	next, _ = updated.Update(msg)
	updated = next.(*renderModel)
	if len(updated.model.Pool.Parked) != 1 || updated.model.Pool.Parked[0].ID != types.TerminalID("term-1") {
		t.Fatalf("expected runtime-loaded parked item, got visible=%#v parked=%#v exited=%#v", updated.model.Pool.Visible, updated.model.Pool.Parked, updated.model.Pool.Exited)
	}
	if len(runner.effects) != 1 {
		t.Fatalf("expected one effect executed, got %d", len(runner.effects))
	}
}
