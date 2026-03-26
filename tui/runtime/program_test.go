package runtime

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
)

func TestRenderModelMapsWorkbenchKeysThroughRouter(t *testing.T) {
	model := NewRenderModel(app.NewModel("main"))
	render, ok := model.(*renderModel)
	if !ok {
		t.Fatalf("expected *renderModel, got %T", model)
	}

	next, _ := render.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	updated := next.(*renderModel)
	if updated.model.Overlay.Active.Kind != featureoverlay.KindConnectPicker {
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
