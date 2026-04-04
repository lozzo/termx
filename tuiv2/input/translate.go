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
		if action := km.LookupNormal(msg); action != nil {
			return RouteResult{Action: action}
		}
		data := encodeTeaKey(msg)
		if len(data) != 0 {
			return RouteResult{TerminalInput: &TerminalInput{Kind: TerminalInputBytes, Data: data}}
		}
		return RouteResult{TerminalInput: &TerminalInput{Kind: TerminalInputEncodedKey}}
	case ModePane:
		return routeStickyModeKeyMsg(msg, km.LookupPane, km)
	case ModeResize:
		return routeStickyModeKeyMsg(msg, km.LookupResize, km)
	case ModeTab:
		return routeStickyModeKeyMsg(msg, km.LookupTab, km)
	case ModeWorkspace:
		return routeStickyModeKeyMsg(msg, km.LookupWorkspace, km)
	case ModeFloating:
		return routeStickyModeKeyMsg(msg, km.LookupFloating, km)
	case ModeDisplay:
		return routeStickyModeKeyMsg(msg, km.LookupDisplay, km)
	case ModeGlobal:
		return routeStickyModeKeyMsg(msg, km.LookupGlobal, km)
	case ModeTerminalManager:
		if action := km.LookupTerminalManager(msg); action != nil {
			return RouteResult{Action: action}
		}
		return RouteResult{}
	case ModePicker:
		if action := km.LookupPicker(msg); action != nil {
			return RouteResult{Action: action}
		}
		return RouteResult{}
	case ModeWorkspacePicker:
		if action := km.LookupWorkspacePicker(msg); action != nil {
			return RouteResult{Action: action}
		}
		return RouteResult{}
	case ModeHelp:
		if action := km.LookupHelp(msg); action != nil {
			return RouteResult{Action: action}
		}
		return RouteResult{}
	default:
		return RouteResult{}
	}
}

func routeStickyModeKeyMsg(msg tea.KeyMsg, lookup func(tea.KeyMsg) *SemanticAction, km *Keymap) RouteResult {
	if action := lookup(msg); action != nil {
		return RouteResult{Action: action}
	}
	if action := km.LookupNormal(msg); action != nil {
		return RouteResult{Action: action}
	}
	return RouteResult{}
}

// encodeTeaKey converts a tea.KeyMsg to its raw byte representation suitable
// for forwarding to a PTY. All printable keys and Ctrl combinations are covered
// so that ModeNormal is a true passthrough.
func encodeTeaKey(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		var prefix []byte
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
	// Ctrl keys — sent as the corresponding ASCII control code.
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlH:
		return []byte{0x08}
	case tea.KeyCtrlJ:
		return []byte{0x0a}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlQ:
		return []byte{0x11}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlV:
		return []byte{0x16}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlX:
		return []byte{0x18}
	case tea.KeyCtrlY:
		return []byte{0x19}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	// Arrow keys.
	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}
	// Other common keys.
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyPgUp:
		return []byte{0x1b, '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{0x1b, '[', '6', '~'}
	default:
		return nil
	}
}
