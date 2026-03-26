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
	if got := router.Translate(Context{Screen: app.ScreenWorkbench}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}); got != app.IntentOpenConnectOverlay {
		t.Fatalf("expected connect overlay intent, got %#v", got)
	}
	if got := router.Translate(Context{Screen: app.ScreenWorkbench}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}); got != app.IntentDisconnectActivePane {
		t.Fatalf("expected disconnect intent, got %#v", got)
	}
	if got := router.Translate(Context{Screen: app.ScreenWorkbench}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); got != app.IntentReconnectActivePane {
		t.Fatalf("expected reconnect intent, got %#v", got)
	}
	if got := router.Translate(Context{Screen: app.ScreenWorkbench}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}); got != app.IntentOpenHelpOverlay {
		t.Fatalf("expected help intent, got %#v", got)
	}

	if got := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); got != app.IntentPoolSelectNext {
		t.Fatalf("expected pool next intent, got %#v", got)
	}
	if got := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}); got != app.IntentPoolSelectPrev {
		t.Fatalf("expected pool prev intent, got %#v", got)
	}
	if _, ok := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}).(app.IntentKillSelectedTerminal); !ok {
		t.Fatalf("expected pool kill intent")
	}
	if _, ok := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")}).(app.IntentRemoveSelectedTerminal); !ok {
		t.Fatalf("expected pool remove intent")
	}
	if got := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}); got != app.IntentOpenHelpOverlay {
		t.Fatalf("expected pool help intent, got %#v", got)
	}
	if got := router.Translate(Context{Screen: app.ScreenTerminalPool}, tea.KeyMsg{Type: tea.KeyEsc}); got != app.IntentCloseScreen {
		t.Fatalf("expected esc to close pool, got %#v", got)
	}
}
