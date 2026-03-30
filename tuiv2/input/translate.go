package input

import tea "github.com/charmbracelet/bubbletea"

// TranslateKeyMsg converts a tea.KeyMsg into a RouteResult given the current
// mode and keymap.
//
// Routing rules:
//   - ModePicker (and all non-Normal modes): no SemanticAction is produced;
//     the key is forwarded as TerminalInput so the active widget can consume it.
//   - ModeNormal: keymap is consulted first; if a binding matches, return its
//     SemanticAction with no TerminalInput.
//   - ModeNormal, no binding: printable runes and control keys that are not
//     intercepted are forwarded as TerminalInput (passthrough to the terminal).
func TranslateKeyMsg(msg tea.KeyMsg, mode ModeKind, km *Keymap) RouteResult {
	// In any overlay mode the router does not produce semantic actions —
	// the overlay component handles the key itself.
	if mode != ModeNormal {
		return RouteResult{}
	}

	// Consult keymap.
	if action := km.LookupNormal(msg); action != "" {
		return RouteResult{
			Action: &SemanticAction{Kind: action},
		}
	}

	// Passthrough: encode the key as terminal bytes.
	data := encodeTeaKey(msg)
	if len(data) == 0 {
		return RouteResult{}
	}
	return RouteResult{
		TerminalInput: &TerminalInput{
			Kind: TerminalInputBytes,
			Data: data,
		},
	}
}

// encodeTeaKey converts a tea.KeyMsg to its raw byte representation.
// This covers the common cases needed for TUI passthrough; a full
// kitty-protocol encoder lives elsewhere.
func encodeTeaKey(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		prefix := []byte(nil)
		if msg.Alt {
			prefix = []byte{0x1b}
		}
		text := []byte(string(msg.Runes))
		if prefix != nil {
			return append(prefix, text...)
		}
		return text

	case tea.KeySpace:
		if msg.Alt {
			return []byte{0x1b, ' '}
		}
		return []byte{' '}

	case tea.KeyEnter:
		return []byte{'\r'}

	case tea.KeyBackspace:
		return []byte{0x7f}

	case tea.KeyTab:
		return []byte{'\t'}

	case tea.KeyEsc:
		// Pass ESC through as-is so the terminal sees it.
		// (tea.KeyEscape == tea.KeyEsc; only one case needed)
		return []byte{0x1b}

	case tea.KeyCtrlC:
		return []byte{0x03}

	case tea.KeyCtrlA:
		return []byte{0x01}

	case tea.KeyCtrlD:
		return []byte{0x04}

	case tea.KeyCtrlZ:
		return []byte{0x1a}

	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}

	default:
		return nil
	}
}

// TranslateKey is the legacy stub preserved for API compatibility.
// New callers should use TranslateKeyMsg.
func TranslateKey(any) RouteResult {
	return RouteResult{}
}

// TranslateRaw is a stub for raw-byte routing (not yet implemented).
func TranslateRaw([]byte) RouteResult {
	return RouteResult{}
}

// TranslateEvent is a stub for generic event routing (not yet implemented).
func TranslateEvent(any) RouteResult {
	return RouteResult{}
}
