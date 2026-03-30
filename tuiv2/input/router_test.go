package input

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// helper: build a KeyMsg for a control key.
func ctrlKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// helper: build a KeyMsg for a printable rune.
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// helper: build a KeyMsg for a special (non-rune) key.
func specialKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// --- Ctrl-F → ActionOpenPicker ---

func TestRouteKeyMsg_CtrlF_NormalMode_ProducesOpenPicker(t *testing.T) {
	r := NewRouter() // starts in ModeNormal
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))

	if result.Action == nil {
		t.Fatal("expected SemanticAction, got nil")
	}
	if result.Action.Kind != ActionOpenPicker {
		t.Errorf("expected ActionOpenPicker, got %q", result.Action.Kind)
	}
	if result.TerminalInput != nil {
		t.Error("TerminalInput should be nil when a SemanticAction fires")
	}
}

// --- Ctrl-C in Normal mode → passthrough (nil SemanticAction) ---

func TestRouteKeyMsg_CtrlC_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlC))

	if result.Action != nil {
		t.Errorf("Ctrl-C should not produce a SemanticAction in Normal mode, got %v", result.Action)
	}
	if result.TerminalInput == nil {
		t.Fatal("Ctrl-C should produce TerminalInput passthrough")
	}
	if result.TerminalInput.Kind != TerminalInputBytes {
		t.Errorf("expected TerminalInputBytes, got %q", result.TerminalInput.Kind)
	}
	// Ctrl-C → 0x03
	if len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != 0x03 {
		t.Errorf("Ctrl-C: expected [0x03], got %v", result.TerminalInput.Data)
	}
}

// --- Escape in Normal mode → passthrough (nil SemanticAction) ---

func TestRouteKeyMsg_Escape_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(specialKey(tea.KeyEsc))

	if result.Action != nil {
		t.Errorf("Escape should not produce a SemanticAction in Normal mode, got %v", result.Action)
	}
	if result.TerminalInput == nil {
		t.Fatal("Escape should produce TerminalInput passthrough")
	}
	if len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != 0x1b {
		t.Errorf("Escape: expected [0x1b], got %v", result.TerminalInput.Data)
	}
}

// --- Printable rune 'a' in Normal mode → TerminalInput passthrough ---

func TestRouteKeyMsg_PrintableRune_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(runeKey('a'))

	if result.Action != nil {
		t.Errorf("printable rune should not produce SemanticAction, got %v", result.Action)
	}
	if result.TerminalInput == nil {
		t.Fatal("printable rune should produce TerminalInput")
	}
	if result.TerminalInput.Kind != TerminalInputBytes {
		t.Errorf("expected TerminalInputBytes, got %q", result.TerminalInput.Kind)
	}
	if string(result.TerminalInput.Data) != "a" {
		t.Errorf("expected 'a', got %q", result.TerminalInput.Data)
	}
}

func TestRouteKeyMsg_MultipleRunes_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}}
	result := r.RouteKeyMsg(msg)

	if result.Action != nil {
		t.Errorf("rune sequence should not produce SemanticAction, got %v", result.Action)
	}
	if result.TerminalInput == nil {
		t.Fatal("rune sequence should produce TerminalInput")
	}
	if string(result.TerminalInput.Data) != "hi" {
		t.Errorf("expected 'hi', got %q", result.TerminalInput.Data)
	}
}

// --- Picker mode: any key should not produce a SemanticAction ---

func TestRouteKeyMsg_AnyKey_PickerMode_NoSemanticAction(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePicker})

	keys := []tea.KeyMsg{
		ctrlKey(tea.KeyCtrlF),   // would be ActionOpenPicker in Normal
		ctrlKey(tea.KeyCtrlC),
		specialKey(tea.KeyEsc),
		runeKey('a'),
		runeKey('z'),
	}

	for _, msg := range keys {
		result := r.RouteKeyMsg(msg)
		if result.Action != nil {
			t.Errorf("in Picker mode, key %v should not produce SemanticAction, got %v", msg, result.Action)
		}
	}
}

// --- Ctrl-F in non-Normal modes → no SemanticAction ---

func TestRouteKeyMsg_CtrlF_NonNormalModes_NoSemanticAction(t *testing.T) {
	modes := []ModeKind{ModePicker, ModePrompt, ModeHelp, ModePrefix, ModeTerminalManager, ModeWorkspacePicker}
	for _, mode := range modes {
		r := NewRouter()
		r.SetMode(ModeState{Kind: mode})
		result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))
		if result.Action != nil {
			t.Errorf("mode %q: Ctrl-F should not produce SemanticAction, got %v", mode, result.Action)
		}
	}
}

// --- Mode round-trip via SetMode/Mode ---

func TestRouter_SetMode_RoundTrip(t *testing.T) {
	r := NewRouter()
	if r.Mode().Kind != ModeNormal {
		t.Errorf("initial mode should be ModeNormal, got %q", r.Mode().Kind)
	}
	r.SetMode(ModeState{Kind: ModePicker})
	if r.Mode().Kind != ModePicker {
		t.Errorf("expected ModePicker after SetMode, got %q", r.Mode().Kind)
	}
}

// --- Custom keymap ---

func TestRouter_CustomKeymap_RespectedInNormalMode(t *testing.T) {
	km := Keymap{
		Normal: []Binding{
			{Type: tea.KeyCtrlT, Action: ActionOpenTerminalManager},
		},
	}
	r := NewRouterWithKeymap(km)
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlT))
	if result.Action == nil || result.Action.Kind != ActionOpenTerminalManager {
		t.Errorf("expected ActionOpenTerminalManager, got %v", result.Action)
	}
	// Ctrl-F not in custom map → passthrough
	result2 := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))
	if result2.Action != nil {
		t.Errorf("Ctrl-F not in custom keymap, expected no action, got %v", result2.Action)
	}
}
