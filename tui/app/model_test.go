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

func TestTerminalPoolKeysDriveSelectionAndSearchThroughUpdate(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})

	teaModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	down := teaModel.(Model)
	if down.Pool.SelectedTerminalID != "term-2" {
		t.Fatalf("expected down key to move selection to term-2, got %q", down.Pool.SelectedTerminalID)
	}
	if down.Pool.PreviewTerminalID != "term-2" {
		t.Fatalf("expected down key to switch preview to term-2, got %q", down.Pool.PreviewTerminalID)
	}

	teaModel, _ = down.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	searching := teaModel.(Model)
	if !searching.Pool.SearchInputActive {
		t.Fatal("expected slash to enter search input mode")
	}

	filtered := searching
	for _, r := range []rune("backend") {
		teaModel, _ = filtered.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		filtered = teaModel.(Model)
	}
	if filtered.Pool.Query != "backend" {
		t.Fatalf("expected search query to update, got %q", filtered.Pool.Query)
	}
	if filtered.Pool.SelectedTerminalID != "term-1" {
		t.Fatalf("expected search to select term-1, got %q", filtered.Pool.SelectedTerminalID)
	}
	if filtered.Pool.PreviewTerminalID != "term-1" {
		t.Fatalf("expected search to switch preview term-1, got %q", filtered.Pool.PreviewTerminalID)
	}
}

func TestTerminalPoolOverlayBlocksPageHotkeysExceptEsc(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})
	model = model.Apply(OpenTerminalMetadataEditorIntent{})
	before := model

	teaModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	after := teaModel.(Model)
	if after.Overlay.Active().Kind != OverlayTerminalMetadataEditor {
		t.Fatalf("expected metadata editor to stay open, got %q", after.Overlay.Active().Kind)
	}
	if after.Pool.SelectedTerminalID != before.Pool.SelectedTerminalID {
		t.Fatalf("expected overlay hotkey to avoid pool action, got %q", after.Pool.SelectedTerminalID)
	}

	teaModel, _ = after.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cancelled := teaModel.(Model)
	if cancelled.Overlay.HasActive() {
		t.Fatal("expected esc to close overlay")
	}
}
