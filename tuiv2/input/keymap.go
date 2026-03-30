package input

import tea "github.com/charmbracelet/bubbletea"

// Binding maps a key to an action in a specific mode.
type Binding struct {
	// Type matches tea.KeyMsg.Type for non-rune keys (e.g. KeyCtrlF, KeyEsc).
	// When Type == tea.KeyRunes, Rune must also match.
	Type tea.KeyType
	// Rune is only examined when Type == tea.KeyRunes.
	Rune rune
	// Action produced when this binding fires.
	Action ActionKind
}

// Keymap holds per-mode key bindings.
type Keymap struct {
	// Normal holds bindings active in ModeNormal.
	Normal []Binding
}

// DefaultKeymap returns the canonical key bindings for tuiv2.
//
// Design rationale:
//   - Ctrl-F  → open-picker    (mnemonic: Find a terminal)
//   - Ctrl-C  → nil in normal  (pass through to terminal)
//   - Escape  → nil in normal  (pass through to terminal)
//   - runes   → passthrough    (TerminalInput)
func DefaultKeymap() Keymap {
	return Keymap{
		Normal: []Binding{
			{Type: tea.KeyCtrlF, Action: ActionOpenPicker},
		},
	}
}

// LookupNormal returns the ActionKind bound to msg in ModeNormal, or "".
func (km *Keymap) LookupNormal(msg tea.KeyMsg) ActionKind {
	for _, b := range km.Normal {
		if b.Type != tea.KeyRunes {
			if msg.Type == b.Type {
				return b.Action
			}
		} else {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == b.Rune {
				return b.Action
			}
		}
	}
	return ""
}
