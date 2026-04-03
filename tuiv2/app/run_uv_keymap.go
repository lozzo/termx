package app

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func uvKeyToTeaKeyMsg(event uv.KeyPressEvent) (tea.KeyMsg, bool) {
	k := event.Key()
	msg := tea.KeyMsg{Alt: k.Mod.Contains(uv.ModAlt)}

	if k.Mod.Contains(uv.ModCtrl) {
		switch k.Code {
		case uv.KeyUp:
			msg.Type = tea.KeyCtrlUp
			return msg, true
		case uv.KeyDown:
			msg.Type = tea.KeyCtrlDown
			return msg, true
		case uv.KeyRight:
			msg.Type = tea.KeyCtrlRight
			return msg, true
		case uv.KeyLeft:
			msg.Type = tea.KeyCtrlLeft
			return msg, true
		}

		switch {
		case k.Code >= 'a' && k.Code <= 'z':
			msg.Type = tea.KeyType(int(tea.KeyCtrlA) + int(k.Code-'a'))
			return msg, true
		case k.Code >= 'A' && k.Code <= 'Z':
			msg.Type = tea.KeyType(int(tea.KeyCtrlA) + int(unicode.ToLower(k.Code)-'a'))
			return msg, true
		case k.Code == '\\':
			msg.Type = tea.KeyCtrlBackslash
			return msg, true
		case k.Code == ']':
			msg.Type = tea.KeyCtrlCloseBracket
			return msg, true
		case k.Code == '^':
			msg.Type = tea.KeyCtrlCaret
			return msg, true
		case k.Code == '_':
			msg.Type = tea.KeyCtrlUnderscore
			return msg, true
		case k.Code == ' ' || k.Code == '@':
			msg.Type = tea.KeyCtrlAt
			return msg, true
		case k.Code == '?':
			msg.Type = tea.KeyCtrlQuestionMark
			return msg, true
		}
	}

	if k.Mod.Contains(uv.ModShift) {
		switch k.Code {
		case uv.KeyTab:
			msg.Type = tea.KeyShiftTab
			return msg, true
		case uv.KeyUp:
			msg.Type = tea.KeyShiftUp
			return msg, true
		case uv.KeyDown:
			msg.Type = tea.KeyShiftDown
			return msg, true
		case uv.KeyRight:
			msg.Type = tea.KeyShiftRight
			return msg, true
		case uv.KeyLeft:
			msg.Type = tea.KeyShiftLeft
			return msg, true
		}
	}

	switch k.Code {
	case uv.KeyUp:
		msg.Type = tea.KeyUp
	case uv.KeyDown:
		msg.Type = tea.KeyDown
	case uv.KeyRight:
		msg.Type = tea.KeyRight
	case uv.KeyLeft:
		msg.Type = tea.KeyLeft
	case uv.KeyTab:
		msg.Type = tea.KeyTab
	case uv.KeyEnter, uv.KeyKpEnter:
		msg.Type = tea.KeyEnter
	case uv.KeyEscape:
		msg.Type = tea.KeyEsc
	case uv.KeyBackspace:
		msg.Type = tea.KeyBackspace
	case uv.KeyDelete:
		msg.Type = tea.KeyDelete
	case uv.KeyInsert:
		msg.Type = tea.KeyInsert
	case uv.KeyHome:
		msg.Type = tea.KeyHome
	case uv.KeyEnd:
		msg.Type = tea.KeyEnd
	case uv.KeyPgUp:
		msg.Type = tea.KeyPgUp
	case uv.KeyPgDown:
		msg.Type = tea.KeyPgDown
	case uv.KeySpace:
		msg.Type = tea.KeySpace
	case uv.KeyF1:
		msg.Type = tea.KeyF1
	case uv.KeyF2:
		msg.Type = tea.KeyF2
	case uv.KeyF3:
		msg.Type = tea.KeyF3
	case uv.KeyF4:
		msg.Type = tea.KeyF4
	case uv.KeyF5:
		msg.Type = tea.KeyF5
	case uv.KeyF6:
		msg.Type = tea.KeyF6
	case uv.KeyF7:
		msg.Type = tea.KeyF7
	case uv.KeyF8:
		msg.Type = tea.KeyF8
	case uv.KeyF9:
		msg.Type = tea.KeyF9
	case uv.KeyF10:
		msg.Type = tea.KeyF10
	case uv.KeyF11:
		msg.Type = tea.KeyF11
	case uv.KeyF12:
		msg.Type = tea.KeyF12
	default:
		if k.Text != "" {
			msg.Type = tea.KeyRunes
			msg.Runes = []rune(k.Text)
			return msg, true
		}
		if unicode.IsPrint(k.Code) {
			msg.Type = tea.KeyRunes
			msg.Runes = []rune{rune(k.Code)}
			return msg, true
		}
		return tea.KeyMsg{}, false
	}
	return msg, true
}
