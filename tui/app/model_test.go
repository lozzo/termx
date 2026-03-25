package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRootModelCanSwitchBetweenWorkbenchAndTerminalPoolScreens(t *testing.T) {
	model := NewModel()
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected initial screen %q, got %q", ScreenWorkbench, model.Screen)
	}
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected initial focus %q, got %q", FocusWorkbench, model.FocusTarget)
	}
	if model.Overlay.HasActive() {
		t.Fatalf("expected no active overlay, got %#v", model.Overlay)
	}

	model = model.SwitchScreen(ScreenTerminalPool)
	if model.Screen != ScreenTerminalPool {
		t.Fatalf("expected switched screen %q, got %q", ScreenTerminalPool, model.Screen)
	}
	if model.FocusTarget != FocusTerminalPool {
		t.Fatalf("expected switched focus %q, got %q", FocusTerminalPool, model.FocusTarget)
	}

	model = model.SwitchScreen(ScreenWorkbench)
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected switched back screen %q, got %q", ScreenWorkbench, model.Screen)
	}
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected switched back focus %q, got %q", FocusWorkbench, model.FocusTarget)
	}
}

func TestOpeningOverlayKeepsWorkbenchFocusTarget(t *testing.T) {
	model := NewModel()
	model = model.Apply(IntentOpenHelp)
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected overlay to keep workbench focus target, got %q", model.FocusTarget)
	}
	if !model.Overlay.HasActive() {
		t.Fatal("expected help overlay to be active")
	}
}

func TestAppShellCanNavigateBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := NewModel()

	pool := model.Apply(OpenTerminalPoolIntent{})
	if pool.Screen != ScreenTerminalPool {
		t.Fatalf("expected pool screen, got %q", pool.Screen)
	}
	if pool.FocusTarget != FocusTerminalPool {
		t.Fatalf("expected pool focus, got %q", pool.FocusTarget)
	}

	workbench := pool.Apply(CloseTerminalPoolIntent{})
	if workbench.Screen != ScreenWorkbench {
		t.Fatalf("expected workbench screen, got %q", workbench.Screen)
	}
	if workbench.FocusTarget != FocusWorkbench {
		t.Fatalf("expected workbench focus, got %q", workbench.FocusTarget)
	}
}

func TestAppShellHotkeyCanNavigateBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := NewModel()

	teaModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	pool := teaModel.(Model)
	if pool.Screen != ScreenTerminalPool {
		t.Fatalf("expected hotkey to open pool, got %q", pool.Screen)
	}

	teaModel, _ = pool.Update(tea.KeyMsg{Type: tea.KeyEsc})
	workbench := teaModel.(Model)
	if workbench.Screen != ScreenWorkbench {
		t.Fatalf("expected esc to return workbench, got %q", workbench.Screen)
	}
}
