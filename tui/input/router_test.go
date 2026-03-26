package input

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
)

func TestInputRouterMapsWorkbenchAndPoolKeysToIntents(t *testing.T) {
	router := NewRouter()

	if got := router.Translate(Context{Screen: app.ScreenWorkbench}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}); got != app.IntentOpenTerminalPool {
		t.Fatalf("expected open pool intent, got %#v", got)
	}

	if got := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyEsc}); got != app.IntentCloseScreen {
		t.Fatalf("expected esc to close pool, got %#v", got)
	}
}
