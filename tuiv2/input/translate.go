package input

import tea "github.com/charmbracelet/bubbletea"

// TranslateKeyMsg converts a tea.KeyMsg into a RouteResult given the current
// mode and keymap.
func TranslateKeyMsg(msg tea.KeyMsg, mode ModeKind, km *Keymap) RouteResult {
	if km == nil {
		defaultMap := DefaultKeymap()
		km = &defaultMap
	}

	switch mode {
	case ModeNormal:
		if action := km.LookupNormal(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		data := encodeTeaKey(msg)
		if len(data) == 0 {
			return RouteResult{}
		}
		return RouteResult{TerminalInput: &TerminalInput{Kind: TerminalInputBytes, Data: data}}
	case ModePane:
		if action := km.LookupPane(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeResize:
		if action := km.LookupResize(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeTab:
		if action := km.LookupTab(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeWorkspace:
		if action := km.LookupWorkspace(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeFloating:
		if action := km.LookupFloating(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeDisplay:
		if action := km.LookupDisplay(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeGlobal:
		if action := km.LookupGlobal(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModePicker:
		if action := km.LookupPicker(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeWorkspacePicker:
		if action := km.LookupWorkspacePicker(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	case ModeHelp:
		if action := km.LookupHelp(msg); action != "" {
			return RouteResult{Action: &SemanticAction{Kind: action}}
		}
		return RouteResult{}
	default:
		return RouteResult{}
	}
}

// encodeTeaKey converts a tea.KeyMsg to its raw byte representation.
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

func TranslateKey(any) RouteResult { return RouteResult{} }
func TranslateRaw([]byte) RouteResult { return RouteResult{} }
func TranslateEvent(any) RouteResult { return RouteResult{} }
